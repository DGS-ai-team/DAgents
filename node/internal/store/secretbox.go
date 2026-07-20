package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const secretKeyFileName = ".llm_secret_key"

// SecretBox 使用 AES-256-GCM 加密敏感字段；密钥存放在 runtime 目录。
type SecretBox struct {
	key []byte
}

// OpenSecretBox 打开或创建 `<keyDir>/.llm_secret_key`（32 字节）。
func OpenSecretBox(keyDir string) (*SecretBox, error) {
	dir := strings.TrimSpace(keyDir)
	if dir == "" {
		return nil, fmt.Errorf("secret key dir is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create secret key dir: %w", err)
	}
	path := filepath.Join(dir, secretKeyFileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read secret key: %w", err)
		}
		key := make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, key); err != nil {
			return nil, fmt.Errorf("generate secret key: %w", err)
		}
		if err := os.WriteFile(path, key, 0o600); err != nil {
			return nil, fmt.Errorf("write secret key: %w", err)
		}
		return &SecretBox{key: key}, nil
	}
	if len(raw) != 32 {
		return nil, fmt.Errorf("secret key file must be 32 bytes")
	}
	key := make([]byte, 32)
	copy(key, raw)
	return &SecretBox{key: key}, nil
}

// Encrypt 加密明文，返回 base64(nonce|ciphertext)。
func (b *SecretBox) Encrypt(plaintext string) (string, error) {
	if b == nil || len(b.key) != 32 {
		return "", fmt.Errorf("secret box unavailable")
	}
	block, err := aes.NewCipher(b.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt 解密 Encrypt 产物。
func (b *SecretBox) Decrypt(encoded string) (string, error) {
	if b == nil || len(b.key) != 32 {
		return "", fmt.Errorf("secret box unavailable")
	}
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	block, err := aes.NewCipher(b.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(plain), nil
}
