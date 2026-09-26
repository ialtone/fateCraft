// Package llmsettings resolves the single effective LLM configuration used by
// every model-backed feature.
package llmsettings

import (
	"context"
	"errors"
	"strings"

	"github.com/ialtone/fatecraft/internal/secretbox"
	"github.com/ialtone/fatecraft/internal/storage"
)

const (
	AccessModeUserManaged     = "user_managed"
	AccessModePlatformManaged = "platform_managed"
	SourceUser                = "user"
	SourcePlatform            = "platform"
	// SourceClient 表示原始响应由浏览器经 Relay 回传、未经服务端直连获取。
	SourceClient = "client"
)

var (
	ErrUserSettingsDisabled  = errors.New("平台当前统一提供模型配置，用户设置不可修改")
	ErrEncryptionUnavailable = errors.New("服务端尚未配置 AUTH_DATA_ENCRYPTION_KEY，LLM API Key 无论是用户配置还是平台公共配置都必须先加密后保存")
	ErrMissingAPIKey         = errors.New("API Key 不能为空")
	ErrInvalidAccessMode     = errors.New("llm_access_mode 必须是 user_managed 或 platform_managed")
	ErrPlatformIncomplete    = errors.New("平台模型模式需要完整配置 Base URL、API Key 和叙事模型")
	ErrInvalidLLMRoute       = errors.New("llm_route 必须是 server 或 client")
)

type userRepository interface {
	Get(context.Context, string) (*storage.LLMSettings, error)
	Save(context.Context, string, *storage.LLMSettings) error
}

type systemRepository interface {
	Get(context.Context) (*storage.SystemSettings, error)
	Save(context.Context, *storage.SystemSettings) error
}

type Service struct {
	users  userRepository
	system systemRepository
	box    *secretbox.Box
	boxErr error
}

func New(users userRepository, system systemRepository, encryptionKey string) *Service {
	box, err := secretbox.New(encryptionKey)
	return &Service{users: users, system: system, box: box, boxErr: err}
}

func (s *Service) AccessMode(ctx context.Context) (string, error) {
	settings, err := s.system.Get(ctx)
	if err != nil {
		return "", err
	}
	mode := normalizeAccessMode(settings.LLMAccessMode)
	if !ValidAccessMode(mode) {
		return "", ErrInvalidAccessMode
	}
	return mode, nil
}

// EffectiveRoute 返回该玩家的有效路由：platform_managed 时恒为 server，
// 否则为 llm_settings.llm_route（未配置时默认为 server）。
func (s *Service) EffectiveRoute(ctx context.Context, playerID string) (string, error) {
	mode, err := s.AccessMode(ctx)
	if err != nil {
		return "", err
	}
	if mode == AccessModePlatformManaged {
		return storage.LLMRouteServer, nil
	}
	stored, err := s.users.Get(ctx, playerID)
	if err != nil {
		return "", err
	}
	return normalizeLLMRoute(stored.LLMRoute), nil
}

// Get returns a runtime-ready configuration. APIKey is populated only in
// memory and must never be serialized or logged.
func (s *Service) Get(ctx context.Context, playerID string) (*storage.LLMSettings, error) {
	global, err := s.system.Get(ctx)
	if err != nil {
		return nil, err
	}
	mode := normalizeAccessMode(global.LLMAccessMode)
	if !ValidAccessMode(mode) {
		return nil, ErrInvalidAccessMode
	}
	if mode == AccessModePlatformManaged {
		apiKey, err := s.decrypt(global.PublicLLMAPIKeyEncrypted)
		if err != nil {
			return nil, err
		}
		out := platformSettings(global, apiKey)
		if strings.TrimSpace(out.BaseURL) == "" || strings.TrimSpace(out.APIKey) == "" || strings.TrimSpace(out.Model) == "" {
			return nil, ErrPlatformIncomplete
		}
		return out, nil
	}

	out, err := s.users.Get(ctx, playerID)
	if err != nil {
		return nil, err
	}
	out.LLMRoute = normalizeLLMRoute(out.LLMRoute)
	// client 路由的 Key 只存浏览器：服务端不解密、不要求 Key，APIKey 为空。
	if out.LLMRoute == storage.LLMRouteClient {
		out.APIKey = ""
		out.APIKeyEncrypted = ""
		out.HasAPIKey = false
		out.AccessMode = AccessModeUserManaged
		out.Source = SourceClient
		out.Editable = true
		return out, nil
	}
	out.APIKey, err = s.decrypt(out.APIKeyEncrypted)
	if err != nil {
		return nil, err
	}
	out.HasAPIKey = out.APIKey != ""
	out.AccessMode = AccessModeUserManaged
	out.Source = SourceUser
	out.Editable = true
	return out, nil
}

func (s *Service) UserView(ctx context.Context, playerID string) (*storage.LLMSettings, error) {
	out, err := s.Get(ctx, playerID)
	if err != nil {
		return nil, err
	}
	out.HasAPIKey = out.APIKey != ""
	out.APIKey = ""
	out.APIKeyEncrypted = ""
	return out, nil
}

// UpdateUser preserves the existing key when apiKey is nil. A supplied empty
// key is rejected because user-managed mode cannot operate without one.
// route 为新的 llm_route：切到 client 时忽略 apiKey 并在同一事务中清除服务端 Key
// （切回 server 需重新填写）；client 路由不触发 ErrMissingAPIKey；路由变化递增 revision。
func (s *Service) UpdateUser(ctx context.Context, playerID string, next *storage.LLMSettings, apiKey *string, route string) (*storage.LLMSettings, error) {
	mode, err := s.AccessMode(ctx)
	if err != nil {
		return nil, err
	}
	if mode != AccessModeUserManaged {
		return nil, ErrUserSettingsDisabled
	}
	route = normalizeLLMRoute(route)
	if !ValidLLMRoute(route) {
		return nil, ErrInvalidLLMRoute
	}
	current, err := s.users.Get(ctx, playerID)
	if err != nil {
		return nil, err
	}
	next.LLMRoute = route
	if route == storage.LLMRouteClient {
		next.APIKeyEncrypted = ""
	} else {
		next.APIKeyEncrypted = current.APIKeyEncrypted
		if apiKey != nil {
			plain := strings.TrimSpace(*apiKey)
			if plain == "" {
				return nil, ErrMissingAPIKey
			}
			next.APIKeyEncrypted, err = s.encrypt(plain)
			if err != nil {
				return nil, err
			}
		}
		if next.APIKeyEncrypted == "" {
			return nil, ErrMissingAPIKey
		}
	}
	next.APIKey = ""
	if err := s.users.Save(ctx, playerID, next); err != nil {
		return nil, err
	}
	return s.UserView(ctx, playerID)
}

func (s *Service) AdminView(ctx context.Context) (*storage.SystemSettings, error) {
	settings, err := s.system.Get(ctx)
	if err != nil {
		return nil, err
	}
	settings.LLMAccessMode = normalizeAccessMode(settings.LLMAccessMode)
	settings.PublicLLMHasAPIKey = settings.PublicLLMAPIKeyEncrypted != ""
	return settings, nil
}

// UpdatePlatform applies all global fields and handles the write-only API key.
func (s *Service) UpdatePlatform(ctx context.Context, next *storage.SystemSettings, apiKey *string, clearAPIKey bool) (*storage.SystemSettings, error) {
	if !ValidAccessMode(normalizeAccessMode(next.LLMAccessMode)) {
		return nil, ErrInvalidAccessMode
	}
	current, err := s.system.Get(ctx)
	if err != nil {
		return nil, err
	}
	next.LLMAccessMode = normalizeAccessMode(next.LLMAccessMode)
	next.PublicLLMBaseURL = strings.TrimSpace(next.PublicLLMBaseURL)
	next.PublicLLMProvider = strings.TrimSpace(next.PublicLLMProvider)
	next.PublicLLMProtocol = strings.TrimSpace(next.PublicLLMProtocol)
	next.PublicLLMModel = strings.TrimSpace(next.PublicLLMModel)
	next.PublicLLMMemoryModel = strings.TrimSpace(next.PublicLLMMemoryModel)
	next.PublicLLMInquiryModel = strings.TrimSpace(next.PublicLLMInquiryModel)
	next.PublicLLMReasoningEffort = strings.TrimSpace(next.PublicLLMReasoningEffort)
	next.PublicLLMMemoryReasoningEffort = strings.TrimSpace(next.PublicLLMMemoryReasoningEffort)
	next.PublicLLMInquiryReasoningEffort = strings.TrimSpace(next.PublicLLMInquiryReasoningEffort)
	if next.PublicLLMProtocol == "" {
		next.PublicLLMProtocol = storage.DefaultLLMProtocol
	}
	if next.PublicLLMProvider == "" {
		next.PublicLLMProvider = "openai"
	}
	if next.PublicLLMMaxContextTokens == 0 {
		next.PublicLLMMaxContextTokens = storage.DefaultLLMMaxContextTokens
	}
	if next.PublicLLMMaxResponseTokens == 0 {
		next.PublicLLMMaxResponseTokens = storage.DefaultLLMMaxResponseTokens
	}
	next.PublicLLMAPIKeyEncrypted = current.PublicLLMAPIKeyEncrypted
	keyChanged := false
	if clearAPIKey {
		next.PublicLLMAPIKeyEncrypted = ""
		keyChanged = current.PublicLLMAPIKeyEncrypted != ""
	} else if apiKey != nil && strings.TrimSpace(*apiKey) != "" {
		next.PublicLLMAPIKeyEncrypted, err = s.encrypt(strings.TrimSpace(*apiKey))
		if err != nil {
			return nil, err
		}
		keyChanged = true
	}
	if next.LLMAccessMode == AccessModePlatformManaged &&
		(strings.TrimSpace(next.PublicLLMBaseURL) == "" || next.PublicLLMAPIKeyEncrypted == "" || strings.TrimSpace(next.PublicLLMModel) == "") {
		return nil, ErrPlatformIncomplete
	}
	if keyChanged || !samePlatformInvocation(current, next) || current.LLMAccessMode != next.LLMAccessMode {
		next.PublicLLMSettingsRevision = current.PublicLLMSettingsRevision + 1
		if next.PublicLLMSettingsRevision < 1 {
			next.PublicLLMSettingsRevision = 1
		}
	} else {
		next.PublicLLMSettingsRevision = current.PublicLLMSettingsRevision
		if next.PublicLLMSettingsRevision < 1 {
			next.PublicLLMSettingsRevision = 1
		}
	}
	if err := s.system.Save(ctx, next); err != nil {
		return nil, err
	}
	return s.AdminView(ctx)
}

func (s *Service) PlatformAPIKey(ctx context.Context) (string, error) {
	settings, err := s.system.Get(ctx)
	if err != nil {
		return "", err
	}
	return s.decrypt(settings.PublicLLMAPIKeyEncrypted)
}

func ValidAccessMode(mode string) bool {
	return mode == AccessModeUserManaged || mode == AccessModePlatformManaged
}

// ValidLLMRoute 报告路由取值是否合法。
func ValidLLMRoute(route string) bool {
	return route == storage.LLMRouteServer || route == storage.LLMRouteClient
}

// normalizeLLMRoute 清洗路由取值，空值默认为 server。
func normalizeLLMRoute(route string) string {
	route = strings.TrimSpace(route)
	if route == "" {
		return storage.LLMRouteServer
	}
	return route
}

func normalizeAccessMode(mode string) string {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		return AccessModeUserManaged
	}
	return mode
}

func platformSettings(s *storage.SystemSettings, apiKey string) *storage.LLMSettings {
	provider := strings.TrimSpace(s.PublicLLMProvider)
	if provider == "" {
		provider = "openai"
	}
	return &storage.LLMSettings{
		Provider: provider, BaseURL: s.PublicLLMBaseURL, Protocol: s.PublicLLMProtocol, APIKey: apiKey, HasAPIKey: apiKey != "",
		AccessMode: AccessModePlatformManaged, Source: SourcePlatform, Editable: false,
		LLMRoute: storage.LLMRouteServer,
		Model:    s.PublicLLMModel, MemoryModel: s.PublicLLMMemoryModel, InquiryModel: s.PublicLLMInquiryModel,
		MaxContextTokens: s.PublicLLMMaxContextTokens, MaxResponseTokens: s.PublicLLMMaxResponseTokens,
		Temperature: s.PublicLLMTemperature, TopP: s.PublicLLMTopP, StreamEnabled: s.PublicLLMStreamEnabled,
		ReasoningEffort:        s.PublicLLMReasoningEffort,
		MemoryReasoningEffort:  s.PublicLLMMemoryReasoningEffort,
		InquiryReasoningEffort: s.PublicLLMInquiryReasoningEffort,
		SettingsRevision:       s.PublicLLMSettingsRevision, UpdatedAt: s.UpdatedAt,
	}
}

func samePlatformInvocation(a, b *storage.SystemSettings) bool {
	return a.LLMAccessMode == b.LLMAccessMode && a.PublicLLMBaseURL == b.PublicLLMBaseURL && a.PublicLLMProvider == b.PublicLLMProvider && a.PublicLLMProtocol == b.PublicLLMProtocol &&
		a.PublicLLMModel == b.PublicLLMModel && a.PublicLLMMemoryModel == b.PublicLLMMemoryModel &&
		a.PublicLLMInquiryModel == b.PublicLLMInquiryModel && a.PublicLLMMaxContextTokens == b.PublicLLMMaxContextTokens &&
		a.PublicLLMMaxResponseTokens == b.PublicLLMMaxResponseTokens && a.PublicLLMTemperature == b.PublicLLMTemperature &&
		a.PublicLLMTopP == b.PublicLLMTopP && a.PublicLLMStreamEnabled == b.PublicLLMStreamEnabled &&
		a.PublicLLMReasoningEffort == b.PublicLLMReasoningEffort &&
		a.PublicLLMMemoryReasoningEffort == b.PublicLLMMemoryReasoningEffort &&
		a.PublicLLMInquiryReasoningEffort == b.PublicLLMInquiryReasoningEffort
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
	return s.box.Decrypt(value)
}
