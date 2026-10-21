// Package llmproxy persists owner-managed LLM outbound proxy settings and
// publishes validated snapshots to the shared outbound manager.
package llmproxy

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"github.com/ialtone/fatecraft/internal/llmoutbound"
	"github.com/ialtone/fatecraft/internal/secretbox"
	"github.com/ialtone/fatecraft/internal/storage"
)

var (
	ErrEncryptionUnavailable = errors.New("服务端尚未配置 AUTH_DATA_ENCRYPTION_KEY，无法保存或读取 LLM 出站代理密码")
	ErrDecryption            = errors.New("无法解密已保存的 LLM 出站代理密码，请检查 AUTH_DATA_ENCRYPTION_KEY")
	ErrPasswordConflict      = errors.New("不能同时清除和替换代理密码")
	ErrMissingTestURL        = errors.New("请填写 HTTPS 检测地址；未配置平台公共 Base URL 时无法自动提取")
)

type systemRepository interface {
	Get(context.Context) (*storage.SystemSettings, error)
	SaveLLMProxy(context.Context, *storage.SystemSettings, int64) (*storage.SystemSettings, error)
}

// Service serializes proxy saves with snapshot publication. It never logs or
// returns plaintext or encrypted passwords.
type Service struct {
	system   systemRepository
	outbound *llmoutbound.Manager
	box      *secretbox.Box
	boxErr   error
	mu       sync.Mutex
}

func New(system systemRepository, outbound *llmoutbound.Manager, encryptionKey string) *Service {
	box, err := secretbox.New(encryptionKey)
	return &Service{system: system, outbound: outbound, box: box, boxErr: err}
}

// View is the only proxy representation exposed by the admin API.
type View struct {
	Enabled     bool   `json:"enabled"`
	URL         string `json:"url"`
	Username    string `json:"username"`
	HasPassword bool   `json:"has_password"`
	CAPEM       string `json:"ca_pem"`
	Revision    int64  `json:"revision"`
}

func (s *Service) View(ctx context.Context) (*View, error) {
	settings, err := s.system.Get(ctx)
	if err != nil {
		return nil, err
	}
	return viewFrom(settings), nil
}

// UpdateInput carries only the proxy group. Password nil/empty keeps the saved
// value; a non-empty password replaces it; ClearPassword removes it.
type UpdateInput struct {
	ExpectedRevision int64
	Enabled          *bool
	URL              *string
	Username         *string
	Password         *string
	ClearPassword    bool
	CAPEM            *string
}

func (s *Service) Update(ctx context.Context, input UpdateInput) (*View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if input.ClearPassword && input.Password != nil && *input.Password != "" {
		return nil, ErrPasswordConflict
	}
	current, err := s.system.Get(ctx)
	if err != nil {
		return nil, err
	}
	next := *current
	if input.Enabled != nil {
		next.LLMProxyEnabled = *input.Enabled
	}
	if input.URL != nil {
		if strings.ContainsAny(*input.URL, "\r\n") {
			return nil, fmt.Errorf("%w: 地址不能包含换行", llmoutbound.ErrInvalidConfig)
		}
		next.LLMProxyURL = strings.TrimSpace(*input.URL)
	}
	if input.Username != nil {
		next.LLMProxyUsername = *input.Username
	}
	if input.CAPEM != nil {
		next.LLMProxyCAPEM = *input.CAPEM
	}

	password := ""
	switch {
	case input.ClearPassword:
		next.LLMProxyPasswordEncrypted = ""
	case input.Password != nil && *input.Password != "":
		plain := *input.Password
		encrypted, err := s.encrypt(plain)
		if err != nil {
			return nil, err
		}
		next.LLMProxyPasswordEncrypted = encrypted
		password = plain
	default:
		if next.LLMProxyEnabled {
			password, err = s.decrypt(next.LLMProxyPasswordEncrypted)
			if err != nil {
				return nil, err
			}
		}
	}

	candidate, err := llmoutbound.Build(llmoutbound.Config{
		Enabled: next.LLMProxyEnabled, URL: next.LLMProxyURL,
		Username: next.LLMProxyUsername, Password: password, CAPEM: next.LLMProxyCAPEM,
	}, current.LLMProxyRevision+1)
	if err != nil {
		return nil, err
	}
	saved, err := s.system.SaveLLMProxy(ctx, &next, input.ExpectedRevision)
	if err != nil {
		candidate.Close()
		return nil, err
	}
	// Publish only after the transaction committed. The candidate was built
	// before the save so an invalid config never becomes durable.
	s.outbound.Publish(candidate.WithRevision(saved.LLMProxyRevision))
	return viewFrom(saved), nil
}

// Reload publishes the persisted configuration at startup. An enabled proxy
// that cannot be decrypted or built is a fatal configuration error; it must not
// silently fall back to direct.
func (s *Service) Reload(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	settings, err := s.system.Get(ctx)
	if err != nil {
		return err
	}
	if !settings.LLMProxyEnabled {
		// Disabled means explicit direct. The saved CA may still extend trust for
		// direct model targets, but no saved credentials are decrypted.
		snapshot, err := llmoutbound.Build(llmoutbound.Config{CAPEM: settings.LLMProxyCAPEM}, settings.LLMProxyRevision)
		if err != nil {
			return err
		}
		s.outbound.Publish(snapshot)
		return nil
	}
	password, err := s.decrypt(settings.LLMProxyPasswordEncrypted)
	if err != nil {
		return err
	}
	snapshot, err := llmoutbound.Build(llmoutbound.Config{
		Enabled: true, URL: settings.LLMProxyURL, Username: settings.LLMProxyUsername,
		Password: password, CAPEM: settings.LLMProxyCAPEM,
	}, settings.LLMProxyRevision)
	if err != nil {
		return fmt.Errorf("LLM 出站代理配置无法启用: %w", err)
	}
	s.outbound.Publish(snapshot)
	return nil
}

// Test runs a bounded connectivity check against the saved runtime snapshot.
func (s *Service) Test(ctx context.Context, target string) (llmoutbound.TestResult, error) {
	if !s.outbound.Snapshot().Enabled() {
		return llmoutbound.TestResult{}, llmoutbound.ErrProxyDisabled
	}
	if strings.TrimSpace(target) == "" {
		settings, err := s.system.Get(ctx)
		if err != nil {
			return llmoutbound.TestResult{}, err
		}
		target, err = deriveTestURL(settings.PublicLLMBaseURL)
		if err != nil {
			return llmoutbound.TestResult{}, err
		}
	}
	return s.outbound.TestConnection(ctx, target)
}

func deriveTestURL(baseURL string) (string, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return "", ErrMissingTestURL
	}
	u, err := url.Parse(baseURL)
	if err != nil || u == nil || !strings.EqualFold(u.Scheme, "https") || u.Host == "" {
		return "", ErrMissingTestURL
	}
	return "https://" + u.Host, nil
}

func viewFrom(settings *storage.SystemSettings) *View {
	if settings == nil {
		return &View{}
	}
	return &View{
		Enabled: settings.LLMProxyEnabled, URL: settings.LLMProxyURL,
		Username: settings.LLMProxyUsername, HasPassword: settings.LLMProxyPasswordEncrypted != "",
		CAPEM: settings.LLMProxyCAPEM, Revision: settings.LLMProxyRevision,
	}
}

func (s *Service) encrypt(value string) (string, error) {
	if s.boxErr != nil || s.box == nil {
		return "", ErrEncryptionUnavailable
	}
	return s.box.Encrypt(value)
}

func (s *Service) decrypt(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if s.boxErr != nil || s.box == nil {
		return "", ErrEncryptionUnavailable
	}
	plain, err := s.box.Decrypt(value)
	if err != nil {
		return "", ErrDecryption
	}
	return plain, nil
}
