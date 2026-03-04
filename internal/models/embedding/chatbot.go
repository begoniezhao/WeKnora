package embedding

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

const chatBotEmbedPath = "/api/v1/embeddings"

// ChatBotEmbedder 实现 embedding.Embedder 接口，对接 ChatBot /api/v1/embeddings
type ChatBotEmbedder struct {
	modelName  string
	modelID    string
	appID      string
	appSecret  string
	baseURL    string
	dimensions int
	client     *http.Client
	EmbedderPooler
}

// NewChatBotEmbedder 构造 ChatBotEmbedder
func NewChatBotEmbedder(config Config) (*ChatBotEmbedder, error) {
	if config.AppID == "" {
		return nil, fmt.Errorf("ChatBot embedder: AppID is required")
	}
	if config.AppSecret == "" {
		return nil, fmt.Errorf("ChatBot embedder: AppSecret is required")
	}
	return &ChatBotEmbedder{
		modelName:  config.ModelName,
		modelID:    config.ModelID,
		appID:      config.AppID,
		appSecret:  config.AppSecret,
		baseURL:    strings.TrimRight(config.BaseURL, "/"),
		dimensions: config.Dimensions,
		client:     &http.Client{Timeout: 60 * time.Second},
	}, nil
}

type chatBotEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type chatBotEmbedResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

func (e *ChatBotEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	results, err := e.BatchEmbed(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("chatbot embedder: empty response")
	}
	return results[0], nil
}

func (e *ChatBotEmbedder) BatchEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	reqBody := chatBotEmbedRequest{Model: e.modelName, Input: texts}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("chatbot embedder: marshal: %w", err)
	}

	requestID := uuid.New().String()
	headers := utils.Sign(e.appID, e.appSecret, requestID, string(bodyBytes))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+chatBotEmbedPath, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("chatbot embedder: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("chatbot embedder: do request: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("chatbot embedder: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("chatbot embedder: status %d: %s", resp.StatusCode, string(respBytes))
	}

	var embedResp chatBotEmbedResponse
	if err := json.Unmarshal(respBytes, &embedResp); err != nil {
		return nil, fmt.Errorf("chatbot embedder: unmarshal: %w", err)
	}

	result := make([][]float32, len(texts))
	for _, item := range embedResp.Data {
		if item.Index < len(result) {
			result[item.Index] = item.Embedding
		}
	}
	return result, nil
}

func (e *ChatBotEmbedder) BatchEmbedWithPool(ctx context.Context, model Embedder, texts []string) ([][]float32, error) {
	return e.BatchEmbed(ctx, texts)
}

func (e *ChatBotEmbedder) GetModelName() string { return e.modelName }
func (e *ChatBotEmbedder) GetModelID() string   { return e.modelID }
func (e *ChatBotEmbedder) GetDimensions() int   { return e.dimensions }
