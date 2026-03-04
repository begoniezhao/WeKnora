package rerank_test

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/models/rerank"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewChatBotReranker_MissingAppSecret(t *testing.T) {
	_, err := rerank.NewChatBotReranker(&rerank.RerankerConfig{
		ModelID:   "model-id",
		ModelName: "chatbot-rerank",
		BaseURL:   "http://localhost:8080",
		AppID:     "appid",
		AppSecret: "",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AppSecret")
}

func TestNewChatBotReranker_Success(t *testing.T) {
	r, err := rerank.NewChatBotReranker(&rerank.RerankerConfig{
		ModelID:   "model-id",
		ModelName: "chatbot-rerank",
		BaseURL:   "http://localhost:8080",
		AppID:     "appid",
		AppSecret: "secret",
	})
	require.NoError(t, err)
	assert.Equal(t, "chatbot-rerank", r.GetModelName())
}
