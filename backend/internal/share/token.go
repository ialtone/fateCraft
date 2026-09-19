package share

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"

	"github.com/google/uuid"
)

var ErrSigningUnavailable = errors.New("分享链接签名密钥未配置或长度不足")

type signer struct{ key []byte }

func newSigner(raw string) (*signer, error) {
	if len(raw) < 32 {
		return nil, ErrSigningUnavailable
	}
	return &signer{key: []byte(raw)}, nil
}

func (s *signer) sign(id uuid.UUID) string {
	payload := base64.RawURLEncoding.EncodeToString(id[:])
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write([]byte("v1." + payload))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return "v1." + payload + "." + signature
}

func (s *signer) parse(token string) (uuid.UUID, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != "v1" {
		return uuid.Nil, errors.New("invalid share token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(payload) != 16 {
		return uuid.Nil, errors.New("invalid share token")
	}
	provided, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return uuid.Nil, errors.New("invalid share token")
	}
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write([]byte("v1." + parts[1]))
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return uuid.Nil, errors.New("invalid share token")
	}
	id, err := uuid.FromBytes(payload)
	if err != nil {
		return uuid.Nil, errors.New("invalid share token")
	}
	return id, nil
}

func (s *signer) fingerprint(value string) string {
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write([]byte("reporter:" + value))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
