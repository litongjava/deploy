// Package secret 提供平台里所有“需要加密存放的东西”的加解密：
// 项目 Secret 环境变量、Git 凭据、SSH passphrase。
//
// 用 AES-256-GCM。密钥来自配置（DEPLOY_SECRET_KEY 或 dataDir/secret.key）。
// 密文格式：base64(nonce[12] || ciphertext || tag)，便于直接塞进 TEXT 列。
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// Box 持有派生好的 AEAD。
type Box struct {
	aead cipher.AEAD
}

// New 用主密钥构造 Box。主密钥是任意长度的字符串，
// 这里用 SHA-256 派生出固定 32 字节，避免要求用户提供恰好 32 字节的密钥。
func New(masterKey string) (*Box, error) {
	if masterKey == "" {
		return nil, errors.New("master key is empty")
	}
	sum := sha256.Sum256([]byte(masterKey))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Encrypt 加密明文，返回 base64 密文。明文为空时返回空串（表示“未设置”）。
func (b *Box) Encrypt(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	out := b.aead.Seal(nonce, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(out), nil
}

// Decrypt 是 Encrypt 的逆操作。密文为空时返回空串。
func (b *Box) Decrypt(encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("ciphertext is not valid base64: %w", err)
	}
	ns := b.aead.NonceSize()
	if len(raw) < ns {
		return "", errors.New("ciphertext is too short")
	}
	nonce, ct := raw[:ns], raw[ns:]
	plain, err := b.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		// 最常见的原因是换了 secret.key：明确提示，别让人以为是程序坏了。
		return "", fmt.Errorf("decryption failed (the key may have changed): %w", err)
	}
	return string(plain), nil
}
