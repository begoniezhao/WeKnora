package chat

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
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
)

const chatBotChatPath = "/api/v1/chat/completions"

// ChatBotChat 实现 chat.Chat 接口，对接 ChatBot /api/v1/chat/completions
type ChatBotChat struct {
	modelName string
	modelID   string
	appID     string
	appSecret string // 已解密明文
	baseURL   string
	client    *http.Client
}

// NewChatBotChat 构造 ChatBotChat 实例
func NewChatBotChat(config *ChatConfig) (*ChatBotChat, error) {
	if config.AppID == "" {
		return nil, fmt.Errorf("ChatBot provider: AppID is required")
	}
	if config.AppSecret == "" {
		return nil, fmt.Errorf("ChatBot provider: AppSecret is required")
	}
	baseURL := strings.TrimRight(config.BaseURL, "/")
	return &ChatBotChat{
		modelName: config.ModelName,
		modelID:   config.ModelID,
		appID:     config.AppID,
		appSecret: config.AppSecret,
		baseURL:   baseURL,
		client:    &http.Client{Timeout: 120 * time.Second},
	}, nil
}

type chatBotChatRequest struct {
	Model       string           `json:"model"`
	Messages    []chatBotMessage `json:"messages"`
	Stream      bool             `json:"stream"`
	MaxTokens   int              `json:"max_tokens,omitempty"`
	Temperature float64          `json:"temperature,omitempty"`
	TopP        float64          `json:"top_p,omitempty"`
}

type chatBotMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatBotChatResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// Chat 非流式聊天
func (c *ChatBotChat) Chat(ctx context.Context, messages []Message, opts *ChatOptions) (*types.ChatResponse, error) {
	reqBody := chatBotChatRequest{
		Model:    c.modelName,
		Messages: convertToChatBotMessages(messages),
		Stream:   false,
	}
	if opts != nil {
		reqBody.Temperature = opts.Temperature
		reqBody.TopP = opts.TopP
		reqBody.MaxTokens = opts.MaxTokens
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("chatbot chat: marshal request: %w", err)
	}

	requestID := uuid.New().String()
	headers := utils.Sign(c.appID, c.appSecret, requestID, string(bodyBytes))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+chatBotChatPath, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("chatbot chat: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("chatbot chat: do request: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("chatbot chat: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("chatbot chat: unexpected status %d: %s", resp.StatusCode, string(respBytes))
	}

	var chatResp chatBotChatResponse
	if err := json.Unmarshal(respBytes, &chatResp); err != nil {
		return nil, fmt.Errorf("chatbot chat: unmarshal response: %w", err)
	}

	content := ""
	finishReason := ""
	if len(chatResp.Choices) > 0 {
		content = chatResp.Choices[0].Message.Content
		finishReason = chatResp.Choices[0].FinishReason
	}

	return &types.ChatResponse{
		Content:      content,
		FinishReason: finishReason,
		Usage: struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		}{
			PromptTokens:     chatResp.Usage.PromptTokens,
			CompletionTokens: chatResp.Usage.CompletionTokens,
			TotalTokens:      chatResp.Usage.TotalTokens,
		},
	}, nil
}

// ChatStream 流式聊天（SSE）
func (c *ChatBotChat) ChatStream(ctx context.Context, messages []Message, opts *ChatOptions) (<-chan types.StreamResponse, error) {
	reqBody := chatBotChatRequest{
		Model:    c.modelName,
		Messages: convertToChatBotMessages(messages),
		Stream:   true,
	}
	if opts != nil {
		reqBody.Temperature = opts.Temperature
		reqBody.TopP = opts.TopP
		reqBody.MaxTokens = opts.MaxTokens
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("chatbot stream: marshal request: %w", err)
	}

	requestID := uuid.New().String()
	headers := utils.Sign(c.appID, c.appSecret, requestID, string(bodyBytes))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+chatBotChatPath, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("chatbot stream: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("chatbot stream: do request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("chatbot stream: unexpected status %d: %s", resp.StatusCode, string(body))
	}

	ch := make(chan types.StreamResponse, 16)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		processSSEStream(ctx, resp.Body, ch)
	}()

	return ch, nil
}

func (c *ChatBotChat) GetModelName() string { return c.modelName }
func (c *ChatBotChat) GetModelID() string   { return c.modelID }

func convertToChatBotMessages(messages []Message) []chatBotMessage {
	result := make([]chatBotMessage, 0, len(messages))
	for _, m := range messages {
		result = append(result, chatBotMessage{Role: m.Role, Content: m.Content})
	}
	return result
}

// processSSEStream 解析 SSE 流，逐块发送到 channel
func processSSEStream(ctx context.Context, body io.Reader, ch chan<- types.StreamResponse) {
	buf := make([]byte, 4096)
	var pending strings.Builder
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		n, err := body.Read(buf)
		if n > 0 {
			pending.Write(buf[:n])
			for {
				s := pending.String()
				idx := strings.Index(s, "\n\n")
				if idx < 0 {
					break
				}
				line := strings.TrimSpace(s[:idx])
				pending.Reset()
				pending.WriteString(s[idx+2:])
				if !strings.HasPrefix(line, "data:") {
					continue
				}
				data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				if data == "[DONE]" {
					ch <- types.StreamResponse{Done: true}
					return
				}
				var chunk struct {
					Choices []struct {
						Delta struct {
							Content string `json:"content"`
						} `json:"delta"`
						FinishReason *string `json:"finish_reason"`
					} `json:"choices"`
				}
				if jsonErr := json.Unmarshal([]byte(data), &chunk); jsonErr != nil {
					continue
				}
				if len(chunk.Choices) > 0 {
					ch <- types.StreamResponse{
						Content: chunk.Choices[0].Delta.Content,
					}
				}
			}
		}
		if err != nil {
			if err != io.EOF {
				ch <- types.StreamResponse{Done: true}
			}
			return
		}
	}
}
