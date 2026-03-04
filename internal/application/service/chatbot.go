package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/infrastructure/crypto"
	"github.com/Tencent/WeKnora/internal/models/provider"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
)

// ChatBotService 处理 ChatBot 厂商的初始化逻辑
type ChatBotService interface {
	Initialize(ctx context.Context, appID, appSecret string) (*InitializeResult, error)
	// CheckStatus 检查当前租户的 ChatBot 凭证是否可正常解密
	// needsReinit=true 表示加密状态已损坏（salt 变更等），需要用户重新填写凭证
	CheckStatus(ctx context.Context) (*ChatBotStatusResult, error)
}

// ChatBotStatusResult 状态检查结果
type ChatBotStatusResult struct {
	HasModels     bool   `json:"has_models"`      // 是否已配置 chatbot 模型
	NeedsReinit   bool   `json:"needs_reinit"`    // 是否需要重新初始化（凭证损坏）
	Reason        string `json:"reason,omitempty"` // 需要重新初始化的原因
}

// InitializeResult 返回每个模型的操作结果
type InitializeResult struct {
	Models []ModelAction `json:"models"`
}

// ModelAction 单个模型的操作结果
type ModelAction struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Action string `json:"action"` // "created" | "updated"
}

type chatBotService struct {
	repo      interfaces.ModelRepository
	cryptoSvc *crypto.CryptoService
}

// NewChatBotService 构造 ChatBotService
func NewChatBotService(repo interfaces.ModelRepository, cryptoSvc *crypto.CryptoService) ChatBotService {
	return &chatBotService{
		repo:      repo,
		cryptoSvc: cryptoSvc,
	}
}

// chatBotModelDefs 三个内置模型的静态定义
var chatBotModelDefs = []struct {
	name      string
	modelType types.ModelType
}{
	{name: "chatbot-chat", modelType: types.ModelTypeKnowledgeQA},
	{name: "chatbot-embedding", modelType: types.ModelTypeEmbedding},
	{name: "chatbot-rerank", modelType: types.ModelTypeRerank},
}

func (s *chatBotService) Initialize(ctx context.Context, appID, appSecret string) (*InitializeResult, error) {
	if appID == "" {
		return nil, fmt.Errorf("app_id is required")
	}
	if appSecret == "" {
		return nil, fmt.Errorf("app_secret is required")
	}

	// 步骤1：连通性验证（用明文 secret 构造临时 Chat 客户端，发一条 ping 消息）
	if err := s.pingChatBot(ctx, appID, appSecret); err != nil {
		return nil, fmt.Errorf("ChatBot 服务连通性验证失败：%w", err)
	}

	// 步骤2：加密 appSecret
	encryptedSecret, err := s.cryptoSvc.EncryptString(appSecret)
	if err != nil {
		return nil, fmt.Errorf("加密 AppSecret 失败：%w", err)
	}

	tenantID := types.MustTenantIDFromContext(ctx)
	result := &InitializeResult{}

	for _, def := range chatBotModelDefs {
		action, err := s.upsertModel(ctx, tenantID, def.name, def.modelType, appID, encryptedSecret)
		if err != nil {
			return nil, fmt.Errorf("upsert 模型 %s 失败：%w", def.name, err)
		}
		result.Models = append(result.Models, ModelAction{
			Name:   def.name,
			Type:   string(def.modelType),
			Action: action,
		})
	}

	return result, nil
}

// pingChatBot 调用 ChatBot 服务的 GET /health 接口验证可达性
// ChatBot 健康检查端点：GET /health -> {"status":"ok","timestamp":...}
func (s *chatBotService) pingChatBot(ctx context.Context, appID, appSecret string) error {
	baseURL := strings.TrimRight(provider.ChatBotBaseURL, "/")
	healthURL := baseURL + "/health"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return fmt.Errorf("创建健康检查请求失败: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("ChatBot 服务不可达 (%s): %w", healthURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ChatBot 健康检查返回非 200 状态码: %d", resp.StatusCode)
	}
	return nil
}

// CheckStatus 检查 ChatBot 凭证是否可正常解密
func (s *chatBotService) CheckStatus(ctx context.Context) (*ChatBotStatusResult, error) {
	tenantID := types.MustTenantIDFromContext(ctx)

	// 查找任意一个 chatbot 模型，取其 AppSecret 尝试解密
	for _, def := range chatBotModelDefs {
		models, err := s.repo.List(ctx, tenantID, def.modelType, types.ModelSourceRemote)
		if err != nil {
			continue
		}
		for _, m := range models {
			if m.Parameters.Provider != string(provider.ProviderChatBot) {
				continue
			}
			// 找到 chatbot 模型
			encryptedSecret := m.Parameters.AppSecret
			if encryptedSecret == "" {
				return &ChatBotStatusResult{
					HasModels:   true,
					NeedsReinit: true,
					Reason:      "ChatBot 凭证为空，请重新填写 APPID 和 APPSECRET",
				}, nil
			}
			// 尝试解密
			if _, decErr := s.cryptoSvc.DecryptString(encryptedSecret); decErr != nil {
				return &ChatBotStatusResult{
					HasModels:   true,
					NeedsReinit: true,
					Reason:      "ChatBot 凭证解密失败（服务重启后加密密钥已变更），请重新填写 APPID 和 APPSECRET",
				}, nil
			}
			return &ChatBotStatusResult{HasModels: true, NeedsReinit: false}, nil
		}
	}

	// 没有找到 chatbot 模型
	return &ChatBotStatusResult{HasModels: false, NeedsReinit: false}, nil
}

// upsertModel 按名称检测是否已存在，存在则更新凭证，否则创建
func (s *chatBotService) upsertModel(
	ctx context.Context,
	tenantID uint64,
	name string,
	modelType types.ModelType,
	appID, encryptedSecret string,
) (string, error) {
	// 列出该类型的远程模型，过滤出 chatbot provider
	models, err := s.repo.List(ctx, tenantID, modelType, types.ModelSourceRemote)
	if err != nil {
		return "", err
	}

	var existing *types.Model
	for _, m := range models {
		if m.Name == name && m.Parameters.Provider == string(provider.ProviderChatBot) {
			existing = m
			break
		}
	}

	// 先清除同类型其他模型的默认标记
	excludeID := ""
	if existing != nil {
		excludeID = existing.ID
	}
	if err := s.repo.ClearDefaultByType(ctx, uint(tenantID), modelType, excludeID); err != nil {
		return "", err
	}

	if existing != nil {
		// 更新凭证和默认标记
		existing.Parameters.AppID = appID
		existing.Parameters.AppSecret = encryptedSecret
		existing.IsDefault = true
		if err := s.repo.Update(ctx, existing); err != nil {
			return "", err
		}
		return "updated", nil
	}

	// 创建新模型
	newModel := &types.Model{
		ID:        uuid.New().String(),
		TenantID:  tenantID,
		Name:      name,
		Type:      modelType,
		Source:    types.ModelSourceRemote,
		IsBuiltin: true,
		IsDefault: true,
		Status:    types.ModelStatusActive,
		Parameters: types.ModelParameters{
			Provider:  string(provider.ProviderChatBot),
			BaseURL:   provider.ChatBotBaseURL,
			AppID:     appID,
			AppSecret: encryptedSecret,
		},
	}
	if err := s.repo.Create(ctx, newModel); err != nil {
		return "", err
	}
	return "created", nil
}
