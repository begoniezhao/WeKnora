package handler

import (
	"net/http"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/gin-gonic/gin"
)

// ChatBotHandler 处理 ChatBot 初始化请求
type ChatBotHandler struct {
	svc service.ChatBotService
}

// NewChatBotHandler 构造函数
func NewChatBotHandler(svc service.ChatBotService) *ChatBotHandler {
	return &ChatBotHandler{svc: svc}
}

type initializeChatBotRequest struct {
	AppID     string `json:"app_id"     binding:"required"`
	AppSecret string `json:"app_secret" binding:"required"`
}

// Initialize POST /api/v1/models/chatbot/initialize
func (h *ChatBotHandler) Initialize(c *gin.Context) {
	var req initializeChatBotRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := h.svc.Initialize(c.Request.Context(), req.AppID, req.AppSecret)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// Status GET /api/v1/models/chatbot/status
// 检查当前租户的 ChatBot 凭证是否完好，如需重新初始化则返回 needs_reinit=true
func (h *ChatBotHandler) Status(c *gin.Context) {
	result, err := h.svc.CheckStatus(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}
