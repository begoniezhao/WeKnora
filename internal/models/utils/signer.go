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
