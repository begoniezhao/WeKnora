package chat_test

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewChatBotChat_MissingAppID(t *testing.T) {
	_, err := chat.NewChatBotChat(&chat.ChatConfig{
		ModelID:   "model-id",
		ModelName: "chatbot-chat",
		BaseURL:   "http://localhost:8080",
		AppID:     "",
		AppSecret: "secret",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AppID")
}

func TestNewChatBotChat_MissingAppSecret(t *testing.T) {
	_, err := chat.NewChatBotChat(&chat.ChatConfig{
		ModelID:   "model-id",
		ModelName: "chatbot-chat",
		BaseURL:   "http://localhost:8080",
		AppID:     "appid",
		AppSecret: "",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AppSecret")
}

func TestNewChatBotChat_Success(t *testing.T) {
	c, err := chat.NewChatBotChat(&chat.ChatConfig{
		ModelID:   "model-id",
		ModelName: "chatbot-chat",
		BaseURL:   "http://localhost:8080",
		AppID:     "appid",
		AppSecret: "secret",
	})
	require.NoError(t, err)
	assert.Equal(t, "chatbot-chat", c.GetModelName())
	assert.Equal(t, "model-id", c.GetModelID())
}
