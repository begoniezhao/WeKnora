package embedding_test

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewChatBotEmbedder_MissingAppID(t *testing.T) {
	_, err := embedding.NewChatBotEmbedder(embedding.Config{
		ModelID:   "model-id",
		ModelName: "chatbot-embedding",
		BaseURL:   "http://localhost:8080",
		AppID:     "",
		AppSecret: "secret",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AppID")
}

func TestNewChatBotEmbedder_Success(t *testing.T) {
	e, err := embedding.NewChatBotEmbedder(embedding.Config{
		ModelID:    "model-id",
		ModelName:  "chatbot-embedding",
		BaseURL:    "http://localhost:8080",
		AppID:      "appid",
		AppSecret:  "secret",
		Dimensions: 1536,
	})
	require.NoError(t, err)
	assert.Equal(t, "chatbot-embedding", e.GetModelName())
	assert.Equal(t, 1536, e.GetDimensions())
}
