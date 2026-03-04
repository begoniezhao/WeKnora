package crypto

import (
	"testing"
)

func TestCryptoService_EncryptDecrypt(t *testing.T) {
	masterKey := "my-secret-master-key-123456"
	salt := []byte("test-salt-123456")

	cryptoService, err := NewCryptoService(masterKey, salt)
	if err != nil {
		t.Fatalf("Failed to create crypto service: %v", err)
	}

	testCases := []struct {
		name      string
		plaintext string
	}{
		{"empty string", ""},
		{"simple text", "hello world"},
		{"special characters", "hello@world.com!123#$"},
		{"unicode text", "中文测试 🚀 🌟"},
		{"long text", "this is a very long text that needs to be encrypted properly with multiple lines and special characters"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// 跳过空字符串测试，因为我们的实现不允许空字符串
			if tc.plaintext == "" {
				t.Skip("empty string test skipped")
			}

			// 加密
			encrypted, err := cryptoService.EncryptString(tc.plaintext)
			if err != nil {
				t.Fatalf("Encryption failed: %v", err)
			}

			// 解密
			decrypted, err := cryptoService.DecryptString(encrypted)
			if err != nil {
				t.Fatalf("Decryption failed: %v", err)
			}

			// 验证结果
			if decrypted != tc.plaintext {
				t.Errorf("Decrypted text does not match original. Expected: %s, Got: %s", tc.plaintext, decrypted)
			}
		})
	}
}

func TestCryptoService_EmptyMasterKey(t *testing.T) {
	_, err := NewCryptoService("", nil)
	if err == nil {
		t.Error("Expected error for empty master key, but got none")
	}
}

func TestCryptoService_InvalidEncryption(t *testing.T) {
	cryptoService, err := NewCryptoService("valid-key", nil)
	if err != nil {
		t.Fatalf("Failed to create crypto service: %v", err)
	}

	// 测试空字符串加密
	_, err = cryptoService.EncryptString("")
	if err == nil {
		t.Error("Expected error for empty plaintext, but got none")
	}

	// 测试无效的加密数据解密
	_, err = cryptoService.DecryptString("invalid-base64")
	if err == nil {
		t.Error("Expected error for invalid base64, but got none")
	}

	// 测试过短的加密数据
	_, err = cryptoService.DecryptString("aGVsbG8=")
	if err == nil {
		t.Error("Expected error for short encrypted data, but got none")
	}
}

func TestCryptoService_DifferentSalts(t *testing.T) {
	masterKey := "same-master-key"

	// 使用不同的盐值创建两个加密服务
	salt1 := []byte("salt-one-123456")
	salt2 := []byte("salt-two-789012")

	cryptoService1, err := NewCryptoService(masterKey, salt1)
	if err != nil {
		t.Fatalf("Failed to create crypto service 1: %v", err)
	}

	cryptoService2, err := NewCryptoService(masterKey, salt2)
	if err != nil {
		t.Fatalf("Failed to create crypto service 2: %v", err)
	}

	plaintext := "test data"

	// 使用第一个服务加密
	encrypted1, err := cryptoService1.EncryptString(plaintext)
	if err != nil {
		t.Fatalf("Encryption with service 1 failed: %v", err)
	}

	// 使用第二个服务加密（应该得到不同的结果）
	encrypted2, err := cryptoService2.EncryptString(plaintext)
	if err != nil {
		t.Fatalf("Encryption with service 2 failed: %v", err)
	}

	// 验证不同盐值产生不同的加密结果
	if encrypted1 == encrypted2 {
		t.Error("Different salts should produce different encrypted results")
	}

	// 验证每个服务只能解密自己的数据
	decrypted1, err := cryptoService1.DecryptString(encrypted1)
	if err != nil {
		t.Fatalf("Decryption with service 1 failed: %v", err)
	}
	if decrypted1 != plaintext {
		t.Errorf("Service 1 decryption failed. Expected: %s, Got: %s", plaintext, decrypted1)
	}

	// 服务1应该不能解密服务2的数据
	_, err = cryptoService1.DecryptString(encrypted2)
	if err == nil {
		t.Error("Service 1 should not be able to decrypt data from service 2")
	}
}

func TestGenerateRandomKey(t *testing.T) {
	key, err := GenerateRandomKey(32)
	if err != nil {
		t.Fatalf("Failed to generate random key: %v", err)
	}

	if key == "" {
		t.Error("Generated key should not be empty")
	}

	// 测试过短的长度
	_, err = GenerateRandomKey(8)
	if err == nil {
		t.Error("Expected error for key length less than 16")
	}
}

func TestGenerateRandomSalt(t *testing.T) {
	salt, err := GenerateRandomSalt(16)
	if err != nil {
		t.Fatalf("Failed to generate random salt: %v", err)
	}

	if len(salt) != 16 {
		t.Errorf("Salt length should be 16, got %d", len(salt))
	}

	// 测试过短的长度
	_, err = GenerateRandomSalt(4)
	if err == nil {
		t.Error("Expected error for salt length less than 8")
	}
}

func BenchmarkCryptoService_Encrypt(b *testing.B) {
	cryptoService, err := NewCryptoService("benchmark-master-key", nil)
	if err != nil {
		b.Fatalf("Failed to create crypto service: %v", err)
	}

	plaintext := "this is a test string for benchmarking encryption performance"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := cryptoService.EncryptString(plaintext)
		if err != nil {
			b.Fatalf("Encryption failed: %v", err)
		}
	}
}

func BenchmarkCryptoService_Decrypt(b *testing.B) {
	cryptoService, err := NewCryptoService("benchmark-master-key", nil)
	if err != nil {
		b.Fatalf("Failed to create crypto service: %v", err)
	}

	plaintext := "this is a test string for benchmarking decryption performance"
	encrypted, err := cryptoService.EncryptString(plaintext)
	if err != nil {
		b.Fatalf("Encryption failed: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := cryptoService.DecryptString(encrypted)
		if err != nil {
			b.Fatalf("Decryption failed: %v", err)
		}
	}
}
