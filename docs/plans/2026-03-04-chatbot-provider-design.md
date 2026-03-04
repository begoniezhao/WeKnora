# ChatBot Provider 集成设计文档

**日期：** 2026-03-04  
**状态：** 已确认

---

## 背景

集成一个使用 APPID + APPSECRET 签名认证的第三方 ChatBot 厂商（接口文档见 `docs/chatbot-api.md`）。该厂商提供三类接口：

| 接口 | 路径 | 对应现有类型 |
|------|------|------------|
| Chat Completions | `POST /api/v1/chat/completions` | `chat.Chat` |
| Embeddings | `POST /api/v1/embeddings` | `embedding.Embedder` |
| Rerank | `POST /api/v1/rerank` | `rerank.Reranker` |

**认证特殊性：** 不使用标准 Bearer API Key，而是每次请求动态生成签名头（`X-APPID`、`X-Timestamp`、`X-Nonce`、`X-Signature`），签名算法为 MD5。

---

## 目标

1. 用户在前端模型设置页填写一次 APPID 和 APPSECRET
2. 后端自动创建/更新三个内置模型（分别对应 KnowledgeQA、Embedding、Rerank 类型），并设为各类型默认
3. ChatBot 厂商的三个接口直接实现现有的 `chat.Chat`、`embedding.Embedder`、`rerank.Reranker` 接口，调用逻辑与其他厂商完全一致
4. APPSECRET 加密存储，调用时动态解密加签

---

## 整体流程

```
前端设置页（新增 ChatBot 配置区块）
  ├── 输入 APPID（必填）
  ├── 输入 APPSECRET（必填，密码框）
  └── 点击「保存并初始化」
         ↓
POST /api/v1/models/chatbot/initialize
         ↓
ChatBotHandler.Initialize()
         ↓
ChatBotService.Initialize(ctx, appID, appSecret)
  ├── 1. 用 APPID+APPSECRET 构造签名，调用 Chat 接口验证连通性
  ├── 2. 加密 appSecret（crypto.EncryptString）
  ├── 3. Upsert 三个模型（ListModels 按类型+provider 过滤，存在则 Update，否则 Create）
  │      ├── chatbot-chat      → KnowledgeQA，IsBuiltin=true，IsDefault=true
  │      ├── chatbot-embedding → Embedding，   IsBuiltin=true，IsDefault=true
  │      └── chatbot-rerank    → Rerank，      IsBuiltin=true，IsDefault=true
  └── 4. 返回 {models: [{name, type, action: "created"|"updated"}]}
         ↓
前端刷新模型列表 → 三个模型出现在对应分类下
```

---

## 数据模型变更

### `ModelParameters` 新增字段

**文件：** `internal/types/model.go`

```go
type ModelParameters struct {
    BaseURL             string              `yaml:"base_url"             json:"base_url"`
    APIKey              string              `yaml:"api_key"              json:"api_key"`
    InterfaceType       string              `yaml:"interface_type"       json:"interface_type"`
    EmbeddingParameters EmbeddingParameters `yaml:"embedding_parameters" json:"embedding_parameters"`
    ParameterSize       string              `yaml:"parameter_size"       json:"parameter_size"`
    Provider            string              `yaml:"provider"             json:"provider"`
    ExtraConfig         map[string]string   `yaml:"extra_config"         json:"extra_config"`
    // 新增：ChatBot 厂商专用凭证
    AppID     string `yaml:"app_id,omitempty"     json:"app_id,omitempty"`
    AppSecret string `yaml:"app_secret,omitempty" json:"app_secret,omitempty"` // 加密存储
}
```

`AppSecret` 调用 `crypto.CryptoService.EncryptString()` 加密后写入，读取时调用 `DecryptString()` 解密，仅在内存中用于签名计算，不通过 API 响应返回。

### 三个内置模型的固定属性

| 字段 | chatbot-chat | chatbot-embedding | chatbot-rerank |
|------|-------------|-------------------|----------------|
| `Name` | `chatbot-chat` | `chatbot-embedding` | `chatbot-rerank` |
| `Type` | `KnowledgeQA` | `Embedding` | `Rerank` |
| `Source` | `remote` | `remote` | `remote` |
| `IsBuiltin` | `true` | `true` | `true` |
| `IsDefault` | `true` | `true` | `true` |
| `Parameters.Provider` | `chatbot` | `chatbot` | `chatbot` |
| `Parameters.BaseURL` | 硬编码常量 | 硬编码常量 | 硬编码常量 |
| `Parameters.AppID` | 用户填写 | 同上 | 同上 |
| `Parameters.AppSecret` | 加密存储 | 同上 | 同上 |

---

## 新增文件

### 1. `internal/models/provider/chatbot.go`

注册 `chatbot` Provider 到现有注册表，前端 Provider 列表通过 API 动态获取时也会包含该条目。

```go
const ProviderChatBot ProviderName = "chatbot"

type ChatBotProvider struct{}

func init() { Register(&ChatBotProvider{}) }

func (p *ChatBotProvider) Info() ProviderInfo {
    return ProviderInfo{
        Name:        ProviderChatBot,
        DisplayName: "ChatBot",
        Description: "ChatBot 平台，提供 Chat / Embedding / Rerank",
        DefaultURLs: map[types.ModelType]string{
            types.ModelTypeKnowledgeQA: chatBotBaseURL,
            types.ModelTypeEmbedding:   chatBotBaseURL,
            types.ModelTypeRerank:      chatBotBaseURL,
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
    // AppID/AppSecret 通过专用初始化接口写入，此处做基础校验即可
    return nil
}
```

### 2. `internal/models/utils/signer.go`

实现 ChatBot 文档中描述的 MD5 签名算法，供 Chat / Embedding / Rerank 三个实现共用。

```go
// Sign 生成 ChatBot API 所需的签名请求头
// appID, appSecret: 凭证（appSecret 为调用前已解密的明文）
// requestID: UUID，每次请求唯一
// bodyJSON: 请求体 JSON 字符串，空请求体传 "{}"
// 返回包含以下 key 的 map：
//   "X-APPID", "X-Request-ID", "X-Timestamp", "X-Nonce", "X-Signature"
func Sign(appID, appSecret, requestID, bodyJSON string) map[string]string
```

签名步骤（严格按文档）：

1. 对 `bodyJSON` 计算 MD5，取十六进制小写 → `body` 参数值
2. 生成随机 Nonce（8~32 位字母数字）
3. 取当前 Unix 时间戳（秒）
4. 组装参数 map，key 全部小写：`x-appid`、`x-request-id`、`x-timestamp`、`x-nonce`、`body`
5. 按字典序排序，key 和 value 分别 RFC3986 编码（保留 `A-Z a-z 0-9 - _ . ~`），拼接为 `k=v&k=v...`
6. 末尾追加 `&appsecret=<appSecret>`，对整体字符串计算 MD5，取十六进制小写作为签名值

### 3. `internal/models/chat/chatbot.go`

实现 `chat.Chat` 接口，对接 ChatBot 的 `/api/v1/chat/completions`。

```go
type ChatBotChat struct {
    modelName string
    modelID   string
    appID     string
    appSecret string // 已解密明文
    baseURL   string
    client    *http.Client
}

func NewChatBotChat(config *ChatConfig) (*ChatBotChat, error)

// 实现 chat.Chat 接口
func (c *ChatBotChat) Chat(ctx context.Context, messages []Message, opts *ChatOptions) (*types.ChatResponse, error)
func (c *ChatBotChat) ChatStream(ctx context.Context, messages []Message, opts *ChatOptions) (<-chan types.StreamResponse, error)
func (c *ChatBotChat) GetModelName() string
func (c *ChatBotChat) GetModelID() string
```

请求构造：将内部 `[]chat.Message` 转为 ChatBot 格式（与 OpenAI 兼容），每次调用 `utils.Sign()` 生成签名头，通过原生 `http.Client` 发送。流式响应解析 SSE 格式，逐块写入 channel。

### 4. `internal/models/embedding/chatbot.go`

实现 `embedding.Embedder` 接口，对接 ChatBot 的 `/api/v1/embeddings`。

```go
type ChatBotEmbedder struct {
    modelName  string
    modelID    string
    appID      string
    appSecret  string
    baseURL    string
    dimensions int
    client     *http.Client
    embedding.EmbedderPooler
}

func NewChatBotEmbedder(config *Config) (*ChatBotEmbedder, error)

// 实现 embedding.Embedder 接口
func (e *ChatBotEmbedder) Embed(ctx context.Context, text string) ([]float32, error)
func (e *ChatBotEmbedder) BatchEmbed(ctx context.Context, texts []string) ([][]float32, error)
func (e *ChatBotEmbedder) GetModelName() string
func (e *ChatBotEmbedder) GetDimensions() int
func (e *ChatBotEmbedder) GetModelID() string
```

### 5. `internal/models/rerank/chatbot.go`

实现 `rerank.Reranker` 接口，对接 ChatBot 的 `/api/v1/rerank`。

```go
type ChatBotReranker struct {
    modelName string
    modelID   string
    appID     string
    appSecret string
    baseURL   string
    client    *http.Client
}

func NewChatBotReranker(config *RerankerConfig) (*ChatBotReranker, error)

// 实现 rerank.Reranker 接口
func (r *ChatBotReranker) Rerank(ctx context.Context, query string, documents []string) ([]RankResult, error)
func (r *ChatBotReranker) GetModelName() string
func (r *ChatBotReranker) GetModelID() string
```

响应中的 `results` 按 `relevance_score` 降序，直接映射到标准 `RankResult{Index, Document, RelevanceScore}`。

### 6. `internal/application/service/chatbot.go`

初始化服务，被 Handler 调用，处理连通性验证和 Upsert 逻辑。

```go
type ChatBotService interface {
    Initialize(ctx context.Context, appID, appSecret string) (*InitializeResult, error)
}

type InitializeResult struct {
    Models []ModelAction `json:"models"`
}

type ModelAction struct {
    Name   string `json:"name"`
    Type   string `json:"type"`
    Action string `json:"action"` // "created" | "updated"
}
```

`Initialize` 执行步骤：

1. 用明文 appSecret 构造临时签名，向 ChatBot Chat 接口发一条简短 ping 消息，验证连通性；失败则直接返回错误，不写入任何数据
2. 调用 `crypto.EncryptString(appSecret)` 得到加密后的 `encryptedSecret`
3. 对三个模型依次 Upsert：
   - 调用 `repo.List(ctx, tenantID, modelType, "remote")` 后在内存中过滤 `Parameters.Provider == "chatbot"` 的记录
   - 存在 → `repo.Update()`（仅更新 AppID / AppSecret 字段）
   - 不存在 → `repo.Create()`（写入全部字段，`IsBuiltin=true`）
   - 无论 Create/Update，都调用 `repo.ClearDefaultByType()` 清除同类型其他模型的默认标记，再将该模型 `IsDefault=true` 写入
4. 返回 `InitializeResult`，列出每个模型的操作结果

### 7. `internal/handler/chatbot.go`

```go
type ChatBotHandler struct {
    svc service.ChatBotService
}

type InitializeChatBotRequest struct {
    AppID     string `json:"app_id"     binding:"required"`
    AppSecret string `json:"app_secret" binding:"required"`
}

// POST /api/v1/models/chatbot/initialize
func (h *ChatBotHandler) Initialize(c *gin.Context)
```

响应示例（200）：

```json
{
  "models": [
    {"name": "chatbot-chat",      "type": "KnowledgeQA", "action": "created"},
    {"name": "chatbot-embedding", "type": "Embedding",   "action": "updated"},
    {"name": "chatbot-rerank",    "type": "Rerank",       "action": "created"}
  ]
}
```

连通性验证失败（400）：

```json
{ "error": "无法连接到 ChatBot 服务：<detail>" }
```

---

## 现有文件修改

### `internal/models/chat/chat.go`（工厂函数）

在 `NewRemoteChat()` 的 Provider 路由 switch 中新增 `chatbot` case：

```go
case provider.ProviderChatBot:
    appSecret, err := cryptoSvc.DecryptString(config.AppSecret)
    if err != nil {
        return nil, err
    }
    return NewChatBotChat(&ChatConfig{
        ModelID:   config.ModelID,
        ModelName: config.ModelName,
        BaseURL:   config.BaseURL,
        AppID:     config.AppID,
        AppSecret: appSecret,
    })
```

### `internal/models/embedding/embedder.go`（工厂函数）

在 `NewEmbedder()` 的 Provider 路由 switch 中新增：

```go
case string(provider.ProviderChatBot):
    appSecret, err := cryptoSvc.DecryptString(config.AppSecret)
    if err != nil {
        return nil, err
    }
    return NewChatBotEmbedder(&Config{
        ModelID:    config.ModelID,
        ModelName:  config.ModelName,
        BaseURL:    config.BaseURL,
        AppID:      config.AppID,
        AppSecret:  appSecret,
        Dimensions: config.Dimensions,
    })
```

### `internal/models/rerank/reranker.go`（工厂函数）

在 `NewReranker()` 的 Provider 路由 switch 中新增：

```go
case string(provider.ProviderChatBot):
    appSecret, err := cryptoSvc.DecryptString(config.AppSecret)
    if err != nil {
        return nil, err
    }
    return NewChatBotReranker(&RerankerConfig{
        ModelID:   config.ModelID,
        ModelName: config.ModelName,
        BaseURL:   config.BaseURL,
        AppID:     config.AppID,
        AppSecret: appSecret,
    })
```

### `internal/application/service/model.go`

在 `GetChatModel`、`GetEmbeddingModel`、`GetRerankModel` 构造 Config 时，将新增字段透传：

```go
// 示例：GetChatModel
chat.NewChat(&chat.ChatConfig{
    // ... 现有字段 ...
    AppID:     model.Parameters.AppID,     // 新增
    AppSecret: model.Parameters.AppSecret, // 新增（加密值，在 chat 层解密）
}, s.ollamaService)
```

`GetEmbeddingModel` 和 `GetRerankModel` 同理。

### `internal/router/router.go`

在认证中间件之后，model 路由组附近注册新路由：

```go
r.POST("/models/chatbot/initialize", chatBotHandler.Initialize)
```

同时在 `wire` 或手动依赖注入处注册 `ChatBotHandler`。

---

## 前端变更

**文件：** `frontend/src/views/settings/ModelSettings.vue`

在模型设置页顶部新增 **ChatBot 配置** 区块：

```
┌─────────────────────────────────────────────┐
│  ChatBot 厂商配置                            │
│                                             │
│  APPID     [________________]               │
│  APPSECRET [················]  （密码框）    │
│                                             │
│            [  保存并初始化  ]                │
│                                             │
│  提示：提交后将自动创建/更新三个模型并设为默认  │
└─────────────────────────────────────────────┘
```

**交互逻辑：**

- 点击「保存并初始化」→ 调用 `POST /api/v1/models/chatbot/initialize`
- 成功 → toast 提示"初始化成功" + 刷新模型列表
- 失败 → toast 显示后端返回的错误信息
- 页面刷新后输入框为空（APPID/SECRET 不回填，已存储于模型参数中）

---

## Config 结构扩展

`ChatConfig`、`embedding.Config`、`RerankerConfig` 各新增两个字段，用于透传凭证：

```go
AppID     string // ChatBot APPID
AppSecret string // ChatBot APPSECRET（加密值，实现层负责解密）
```

---

## 安全要点

| 项目 | 处理方式 |
|------|---------|
| AppSecret 写入 | `crypto.EncryptString()` 加密后存入 `ModelParameters.AppSecret` |
| AppSecret 读取 | 在 Chat/Embedding/Rerank 工厂函数中调用 `crypto.DecryptString()`，明文仅在内存中用于签名 |
| API 响应 | 内置模型复用现有 `hideSensitiveInfo()` 逻辑，`AppID`/`AppSecret` 不返回给前端 |
| 连通性校验 | 写入数据库前必须通过签名验证，失败则不写入任何数据 |

---

## 不涉及的改动

- 不修改现有模型创建/删除/列表的 UI 交互逻辑
- 不新增数据库表，仅扩展 `ModelParameters` 的 JSONB 字段（`app_id`、`app_secret`）
- 不影响 KnowledgeBase 已绑定的模型配置（`is_default` 更新只影响新建会话的默认选择）
- 不引入新的模型类型（复用 KnowledgeQA / Embedding / Rerank）
