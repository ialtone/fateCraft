// Package secretbox encrypts application credentials before persistence.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
)

var ErrKeyTooShort = errors.New("AUTH_DATA_ENCRYPTION_KEY 至少需要 32 个字符")

type Box struct{ aead cipher.AEAD }

func New(rawKey string) (*Box, error) {
	if len(rawKey) < 32 {
		return nil, ErrKeyTooShort
	}
	key := sha256.Sum256([]byte(rawKey))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

func (b *Box) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := b.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (b *Box) Decrypt(encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(data) < b.aead.NonceSize() {
		return "", errors.New("凭据密文无效")
	}
	plaintext, err := b.aead.Open(nil, data[:b.aead.NonceSize()], data[b.aead.NonceSize():], nil)
	if err != nil {
		return "", errors.New("无法解密凭据，请检查 AUTH_DATA_ENCRYPTION_KEY")
	}
	return string(plaintext), nil
}
