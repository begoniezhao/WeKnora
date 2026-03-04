package provider

import "github.com/Tencent/WeKnora/internal/types"

const (
	ProviderChatBot ProviderName = "chatbot"

	// ChatBotBaseURL ChatBot 服务硬编码 Base URL（统一入口，路径由各实现拼接）
	ChatBotBaseURL = "http://<host>:8080"
)

type ChatBotProvider struct{}

func init() {
	Register(&ChatBotProvider{})
}

func (p *ChatBotProvider) Info() ProviderInfo {
	return ProviderInfo{
		Name:        ProviderChatBot,
		DisplayName: "ChatBot",
		Description: "ChatBot 平台，提供 Chat / Embedding / Rerank 模型，使用 APPID+APPSECRET 签名认证",
		DefaultURLs: map[types.ModelType]string{
			types.ModelTypeKnowledgeQA: ChatBotBaseURL,
			types.ModelTypeEmbedding:   ChatBotBaseURL,
			types.ModelTypeRerank:      ChatBotBaseURL,
		},
		ModelTypes: []types.ModelType{
			types.ModelTypeKnowledgeQA,
			types.ModelTypeEmbedding,
			types.ModelTypeRerank,
		},
		RequiresAuth: true,
	}
}

func (p *ChatBotProvider) ValidateConfig(config *Config) error {
	// AppID/AppSecret 通过专用初始化接口写入，此处仅做结构校验
	return nil
}
