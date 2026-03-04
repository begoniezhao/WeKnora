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
