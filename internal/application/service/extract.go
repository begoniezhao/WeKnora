package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/service/retriever"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
)

func NewChunkExtractTask(ctx context.Context, client *asynq.Client, tenantID uint, chunkID string, modelID string) error {
	payload, err := json.Marshal(types.ExtractChunkPayload{
		TenantID: tenantID,
		ChunkID:  chunkID,
		ModelID:  modelID,
	})
	if err != nil {
		return err
	}
	task := asynq.NewTask(types.TypeChunkExtract, payload, asynq.MaxRetry(3))
	info, err := client.Enqueue(task)
	if err != nil {
		logger.Errorf(ctx, "failed to enqueue task: %v", err)
		return fmt.Errorf("failed to enqueue task: %v", err)
	}
	logger.Infof(ctx, "enqueued task: id=%s queue=%s", info.ID, info.Queue)
	return nil
}

type ChunkExtractService struct {
	template          *types.PromptTemplateStructured
	modelService      interfaces.ModelService
	knowledgeBaseRepo interfaces.KnowledgeBaseRepository
	chunkRepo         interfaces.ChunkRepository
	graphEngine       interfaces.RetrieveGraphRepository
}

func NewChunkExtractService(
	config *config.Config,
	modelService interfaces.ModelService,
	knowledgeBaseRepo interfaces.KnowledgeBaseRepository,
	chunkRepo interfaces.ChunkRepository,
	graphEngine interfaces.RetrieveGraphRepository,
) interfaces.Extracter {
	generator := NewQAPromptGenerator(NewFormater(), config.Extraction)
	ctx := context.Background()
	logger.Debugf(ctx, "chunk extract prompt: %s", generator.Render(ctx, "extract"))
	return &ChunkExtractService{
		template:          config.Extraction,
		modelService:      modelService,
		knowledgeBaseRepo: knowledgeBaseRepo,
		chunkRepo:         chunkRepo,
		graphEngine:       graphEngine,
	}
}

func (s *ChunkExtractService) Extract(ctx context.Context, t *asynq.Task) error {
	var p types.ExtractChunkPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		logger.Errorf(ctx, "failed to unmarshal task payload: %v", err)
		return err
	}
	ctx = logger.WithRequestID(ctx, uuid.New().String())
	ctx = logger.WithField(ctx, "extract_chunk", p.ChunkID)
	ctx = context.WithValue(ctx, types.TenantIDContextKey, p.TenantID)

	chunk, err := s.chunkRepo.GetChunkByID(ctx, p.TenantID, p.ChunkID)
	if err != nil {
		logger.Errorf(ctx, "failed to get chunk: %v", err)
		return err
	}
	chatModel, err := s.modelService.GetChatModel(ctx, p.ModelID)
	if err != nil {
		logger.Errorf(ctx, "failed to get chat model: %v", err)
		return err
	}

	extractor := NewExtractor(chatModel, s.template)
	graph, err := extractor.extract(ctx, chunk)
	if err != nil {
		return err
	}
	if err = s.graphEngine.AddGraph(ctx,
		types.NameSpace{KnowledgeBase: chunk.KnowledgeBaseID, Knowledge: chunk.KnowledgeID},
		[]*types.GraphData{graph},
	); err != nil {
		logger.Errorf(ctx, "failed to add graph: %v", err)
		return err
	}
	gg, _ := json.Marshal(graph)
	logger.Infof(ctx, "extracted graph: %s", string(gg))
	return nil
}

type Extractor struct {
	chat      chat.Chat
	embedding embedding.Embedder
	formater  *Formater
	template  *types.PromptTemplateStructured
	chatOpt   *chat.ChatOptions

	repo   interfaces.ChunkRepository
	engine *retriever.CompositeRetrieveEngine
}

func NewExtractor(
	chatModel chat.Chat,
	template *types.PromptTemplateStructured,
) Extractor {
	think := false
	return Extractor{
		chat:     chatModel,
		formater: NewFormater(),
		template: template,
		chatOpt: &chat.ChatOptions{
			Temperature: 0.3,
			MaxTokens:   4096,
			Thinking:    &think,
		},
	}
}

func (e *Extractor) extract(ctx context.Context, chunk *types.Chunk) (*types.GraphData, error) {
	generator := NewQAPromptGenerator(e.formater, e.template)
	// logger.Infof(ctx, "chat response: %s", generator.Render(ctx, chunk.Content))
	chatResponse, err := e.chat.Chat(ctx, []chat.Message{
		{
			Role:    "user",
			Content: generator.Render(ctx, chunk.Content),
		},
	}, e.chatOpt)
	if err != nil {
		logger.Errorf(ctx, "failed to chat: %v", err)
		return nil, err
	}

	graph, err := e.formater.ParseGraph(ctx, chatResponse.Content)
	if err != nil {
		logger.Errorf(ctx, "failed to parse graph: %v", err)
		return nil, err
	}
	return graph, nil
}

func chunkToIndex(chunk *types.Chunk) *types.IndexInfo {
	index := &types.IndexInfo{
		Content:         chunk.Content,
		SourceID:        chunk.ID,
		SourceType:      types.ChunkSourceType,
		ChunkID:         chunk.ID,
		KnowledgeID:     chunk.KnowledgeID,
		KnowledgeBaseID: chunk.KnowledgeBaseID,
	}
	return index
}

type QAPromptGenerator struct {
	Formater        *Formater
	Template        *types.PromptTemplateStructured
	ExamplesHeading string
	QuestionPrefix  string
	answerPrefix    string
}

func NewQAPromptGenerator(formater *Formater, template *types.PromptTemplateStructured) *QAPromptGenerator {
	return &QAPromptGenerator{
		Formater:        formater,
		Template:        template,
		ExamplesHeading: "Examples",
		QuestionPrefix:  "Q: ",
		answerPrefix:    "A: ",
	}
}

func (qa *QAPromptGenerator) FormatExampleAsText(example types.GraphData) (string, error) {
	question := example.Text
	answer, err := qa.Formater.FormatExtraction(example.Node, example.Relation)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%s\n%s%s\n", qa.QuestionPrefix, question, qa.answerPrefix, answer), nil
}

func (qa *QAPromptGenerator) Render(ctx context.Context, question string) string {
	promptLines := []string{fmt.Sprintf("%s\n", qa.Template.Description)}

	if len(qa.Template.Examples) > 0 {
		promptLines = append(promptLines, qa.ExamplesHeading)
		for _, example := range qa.Template.Examples {
			formatted, err := qa.FormatExampleAsText(example)
			if err != nil {
				return ""
			}
			promptLines = append(promptLines, formatted)
		}
	}
	promptLines = append(promptLines, fmt.Sprintf("%s%s", qa.QuestionPrefix, question))
	promptLines = append(promptLines, qa.answerPrefix)
	return strings.Join(promptLines, "\n")
}

type FormatType string

const (
	FormatTypeJSON FormatType = "json"
	FormatTypeYAML FormatType = "yaml"
)

const (
	_FENCE_START   = "```"
	_LANGUAGE_TAG  = `(?P<lang>[A-Za-z0-9_+-]+)?`
	_FENCE_NEWLINE = `(?:\s*\n)?`
	_FENCE_BODY    = `(?P<body>[\s\S]*?)`
	_FENCE_END     = "```"
)

var _FENCE_RE = regexp.MustCompile(
	_FENCE_START + _LANGUAGE_TAG + _FENCE_NEWLINE + _FENCE_BODY + _FENCE_END,
)

type Formater struct {
	attributeSuffix string
	formatType      FormatType
	useFences       bool
	nodePrefix      string

	relationSource string
	relationTarget string
	relationPrefix string
}

func NewFormater() *Formater {
	return &Formater{
		attributeSuffix: "_attributes",
		formatType:      FormatTypeJSON,
		useFences:       true,
		nodePrefix:      "entity",
		relationSource:  "entity1",
		relationTarget:  "entity2",
		relationPrefix:  "relation",
	}
}

func (f *Formater) FormatExtraction(nodes []*types.GraphNode, relations []*types.GraphRelation) (string, error) {
	items := make([]map[string]interface{}, 0)
	for _, node := range nodes {
		item := map[string]interface{}{
			f.nodePrefix: node.ID,
			fmt.Sprintf("%s%s", f.nodePrefix, f.attributeSuffix): node.Attributes,
		}
		items = append(items, item)
	}
	for _, relation := range relations {
		item := map[string]interface{}{
			f.relationSource: relation.Source.ID,
			f.relationTarget: relation.Target.ID,
			f.relationPrefix: relation.Type,
			fmt.Sprintf("%s%s", f.relationPrefix, f.attributeSuffix): relation.Attributes,
		}
		items = append(items, item)
	}
	formatted := ""
	switch f.formatType {
	default:
		formattedBytes, err := json.MarshalIndent(items, "", "  ")
		if err != nil {
			return "", err
		}
		formatted = string(formattedBytes)
	}
	if f.useFences {
		formatted = f.addFences(formatted)
	}
	return formatted, nil
}

func (f *Formater) parseOutput(ctx context.Context, text string) ([]map[string]interface{}, error) {
	if text == "" {
		return nil, errors.New("Empty or invalid input string.")
	}
	content := f.ExtractContent(ctx, text)

	var parsed interface{}
	var err error
	if f.formatType == FormatTypeJSON {
		err = json.Unmarshal([]byte(content), &parsed)
	}
	if err != nil {
		return nil, fmt.Errorf("Failed to parse %s content: %s", strings.ToUpper(string(f.formatType)), err.Error())
	}
	if parsed == nil {
		return nil, fmt.Errorf("Content must be a list of extractions or a dict.")
	}

	var items []interface{}
	if parsedMap, ok := parsed.(map[string]interface{}); ok {
		items = []interface{}{parsedMap}
	} else if parsedList, ok := parsed.([]interface{}); ok {
		items = parsedList
	} else {
		return nil, fmt.Errorf("Expected list or dict, got %T", parsed)
	}

	itemsList := make([]map[string]interface{}, 0)
	for _, item := range items {
		if itemMap, ok := item.(map[string]interface{}); ok {
			itemsList = append(itemsList, itemMap)
		} else {
			return nil, fmt.Errorf("Each item in the sequence must be a mapping.")
		}
	}
	return itemsList, nil
}

func (f *Formater) ParseGraph(ctx context.Context, text string) (*types.GraphData, error) {
	matchData, err := f.parseOutput(ctx, text)
	if err != nil {
		return nil, err
	}
	if len(matchData) == 0 {
		logger.Debugf(ctx, "Received empty extraction data.")
		return &types.GraphData{}, nil
	}
	mm, _ := json.Marshal(matchData)
	logger.Debugf(ctx, "Parsed graph data: %s", string(mm))

	var nodes []*types.GraphNode
	var relations []*types.GraphRelation

	for _, group := range matchData {
		switch {
		case group[f.nodePrefix] != nil:
			var attributes map[string]string
			attributesKey := f.nodePrefix + f.attributeSuffix
			if attr, ok := group[attributesKey].(map[string]interface{}); ok {
				attributes = make(map[string]string)
				for k, v := range attr {
					attributes[k] = fmt.Sprintf("%v", v)
				}
			}
			nodes = append(nodes, &types.GraphNode{
				ID:         fmt.Sprintf("%v", group[f.nodePrefix]),
				Attributes: attributes,
			})
		case group[f.relationSource] != nil && group[f.relationTarget] != nil:
			var attributes map[string]string
			attributesKey := f.relationPrefix + f.attributeSuffix
			if attr, ok := group[attributesKey].(map[string]interface{}); ok {
				attributes = make(map[string]string)
				for k, v := range attr {
					attributes[k] = fmt.Sprintf("%v", v)
				}
			}
			relations = append(relations, &types.GraphRelation{
				Source:     &types.GraphNode{ID: fmt.Sprintf("%v", group[f.relationSource])},
				Target:     &types.GraphNode{ID: fmt.Sprintf("%v", group[f.relationTarget])},
				Type:       fmt.Sprintf("%v", group[f.relationPrefix]),
				Attributes: attributes,
			})
		default:
			logger.Warnf(ctx, "Unsupported graph group: %v", group)
			continue
		}
	}
	graph := &types.GraphData{
		Node:     nodes,
		Relation: relations,
	}
	f.rebuildGraph(ctx, graph)
	return graph, nil
}

func (f *Formater) rebuildGraph(ctx context.Context, graph *types.GraphData) {
	nodeMap := make(map[string]*types.GraphNode)
	nodes := make([]*types.GraphNode, 0, len(graph.Node))
	for _, node := range graph.Node {
		if prenode, ok := nodeMap[node.ID]; ok {
			logger.Infof(ctx, "Duplicate node ID: %s, merge attribute", node.ID)
			for k, v := range prenode.Attributes {
				node.Attributes[k] = v
			}
			continue
		}
		nodeMap[node.ID] = node
		nodes = append(nodes, node)
	}
	relations := make([]*types.GraphRelation, 0, len(graph.Relation))
	for _, relation := range graph.Relation {
		if relation.Source.ID == relation.Target.ID {
			logger.Infof(ctx, "Duplicate relation, ignore it")
			continue
		}
		if _, ok := nodeMap[relation.Source.ID]; !ok {
			node := &types.GraphNode{ID: relation.Source.ID}
			nodes = append(nodes, node)
			nodeMap[relation.Source.ID] = node
			logger.Infof(ctx, "Add unknown source node ID: %s", relation.Source.ID)
		}
		source := nodeMap[relation.Source.ID]
		if _, ok := nodeMap[relation.Target.ID]; !ok {
			node := &types.GraphNode{ID: relation.Target.ID}
			nodes = append(nodes, node)
			nodeMap[relation.Target.ID] = node
			logger.Infof(ctx, "Add unknown target node ID: %s", relation.Target.ID)
		}
		target := nodeMap[relation.Target.ID]

		if relation.Type == "" {
			relation.Type = f.relationPrefix
		}
		relation.Source = source
		relation.Target = target
		relations = append(relations, relation)
	}
	*graph = types.GraphData{
		Node:     nodes,
		Relation: relations,
	}
}

func (f *Formater) ExtractContent(ctx context.Context, text string) string {
	if !f.useFences {
		return strings.TrimSpace(text)
	}
	validTags := map[FormatType]map[string]struct{}{
		FormatTypeYAML: {"yaml": {}, "yml": {}},
		FormatTypeJSON: {"json": {}},
	}
	matches := _FENCE_RE.FindAllStringSubmatch(text, -1)
	var candidates []string
	for _, match := range matches {
		lang := match[1]
		body := match[2]
		if f.IsValidLanguageTag(lang, validTags) {
			candidates = append(candidates, body)
		}
	}
	switch {
	case len(candidates) == 1:
		return strings.TrimSpace(candidates[0])

	case len(candidates) > 1:
		logger.Warnf(ctx, "multiple candidates found: %d", len(candidates))
		return strings.TrimSpace(candidates[0])

	case len(matches) == 1:
		logger.Debugf(ctx, "no candidate found, use first match without language tag: %s", matches[0][1])
		return strings.TrimSpace(matches[0][2])

	case len(matches) > 1:
		logger.Warnf(ctx, "multiple matches found: %d", len(matches))
		return strings.TrimSpace(matches[0][2])

	default:
		logger.Warnf(ctx, "no match found")
		return strings.TrimSpace(text)
	}
}

func (f *Formater) addFences(content string) string {
	content = strings.TrimSpace(content)
	return fmt.Sprintf("```%s\n%s\n```", f.formatType, content)
}

func (f *Formater) IsValidLanguageTag(lang string, validTags map[FormatType]map[string]struct{}) bool {
	if lang == "" {
		return true
	}
	tag := strings.TrimSpace(strings.ToLower(lang))
	validSet, ok := validTags[f.formatType]
	if !ok {
		return false
	}
	_, exists := validSet[tag]
	return exists
}
