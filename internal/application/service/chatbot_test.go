package service_test

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChatBotService_Initialize_EmptyAppID(t *testing.T) {
	svc := service.NewChatBotService(nil, nil)
	_, err := svc.Initialize(context.Background(), "", "secret")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "app_id")
}

func TestChatBotService_Initialize_EmptyAppSecret(t *testing.T) {
	svc := service.NewChatBotService(nil, nil)
	_, err := svc.Initialize(context.Background(), "appid", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "app_secret")
}
