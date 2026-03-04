# ChatBot Provider 集成实现计划

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 集成 ChatBot 厂商，实现 APPID+APPSECRET 签名认证，提供 Chat/Embedding/Rerank 三类模型，用户在前端填写一次凭证后自动创建三个内置默认模型。

**Architecture:** 新增 `chatbot` provider 注册到现有 Provider 注册表；三个接口分别实现 `chat.Chat`、`embedding.Embedder`、`rerank.Reranker` 接口；签名工具放到 `internal/models/utils/`；新增专用初始化接口 `POST /api/v1/models/chatbot/initialize` 完成 Upsert + 设默认逻辑。

**Tech Stack:** Go (Gin), MD5 签名, AES-256 加密 (现有 crypto 模块), Vue 3 + TDesign UI

---

## Task 1: 扩展 ModelParameters，新增 AppID/AppSecret 字段

**Files:**
- Modify: `internal/types/model.go`

**Step 1: 读取当前 ModelParameters 定义**

```bash
grep -n "ModelParameters" internal/types/model.go
```

**Step 2: 在 ModelParameters 结构体末尾添加两个字段**

找到 `internal/types/model.go` 中的 `ModelParameters` 结构体，在 `ExtraConfig` 字段后追加：

```go
// ChatBot 厂商专用凭证
AppID     string `yaml:"app_id,omitempty"     json:"app_id,omitempty"`
AppSecret string `yaml:"app_secret,omitempty" json:"app_secret,omitempty"` // AES-256 加密存储
```

**Step 3: 验证编译通过**

```bash
go build ./internal/types/...
```

Expected: 无报错

**Step 4: Commit**

```bash
git add internal/types/model.go
git commit -m "feat: add AppID/AppSecret fields to ModelParameters for ChatBot provider"
```

---

## Task 2: 新增 ChatBot Provider 注册

**Files:**
- Create: `internal/models/provider/chatbot.go`

**Step 1: 查看现有 provider 常量列表结尾位置**

```bash
grep -n "ProviderName\|Provider[A-Z]" internal/models/provider/provider.go | head -40
```

**Step 2: 创建 `internal/models/provider/chatbot.go`**

```go
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
```

> **注意：** 将 `ChatBotBaseURL` 中的 `<host>:8080` 替换为实际 ChatBot 服务地址。

**Step 3: 同时在 `provider.go` 的 `AllProviders` 列表中追加 `ProviderChatBot`**

找到 `AllProviders` 变量定义，在末尾添加 `ProviderChatBot`。

**Step 4: 验证编译**

```bash
go build ./internal/models/provider/...
```

**Step 5: Commit**

```bash
git add internal/models/provider/chatbot.go internal/models/provider/provider.go
git commit -m "feat: register ChatBot provider"
```

---

## Task 3: 实现 MD5 签名工具

**Files:**
- Create: `internal/models/utils/signer.go`
- Create: `internal/models/utils/signer_test.go`

**Step 1: 检查 `internal/models/utils/` 目录是否存在**

```bash
ls internal/models/utils/
```

如不存在则创建：

```bash
mkdir -p internal/models/utils
```

**Step 2: 写失败测试 `internal/models/utils/signer_test.go`**

```go
package utils_test

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/models/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSign_ReturnsRequiredHeaders(t *testing.T) {
	headers := utils.Sign("test-appid", "test-secret", "550e8400-e29b-41d4-a716-446655440000", `{"model":"test"}`)
	require.NotNil(t, headers)
	assert.Equal(t, "test-appid", headers["X-APPID"])
	assert.NotEmpty(t, headers["X-Request-ID"])
	assert.NotEmpty(t, headers["X-Timestamp"])
	assert.NotEmpty(t, headers["X-Nonce"])
	assert.NotEmpty(t, headers["X-Signature"])
}

func TestSign_SignatureIs32HexChars(t *testing.T) {
	headers := utils.Sign("appid", "secret", "req-id-001", "{}")
	sig := headers["X-Signature"]
	assert.Len(t, sig, 32, "MD5 hex string should be 32 chars")
	assert.Regexp(t, `^[0-9a-f]{32}$`, sig)
}

func TestSign_DifferentNonceEachCall(t *testing.T) {
	h1 := utils.Sign("appid", "secret", "req-id-001", "{}")
	h2 := utils.Sign("appid", "secret", "req-id-002", "{}")
	// 不同 requestID 导致签名不同
	assert.NotEqual(t, h1["X-Signature"], h2["X-Signature"])
}

func TestSign_EmptyBodyUsesEmptyJSON(t *testing.T) {
	// 空请求体按文档应传 "{}"，测试与直接传 "{}" 结果一致
	h1 := utils.Sign("appid", "secret", "req-id-001", "{}")
	h2 := utils.Sign("appid", "secret", "req-id-001", "{}")
	// 相同参数（固定 requestID）除 Nonce/Timestamp 外签名应有相同结构
	assert.Equal(t, h1["X-APPID"], h2["X-APPID"])
}
```

**Step 3: 运行测试，确认失败**

```bash
go test ./internal/models/utils/... -v
```

Expected: `FAIL` — `utils.Sign` undefined

**Step 4: 实现 `internal/models/utils/signer.go`**

```go
package utils

import (
	"crypto/md5"
	"fmt"
	"math/rand"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	nonceChars  = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	nonceLength = 16
)

// Sign 按 ChatBot 文档签名算法生成请求头
// appID, appSecret: 凭证（appSecret 为已解密明文）
// requestID: 每次请求唯一的 UUID 字符串
// bodyJSON: 请求体 JSON 字符串，空请求体传 "{}"
func Sign(appID, appSecret, requestID, bodyJSON string) map[string]string {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := generateNonce(nonceLength)

	// 步骤1: 计算 body MD5
	bodyMD5 := md5Hex(bodyJSON)

	// 步骤2-4: 组装参数 map（key 全小写），按字典序排序后 RFC3986 编码拼接
	params := map[string]string{
		"x-appid":      appID,
		"x-request-id": requestID,
		"x-timestamp":  timestamp,
		"x-nonce":      nonce,
		"body":         bodyMD5,
	}

	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, rfc3986Encode(k)+"="+rfc3986Encode(params[k]))
	}
	queryStr := strings.Join(parts, "&")

	// 步骤5-6: 追加 appsecret，计算最终 MD5
	signStr := queryStr + "&appsecret=" + appSecret
	signature := md5Hex(signStr)

	return map[string]string{
		"X-APPID":      appID,
		"X-Request-ID": requestID,
		"X-Timestamp":  timestamp,
		"X-Nonce":      nonce,
		"X-Signature":  signature,
	}
}

func md5Hex(s string) string {
	h := md5.New()
	h.Write([]byte(s))
	return fmt.Sprintf("%x", h.Sum(nil))
}

func generateNonce(length int) string {
	b := make([]byte, length)
	for i := range b {
		b[i] = nonceChars[rand.Intn(len(nonceChars))]
	}
	return string(b)
}

// rfc3986Encode 对字符串做 RFC3986 编码
// 保留字符：A-Z a-z 0-9 - _ . ~
func rfc3986Encode(s string) string {
	var sb strings.Builder
	for _, b := range []byte(s) {
		c := rune(b)
		if (c >= 'A' && c <= 'Z') ||
			(c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '~' {
			sb.WriteRune(c)
		} else {
			sb.WriteString(url.QueryEscape(string(b)))
		}
	}
	return sb.String()
}
```

**Step 5: 运行测试，确认通过**

```bash
go test ./internal/models/utils/... -v
```

Expected: 所有测试 PASS

**Step 6: Commit**

```bash
git add internal/models/utils/signer.go internal/models/utils/signer_test.go
git commit -m "feat: add ChatBot MD5 request signer utility"
```

---

## Task 4: 实现 ChatBot Chat

**Files:**
- Create: `internal/models/chat/chatbot.go`
- Create: `internal/models/chat/chatbot_test.go`

**Step 1: 写失败测试 `internal/models/chat/chatbot_test.go`**

```go
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
```

**Step 2: 运行测试，确认失败**

```bash
go test ./internal/models/chat/... -run TestNewChatBotChat -v
```

Expected: FAIL — `chat.NewChatBotChat` undefined

**Step 3: 实现 `internal/models/chat/chatbot.go`**

```go
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

// ... (其余实现见 internal/models/chat/chatbot.go)
```

**Step 4: 在 `ChatConfig` 中添加 AppID/AppSecret 字段**

修改 `internal/models/chat/chat.go` 的 `ChatConfig` 结构体，追加：

```go
AppID     string
AppSecret string // 加密值，由工厂函数调用方传入，在 NewChatBotChat 中使用前已解密
```

**Step 5: 运行测试，确认通过**

```bash
go test ./internal/models/chat/... -run TestNewChatBotChat -v
```

Expected: PASS

**Step 6: Commit**

```bash
git add internal/models/chat/chatbot.go internal/models/chat/chatbot_test.go internal/models/chat/chat.go
git commit -m "feat: implement ChatBotChat adapting chat.Chat interface"
```

---

## Task 5: 实现 ChatBot Embedder

**Files:**
- Create: `internal/models/embedding/chatbot.go`
- Create: `internal/models/embedding/chatbot_test.go`

（实现方式与 Chat 类似，见 `internal/models/embedding/chatbot.go`）

**Step 1: 在 `embedding.Config` 中添加 AppID/AppSecret 字段**

修改 `internal/models/embedding/embedder.go` 的 `Config` 结构体，追加：

```go
AppID     string
AppSecret string
```

**Step 2: 实现 `internal/models/embedding/chatbot.go`，调用 `/api/v1/embeddings`**

**Step 3: Commit**

```bash
git add internal/models/embedding/chatbot.go internal/models/embedding/chatbot_test.go internal/models/embedding/embedder.go
git commit -m "feat: implement ChatBotEmbedder adapting embedding.Embedder interface"
```

---

## Task 6: 实现 ChatBot Reranker

**Files:**
- Create: `internal/models/rerank/chatbot.go`
- Create: `internal/models/rerank/chatbot_test.go`

（实现方式与 Chat 类似，见 `internal/models/rerank/chatbot.go`）

**Step 1: 在 `RerankerConfig` 中添加 AppID/AppSecret 字段**

修改 `internal/models/rerank/reranker.go` 的 `RerankerConfig` 结构体，追加：

```go
AppID     string
AppSecret string
```

**Step 2: 实现 `internal/models/rerank/chatbot.go`，调用 `/api/v1/rerank`**

**Step 3: Commit**

```bash
git add internal/models/rerank/chatbot.go internal/models/rerank/chatbot_test.go internal/models/rerank/reranker.go
git commit -m "feat: implement ChatBotReranker adapting rerank.Reranker interface"
```

---

## Task 7: 接入工厂函数路由（Chat / Embedding / Rerank）

**Files:**
- Modify: `internal/models/chat/chat.go`
- Modify: `internal/models/embedding/embedder.go`
- Modify: `internal/models/rerank/reranker.go`
- Modify: `internal/application/service/model.go`

**Step 1: 在三个工厂函数 switch 中分别添加 `case provider.ProviderChatBot`**

**Step 2: 在 service 层解密 AppSecret 后透传**

```go
func decryptAppSecret(cryptoSvc *crypto.CryptoService, encrypted string) string {
    if encrypted == "" || cryptoSvc == nil {
        return encrypted
    }
    plain, err := cryptoSvc.DecryptString(encrypted)
    if err != nil {
        return ""
    }
    return plain
}
```

**Step 3: 全量编译验证**

```bash
go build ./...
```

**Step 4: Commit**

```bash
git add internal/models/chat/chat.go internal/models/embedding/embedder.go \
        internal/models/rerank/reranker.go internal/application/service/model.go
git commit -m "feat: route chatbot provider in chat/embedding/rerank factory functions"
```

---

## Task 8: 实现 ChatBotService（Upsert 初始化逻辑 + 健康检查 + 状态检测）

**Files:**
- Create: `internal/application/service/chatbot.go`

### 8.1 连通性验证：使用 `/health` 接口

**重要变更：** 不再发送 chat 请求来测试连通性。ChatBot 服务提供 `GET /health` 接口：

```
GET <ChatBotBaseURL>/health
Response: {"status":"ok","timestamp":<unix>}
```

`pingChatBot` 实现：

```go
// pingChatBot 调用 ChatBot 服务的 GET /health 接口验证可达性
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
```

> **注意：** appID 和 appSecret 参数保留在函数签名中以保持接口一致性，健康检查本身不需要凭证。

### 8.2 ChatBotService 接口

```go
type ChatBotService interface {
    Initialize(ctx context.Context, appID, appSecret string) (*InitializeResult, error)
    // CheckStatus 检查当前租户的 ChatBot 凭证是否可正常解密
    CheckStatus(ctx context.Context) (*ChatBotStatusResult, error)
}

type ChatBotStatusResult struct {
    HasModels   bool   `json:"has_models"`
    NeedsReinit bool   `json:"needs_reinit"`
    Reason      string `json:"reason,omitempty"`
}
```

### 8.3 CheckStatus 实现

查找当前租户的任意一个 chatbot 模型，取其加密的 AppSecret 尝试用 CryptoService 解密：
- 若解密失败 → `NeedsReinit=true`，提示用户重新填写凭证
- 若无 chatbot 模型 → `HasModels=false`
- 若解密成功 → `NeedsReinit=false`

**Step: Commit**

```bash
git add internal/application/service/chatbot.go
git commit -m "feat: implement ChatBotService with /health connectivity check and status detection"
```

---

## Task 9: 实现 ChatBotHandler 和注册路由

**Files:**
- Create: `internal/handler/chatbot.go`
- Modify: `internal/router/router.go`
- Modify: `internal/container/container.go`

### 9.1 Handler 实现

```go
// Initialize POST /api/v1/models/chatbot/initialize
func (h *ChatBotHandler) Initialize(c *gin.Context) { ... }

// Status GET /api/v1/models/chatbot/status
func (h *ChatBotHandler) Status(c *gin.Context) {
    result, err := h.svc.CheckStatus(c.Request.Context())
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }
    c.JSON(http.StatusOK, result)
}
```

### 9.2 路由注册

```go
func RegisterChatBotRoutes(r *gin.RouterGroup, handler *handler.ChatBotHandler) {
    r.POST("/models/chatbot/initialize", handler.Initialize)
    r.GET("/models/chatbot/status", handler.Status)
}
```

**Step: Commit**

```bash
git add internal/handler/chatbot.go internal/router/router.go internal/container/container.go
git commit -m "feat: add ChatBotHandler with status endpoint and register routes"
```

---

## Task 10: Crypto 持久化（关键！）

**Problem:** `CryptoService` 在 `CRYPTO_SALT` 未设置时随机生成 salt，服务重启后 salt 变更导致已加密的 AppSecret 无法解密。

**Solution:** 将首次生成的 masterKey + salt 持久化到 `data-files` volume（`/data/files/.crypto_state.json`），重启时优先从文件恢复。

**Files:**
- Modify: `internal/container/container.go`
- Modify: `docker-compose.yml`

### 10.1 initCryptoService 修改逻辑

```
if CRYPTO_MASTER_KEY && CRYPTO_SALT 均已设置:
    直接使用环境变量（最高优先级）
else:
    尝试从 /data/files/.crypto_state.json 读取
    if 读取成功:
        用文件中的 masterKey/salt 初始化（重启恢复）
    else:
        使用/生成配置（MASTER_KEY 默认 "weknora-default-key"，SALT 随机生成）
        将实际使用的 masterKey + base64(salt) 写入持久化文件
        如写入失败：仅记录 WARN 日志，不阻止服务启动
```

持久化文件格式（`/data/files/.crypto_state.json`，权限 0o600）：

```json
{
  "master_key": "weknora-default-key",
  "salt": "<base64-encoded-salt>"
}
```

### 10.2 docker-compose.yml 修改

在 `app` 服务环境变量中添加透传（用户可通过 `.env` 设置固定值）：

```yaml
# Crypto: 主密钥和盐值，用于 AppSecret 等敏感字段的 AES-256 加密
# 若不设置则自动生成并持久化到 data-files volume（/data/files/.crypto_state.json）
# 重启时自动从文件恢复，保证已加密数据可继续解密
- CRYPTO_MASTER_KEY=${CRYPTO_MASTER_KEY:-}
- CRYPTO_SALT=${CRYPTO_SALT:-}
```

> **注意：** `data-files:/data/files` volume 已在 docker-compose.yml 中挂载，无需新增 volume。

**Step: Commit**

```bash
git add internal/container/container.go docker-compose.yml
git commit -m "feat: persist crypto state to data-files volume for cross-restart recovery"
```

---

## Task 11: 前端添加 ChatBot 配置区块 + 凭证失效提示

**Files:**
- Modify: `frontend/src/views/settings/ModelSettings.vue`
- Modify: `frontend/src/api/model/index.ts`

### 11.1 新增 API 接口

```typescript
export interface ChatBotStatusResult {
  has_models: boolean
  needs_reinit: boolean
  reason?: string
}

export function getChatBotStatus(): Promise<ChatBotStatusResult>
export function initializeChatBot(data: InitializeChatBotRequest): Promise<InitializeChatBotResult>
```

### 11.2 ModelSettings.vue 逻辑

- `onMounted` → `loadModels()`
- `loadModels()` 同时调用 `getChatBotStatus()`，将结果存入 `chatBotNeedsReinit` / `chatBotReinitReason`
- ChatBot 配置区块：若 `chatBotNeedsReinit === true`，在表单上方展示橙色警告框：

```
⚠️ ChatBot 凭证已失效
服务重启后加密密钥已变更，已保存的凭证无法解密。
请重新填写 APPID 和 APPSECRET 并点击"保存并初始化"以恢复服务。
```

- 初始化成功后重置 `chatBotNeedsReinit = false`

**Step: Commit**

```bash
git add frontend/src/api/model/index.ts frontend/src/views/settings/ModelSettings.vue
git commit -m "feat: add ChatBot config with reinit warning in ModelSettings frontend"
```

---

## Task 12: 全量集成验证

**Step 1: 后端全量编译**

```bash
go build ./...
```

**Step 2: 运行所有新增测试**

```bash
go test ./internal/models/utils/... \
        ./internal/models/chat/... \
        ./internal/models/embedding/... \
        ./internal/models/rerank/... \
        ./internal/application/service/... \
        -v -count=1
```

Expected: 所有测试 PASS

**Step 3: 确认 Provider 已注册**

```bash
go test ./internal/models/provider/... -v -run TestList
```

Expected: 输出的 provider 列表中包含 `chatbot`

**Step 4: Final commit（如有未提交文件）**

```bash
git status
git add -A
git commit -m "feat: complete ChatBot provider integration"
```

---

## 关键设计决策记录

### Crypto 持久化策略

| 场景 | 行为 |
|------|------|
| 首次部署，未设置环境变量 | 自动生成 salt，写入 `/data/files/.crypto_state.json` |
| 重启后（文件存在） | 从文件恢复，AppSecret 可正常解密 |
| 文件丢失/损坏后重启 | 生成新 salt，旧加密数据失效，前端展示警告提示重新填写 |
| 用户设置了 `CRYPTO_MASTER_KEY` + `CRYPTO_SALT` 环境变量 | 直接使用环境变量（最高优先级），忽略文件 |
| `data-files` volume 不可写 | 记录 WARN 日志，服务正常启动，但重启后加密数据失效 |

### 连通性检测策略

| 方案 | 选择 |
|------|------|
| 发送 chat 请求 | ❌ 消耗配额，响应慢，失败原因多样 |
| 调用 GET /health | ✅ 轻量、快速、语义明确 |

ChatBot `/health` 接口规范：
```
GET <ChatBotBaseURL>/health
Response 200: {"status":"ok","timestamp":<unix>}
```
