# 加密模块 (Crypto Module)

WeKnora项目的密钥加密解密基础设施模块，提供安全的AES加密解密功能。

## 功能特性

- **AES-256加密**: 使用CTR模式提供高效的流加密
- **PBKDF2密钥派生**: 安全地从主密钥派生加密密钥
- **随机盐值支持**: 每次加密使用不同的盐值增强安全性
- **Base64编码**: 加密结果以Base64格式输出，便于存储和传输
- **完整错误处理**: 提供详细的错误信息和验证机制
- **配置管理**: 支持环境变量和配置文件方式配置

## 快速开始

### 基本用法

```go
package main

import (
	"fmt"
	"log"

	"github.com/begoniezhao/WeKnora/internal/infrastructure/crypto"
)

func main() {
	// 创建加密服务
	masterKey := "your-secure-master-key"
	cryptoService, err := crypto.NewCryptoService(masterKey, nil)
	if err != nil {
		log.Fatal(err)
	}

	// 加密数据
	plaintext := "敏感数据需要加密"
	encrypted, err := cryptoService.EncryptString(plaintext)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("加密结果: %s\n", encrypted)

	// 解密数据
	decrypted, err := cryptoService.DecryptString(encrypted)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("解密结果: %s\n", decrypted)
}
```

### 使用配置

```go
package main

import (
	"fmt"
	"log"

	"github.com/begoniezhao/WeKnora/internal/infrastructure/crypto"
)

func main() {
	// 从环境变量加载配置
	config := crypto.LoadConfigFromEnv()
	
	// 创建加密服务
	cryptoService, err := crypto.NewCryptoServiceFromConfig(config)
	if err != nil {
		log.Fatal(err)
	}

	// 使用服务...
	_ = cryptoService
}
```

## 环境变量配置

可以通过以下环境变量配置加密服务：

| 环境变量 | 描述 | 默认值 |
|---------|------|--------|
| `CRYPTO_MASTER_KEY` | 主密钥（必需） | 无 |
| `CRYPTO_SALT` | 盐值（Base64编码） | 自动生成 |
| `CRYPTO_SALT_LENGTH` | 盐值长度 | 16 |
| `CRYPTO_ITERATIONS` | PBKDF2迭代次数 | 10000 |
| `CRYPTO_KEY_LENGTH` | 派生密钥长度 | 32 |

## API参考

### CryptoService

#### NewCryptoService(masterKey string, salt []byte) (*CryptoService, error)
创建新的加密服务实例。

- `masterKey`: 主密钥字符串
- `salt`: 盐值字节数组，如果为nil则自动生成

#### Encrypt(plaintext []byte) (string, error)
加密字节数组数据。

#### EncryptString(plaintext string) (string, error)
加密字符串数据。

#### Decrypt(encryptedData string) ([]byte, error)
解密数据到字节数组。

#### DecryptString(encryptedData string) (string, error)
解密数据到字符串。

#### GetSalt() []byte
获取当前使用的盐值。

### 工具函数

#### GenerateRandomKey(length int) (string, error)
生成随机密钥。

#### GenerateRandomSalt(length int) ([]byte, error)
生成随机盐值。

### Config

#### LoadConfigFromEnv() *Config
从环境变量加载配置。

#### NewCryptoServiceFromConfig(config *Config) (*CryptoService, error)
从配置创建加密服务。

## 使用场景

### 1. API密钥加密

```go
func encryptAPIKeys() {
	cryptoService, _ := crypto.NewCryptoService("app-secret-key", nil)
	
	apiKeys := map[string]string{
		"OpenAI": "sk-proj-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
		"AWS":    "AKIAIOSFODNN7EXAMPLE",
	}
	
	for service, key := range apiKeys {
		encrypted, _ := cryptoService.EncryptString(key)
		// 存储 encrypted 到数据库或配置文件
	}
}
```

### 2. 配置文件加密

```go
func encryptConfig() {
	cryptoService, _ := crypto.NewCryptoService("config-key", nil)
	
	sensitiveConfig := map[string]string{
		"database_password": "my-secret-db-password",
		"jwt_secret":        "jwt-super-secret-key",
	}
	
	for key, value := range sensitiveConfig {
		encrypted, _ := cryptoService.EncryptString(value)
		// 在配置文件中使用加密值
	}
}
```

### 3. 数据库字段加密

```go
type User struct {
	ID       int
	Name     string
	Email    string
	Password string // 存储加密后的密码
}

func encryptUserPassword(password string) (string, error) {
	cryptoService, err := crypto.NewCryptoService("user-key", nil)
	if err != nil {
		return "", err
	}
	
	return cryptoService.EncryptString(password)
}
```

## 安全最佳实践

### 1. 主密钥管理

- **不要硬编码密钥**: 使用环境变量或密钥管理服务
- **定期轮换密钥**: 定期更新主密钥增强安全性
- **最小权限原则**: 仅授予必要的访问权限

### 2. 盐值使用

- **使用随机盐值**: 默认自动生成随机盐值
- **固定盐值场景**: 仅在需要可重现加密结果时使用固定盐值
- **盐值存储**: 盐值可以公开存储，但应与加密数据分开

### 3. 错误处理

```go
cryptoService, err := crypto.NewCryptoService(masterKey, nil)
if err != nil {
	// 处理配置错误
	log.Fatal("Failed to create crypto service:", err)
}

encrypted, err := cryptoService.EncryptString("sensitive-data")
if err != nil {
	// 处理加密错误
	return fmt.Errorf("encryption failed: %w", err)
}
```

### 4. 性能考虑

- **密钥派生开销**: PBKDF2有计算开销，适合低频操作
- **批量加密**: 对大量数据考虑分批处理
- **缓存服务**: 在应用生命周期内重用CryptoService实例

## 测试

运行测试：

```bash
cd internal/infrastructure/crypto
go test -v
```

运行基准测试：

```bash
go test -bench=.
```

## 故障排除

### 常见错误

1. **空主密钥错误**
   ```
   master key cannot be empty
   ```
   解决方案：提供有效的主密钥

2. **无效的Base64数据**
   ```
   failed to decode base64
   ```
   解决方案：确保加密数据未被篡改

3. **数据过短错误**
   ```
   encrypted data too short
   ```
   解决方案：验证加密数据的完整性

### 调试技巧

启用详细日志记录：

```go
import "log"

func debugEncryption() {
	cryptoService, err := crypto.NewCryptoService("debug-key", nil)
	if err != nil {
		log.Printf("Debug: Failed to create service: %v", err)
		return
	}
	
	// ... 其他操作
}
```

## 贡献指南

1. 遵循Go代码规范
2. 添加适当的单元测试
3. 更新文档和示例
4. 运行测试确保功能正常

## 许可证

本项目使用MIT许可证。详见LICENSE文件。