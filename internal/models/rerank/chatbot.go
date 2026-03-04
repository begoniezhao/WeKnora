package rerank

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/models/utils"
	"github.com/google/uuid"
)

const chatBotRerankPath = "/api/v1/rerank"

// ChatBotReranker 实现 rerank.Reranker 接口，对接 ChatBot /api/v1/rerank
type ChatBotReranker struct {
	modelName string
	modelID   string
	appID     string
	appSecret string
	baseURL   string
	client    *http.Client
}

// NewChatBotReranker 构造 ChatBotReranker
func NewChatBotReranker(config *RerankerConfig) (*ChatBotReranker, error) {
	if config.AppID == "" {
		return nil, fmt.Errorf("ChatBot reranker: AppID is required")
	}
	if config.AppSecret == "" {
		return nil, fmt.Errorf("ChatBot reranker: AppSecret is required")
	}
	return &ChatBotReranker{
		modelName: config.ModelName,
		modelID:   config.ModelID,
		appID:     config.AppID,
		appSecret: config.AppSecret,
		baseURL:   strings.TrimRight(config.BaseURL, "/"),
		client:    &http.Client{Timeout: 60 * time.Second},
	}, nil
}

type chatBotRerankRequest struct {
	Model     string   `json:"model"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
}

type chatBotRerankResponse struct {
	Results []struct {
		Index          int     `json:"index"`
		RelevanceScore float64 `json:"relevance_score"`
		Document       struct {
			Text string `json:"text"`
		} `json:"document"`
	} `json:"results"`
}

func (r *ChatBotReranker) Rerank(ctx context.Context, query string, documents []string) ([]RankResult, error) {
	reqBody := chatBotRerankRequest{
		Model:     r.modelName,
		Query:     query,
		Documents: documents,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("chatbot reranker: marshal: %w", err)
	}

	requestID := uuid.New().String()
	headers := utils.Sign(r.appID, r.appSecret, requestID, string(bodyBytes))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL+chatBotRerankPath, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("chatbot reranker: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("chatbot reranker: do request: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("chatbot reranker: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("chatbot reranker: status %d: %s", resp.StatusCode, string(respBytes))
	}

	var rerankResp chatBotRerankResponse
	if err := json.Unmarshal(respBytes, &rerankResp); err != nil {
		return nil, fmt.Errorf("chatbot reranker: unmarshal: %w", err)
	}

	results := make([]RankResult, 0, len(rerankResp.Results))
	for _, item := range rerankResp.Results {
		results = append(results, RankResult{
			Index:          item.Index,
			RelevanceScore: item.RelevanceScore,
			Document:       DocumentInfo{Text: item.Document.Text},
		})
	}
	return results, nil
}

func (r *ChatBotReranker) GetModelName() string { return r.modelName }
func (r *ChatBotReranker) GetModelID() string   { return r.modelID }
