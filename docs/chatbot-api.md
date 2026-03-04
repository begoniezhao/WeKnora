# ChatBot API Reference

## 概述

所有接口均挂载在 `/api/v1` 前缀下，需要携带签名认证请求头。

**Base URL**
```
http://<host>:8080/api/v1
```

---

## 认证方式（签名验证）

> **获取凭证**：登录 ChatBot 平台后，在「应用管理」或「账号设置」中可获取您的 **APPID** 和 **APPSECRET**，请妥善保管，切勿泄露。

`/chat`、`/embeddings`、`/rerank` 接口均需要在请求头中携带以下签名信息：

| Header | 类型 | 必填 | 说明 |
|--------|------|------|------|
| `X-APPID` | string | ✅ | 平台分配的 APPID |
| `X-Request-ID` | string | ✅ | 请求唯一标识（UUID 格式） |
| `X-Timestamp` | int64 | ✅ | 当前 Unix 时间戳（秒），有效窗口 ±300s |
| `X-Nonce` | string | ✅ | 随机字符串，长度 8~32 位，仅含大小写字母和数字 |
| `X-Signature` | string | ✅ | 请求签名（MD5，见下方签名算法） |

### 签名算法

1. 收集以下参数，所有 key 转为小写：
   - `x-appid` = `X-APPID` 的值
   - `x-request-id` = `X-Request-ID` 的值
   - `x-timestamp` = `X-Timestamp` 的值（字符串形式）
   - `x-nonce` = `X-Nonce` 的值
   - `body` = 请求体 JSON 的 MD5 值（十六进制小写）；若请求体为空则使用 `{}` 的 MD5
2. 对所有 key 按字典序升序排序
3. 对每个 key 和 value 进行 RFC3986 编码（保留 `A-Z a-z 0-9 - _ . ~`，其余字符 `%XX` 编码）
4. 拼接为 `key1=value1&key2=value2&...` 格式的字符串
5. 将拼接结果与 APPSECRET 拼接为 `<拼接结果>&appsecret=<APPSECRET>`
6. 对最终字符串计算 MD5，取十六进制小写字符串作为签名值

---

## 1. Chat Completions

### 接口信息

| 项目 | 内容 |
|------|------|
| **方法** | `POST` |
| **路径** | `/api/v1/chat/completions` |
| **Content-Type** | `application/json` |
| **认证** | 签名验证（见上方） |

### 请求体

```json
{
  "model": "string",           // 必填，模型名称
  "messages": [                // 必填，对话消息列表
    {
      "role": "string",        // 必填，角色：system / user / assistant
      "content": "string",     // 必填，消息内容
      "name": "string"         // 可选，发送者名称
    }
  ],
  "stream": false,             // 可选，是否流式输出，默认 false
  "max_tokens": 0,             // 可选，最大生成 token 数
  "temperature": 0.7,          // 可选，采样温度，范围 0~2
  "top_p": 1.0,                // 可选，核采样概率
  "n": 1,                      // 可选，生成候选数量
  "stop": ["string"],          // 可选，停止词列表
  "presence_penalty": 0.0,     // 可选，存在惩罚
  "frequency_penalty": 0.0,    // 可选，频率惩罚
  "logit_bias": {},            // 可选，logit 偏置
  "user": "string"             // 可选，用户标识
}
```

### 响应体（非流式）

```json
{
  "id": "chatcmpl-xxx",
  "object": "chat.completion",
  "created": 1700000000,
  "model": "gpt-4",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "回复内容"
      },
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 10,
    "completion_tokens": 20,
    "total_tokens": 30
  }
}
```

### 响应体（流式，`stream: true`）

响应格式为 SSE（Server-Sent Events），`Content-Type: text/event-stream`，每行格式如下：

```
data: {"id":"chatcmpl-xxx","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4","choices":[{"index":0,"delta":{"role":"assistant","content":"片段内容"},"finish_reason":null}]}

data: [DONE]
```

### 示例（非流式）

```bash
curl -X POST http://localhost:8080/api/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "X-APPID: your-appid" \
  -H "X-Request-ID: 550e8400-e29b-41d4-a716-446655440000" \
  -H "X-Timestamp: 1700000000" \
  -H "X-Nonce: abc123XYZ" \
  -H "X-Signature: <calculated_signature>" \
  -d '{
    "model": "gpt-4",
    "messages": [
      {"role": "user", "content": "你好，请介绍一下自己"}
    ],
    "temperature": 0.7
  }'
```

### 示例（流式）

```bash
curl -X POST http://localhost:8080/api/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "X-APPID: your-appid" \
  -H "X-Request-ID: 550e8400-e29b-41d4-a716-446655440000" \
  -H "X-Timestamp: 1700000000" \
  -H "X-Nonce: abc123XYZ" \
  -H "X-Signature: <calculated_signature>"  -d '{
    "model": "gpt-4",
    "messages": [
      {"role": "user", "content": "你好，请介绍一下自己"}
    ],
    "stream": true
  }'
```

---

## 2. Embeddings

### 接口信息

| 项目 | 内容 |
|------|------|
| **方法** | `POST` |
| **路径** | `/api/v1/embeddings` |
| **Content-Type** | `application/json` |
| **认证** | 签名验证（见上方） |

### 请求体

```json
{
  "model": "string",                    // 必填，Embedding 模型名称
  "input": ["string", "string"],        // 必填，待向量化的文本列表
  "encoding_format": "float",           // 可选，编码格式，默认 float
  "truncate_prompt_tokens": 0           // 可选，截断 prompt 的最大 token 数
}
```

### 响应体

```json
{
  "data": [
    {
      "index": 0,
      "embedding": [0.0023064255, -0.009327292, ...]
    },
    {
      "index": 1,
      "embedding": [0.0012345678, -0.007654321, ...]
    }
  ]
}
```

### 示例

```bash
curl -X POST http://localhost:8080/api/v1/embeddings \
  -H "Content-Type: application/json" \
  -H "X-APPID: your-appid" \
  -H "X-Request-ID: 550e8400-e29b-41d4-a716-446655440001" \
  -H "X-Timestamp: 1700000000" \
  -H "X-Nonce: def456ABC" \
  -H "X-Signature: <calculated_signature>" \
  -d '{
    "model": "text-embedding-ada-002",
    "input": ["Hello world", "你好世界"]
  }'
```

---

## 3. Rerank

### 接口信息

| 项目 | 内容 |
|------|------|
| **方法** | `POST` |
| **路径** | `/api/v1/rerank` |
| **Content-Type** | `application/json` |
| **认证** | 签名验证（见上方） |

### 请求体

```json
{
  "model": "string",                    // 必填，Rerank 模型名称
  "query": "string",                    // 必填，查询文本
  "documents": ["string", "string"],    // 必填，待重排序的文档列表
  "additional_data": {},                // 可选，传递给模型的额外数据
  "truncate_prompt_tokens": 0           // 可选，截断 prompt 的最大 token 数
}
```

### 响应体

```json
{
  "id": "rerank-xxx",
  "model": "rerank-model-name",
  "usage": {
    "total_tokens": 100
  },
  "results": [
    {
      "index": 2,
      "document": {
        "text": "最相关的文档内容"
      },
      "relevance_score": 0.9876
    },
    {
      "index": 0,
      "document": {
        "text": "次相关的文档内容"
      },
      "relevance_score": 0.6543
    }
  ]
}
```

> `results` 按 `relevance_score` 降序排列，`index` 为文档在原始 `documents` 列表中的位置。

### 示例

```bash
curl -X POST http://localhost:8080/api/v1/rerank \
  -H "Content-Type: application/json" \
  -H "X-APPID: your-appid" \
  -H "X-Request-ID: 550e8400-e29b-41d4-a716-446655440002" \
  -H "X-Timestamp: 1700000000" \
  -H "X-Nonce: ghi789DEF" \
  -H "X-Signature: <calculated_signature>" \
  -d '{
    "model": "rerank-multilingual-v3.0",
    "query": "什么是机器学习？",
    "documents": [
      "机器学习是人工智能的一个分支，通过数据训练模型。",
      "今天天气很好，适合出门散步。",
      "深度学习是机器学习的子集，使用神经网络。"
    ]
  }'
```

---

## 错误响应

所有接口在发生错误时返回统一格式：

### 请求参数错误（400）

```json
{
  "error": {
    "message": "错误描述信息",
    "type": "invalid_request_error",
    "code": "400"
  }
}
```

### 签名验证失败（401）

```json
{
  "error": {
    "code": "INVALID_SIGNATURE",
    "message": "签名验证失败",
    "detail": "具体错误原因"
  }
}
```

| 错误码 | 说明 |
|--------|------|
| `MISSING_HEADERS` | 缺少必要的签名头信息 |
| `INVALID_TIMESTAMP` | 时间戳格式错误或超出有效范围（±300s） |
| `DUPLICATE_NONCE` | Nonce 格式不合法 |
| `INVALID_SIGNATURE` | 签名值不匹配 |
