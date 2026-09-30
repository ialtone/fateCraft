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
	ErrUserSettingsDisabled    = errors.New("平台当前统一提供模型配置，用户设置不可修改")
	ErrEncryptionUnavailable   = errors.New("服务端尚未配置 AUTH_DATA_ENCRYPTION_KEY，LLM API Key 无论是用户配置还是平台公共配置都必须先加密后保存")
	ErrMissingAPIKey           = errors.New("API Key 不能为空")
	ErrConfigurationIncomplete = errors.New("模型 API 尚未配置完整，请先在设置中填写 Base URL、API Key 和模型")
	ErrInvalidAccessMode       = errors.New("llm_access_mode 必须是 user_managed 或 platform_managed")
	ErrPlatformIncomplete      = errors.New("平台模型模式需要完整配置 Base URL、API Key 和叙事模型")
	ErrInvalidLLMRoute         = errors.New("llm_route 必须是 server 或 client")
	ErrClientRouteDisabled     = errors.New("平台未开放客户端直连")
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

// resolveRoute 按“平台开关 × 用户保存值”计算有效路由，是全站有效路由的
// 唯一计算点，与 llm_access_mode 无关。关闭开关时不改写用户保存值，只是忽略它。
func resolveRoute(global *storage.SystemSettings, storedRoute string) string {
	if !global.ClientLLMRouteEnabled {
		return storage.LLMRouteServer
	}
	return normalizeLLMRoute(storedRoute)
}

// EffectiveRoute 返回该玩家的有效路由：平台开关关闭时恒为 server，否则为
// 用户保存的 llm_route（默认为 server）。切换公共 LLM 不改变有效路由。
func (s *Service) EffectiveRoute(ctx context.Context, playerID string) (string, error) {
	global, err := s.system.Get(ctx)
	if err != nil {
		return "", err
	}
	if mode := normalizeAccessMode(global.LLMAccessMode); !ValidAccessMode(mode) {
		return "", ErrInvalidAccessMode
	}
	stored, err := s.users.Get(ctx, playerID)
	if err != nil {
		return "", err
	}
	return resolveRoute(global, stored.LLMRoute), nil
}

// ReadyRoute checks configuration before creating any game state and returns
// the route from the same runtime snapshot. Client keys never reach the server:
// their presence must be checked on the device before submitting creation.
func (s *Service) ReadyRoute(ctx context.Context, playerID string) (string, error) {
	settings, err := s.Get(ctx, playerID)
	if err != nil {
		return "", err
	}
	if !configurationReady(settings.BaseURL, settings.Model, settings.LLMRoute == storage.LLMRouteClient || strings.TrimSpace(settings.APIKey) != "") {
		return "", ErrConfigurationIncomplete
	}
	return settings.LLMRoute, nil
}

func configurationReady(baseURL, model string, hasKey bool) bool {
	return strings.TrimSpace(baseURL) != "" && strings.TrimSpace(model) != "" && hasKey
}

// Get returns a runtime-ready configuration. APIKey is populated only in
// memory and must never be serialized or logged.
//
// 先算有效路由再分派：client（任意接入模式）返回用户个人配置，不检查平台
// 配置是否完整；platform_managed 的 server 返回平台配置；user_managed 的
// server 返回用户个人配置并解密服务端 Key。
func (s *Service) Get(ctx context.Context, playerID string) (*storage.LLMSettings, error) {
	global, err := s.system.Get(ctx)
	if err != nil {
		return nil, err
	}
	mode := normalizeAccessMode(global.LLMAccessMode)
	if !ValidAccessMode(mode) {
		return nil, ErrInvalidAccessMode
	}
	stored, err := s.users.Get(ctx, playerID)
	if err != nil {
		return nil, err
	}
	if resolveRoute(global, stored.LLMRoute) == storage.LLMRouteClient {
		out := stored
		out.LLMRoute = storage.LLMRouteClient
		// client 路由的 Key 只存浏览器：服务端不解密、不要求 Key，APIKey 为空。
		out.APIKey = ""
		out.APIKeyEncrypted = ""
		out.HasAPIKey = false
		out.AccessMode = mode
		out.Source = SourceClient
		out.Editable = true
		out.ClientRouteEnabled = global.ClientLLMRouteEnabled
		return out, nil
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
		out.ClientRouteEnabled = global.ClientLLMRouteEnabled
		return out, nil
	}

	out := stored
	// 有效路由覆盖用户保存值：开关关闭时按 server 计算（用户保存的 client 被忽略，
	// 重新开启后自动恢复）；Key 为空时 HasAPIKey=false，走“未配置 Key”行为。
	out.LLMRoute = resolveRoute(global, out.LLMRoute)
	out.ClientRouteEnabled = global.ClientLLMRouteEnabled
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

// Personal 返回用户保存的个人配置行（不解密 Key），用于设置页展示与保存时
// 的合并基准。LLMRoute 设为有效路由；普通用户经此看不到平台 base_url 与模型。
func (s *Service) Personal(ctx context.Context, playerID string) (*storage.LLMSettings, error) {
	global, err := s.system.Get(ctx)
	if err != nil {
		return nil, err
	}
	mode := normalizeAccessMode(global.LLMAccessMode)
	if !ValidAccessMode(mode) {
		return nil, ErrInvalidAccessMode
	}
	stored, err := s.users.Get(ctx, playerID)
	if err != nil {
		return nil, err
	}
	out := stored
	out.LLMRoute = resolveRoute(global, stored.LLMRoute)
	out.HasAPIKey = out.APIKeyEncrypted != ""
	out.APIKey = ""
	out.APIKeyEncrypted = ""
	out.AccessMode = mode
	out.ClientRouteEnabled = global.ClientLLMRouteEnabled
	// Editable 含义是“个人配置是否可编辑”：user_managed，或开关开启的 platform_managed。
	out.Editable = mode == AccessModeUserManaged || global.ClientLLMRouteEnabled
	switch {
	case out.LLMRoute == storage.LLMRouteClient:
		out.Source = SourceClient
		out.ConfigurationReady = configurationReady(out.BaseURL, out.Model, true)
	case mode == AccessModePlatformManaged:
		out.Source = SourcePlatform
		out.ConfigurationReady = configurationReady(global.PublicLLMBaseURL, global.PublicLLMModel, global.PublicLLMAPIKeyEncrypted != "")
	default:
		out.Source = SourceUser
		out.ConfigurationReady = configurationReady(out.BaseURL, out.Model, out.HasAPIKey)
	}
	return out, nil
}

// PlatformRuntime 直接返回平台配置（解密平台 Key，含完整性检查），与任何
// 玩家的个人路由无关，供管理员公共配置诊断使用。
func (s *Service) PlatformRuntime(ctx context.Context) (*storage.LLMSettings, error) {
	global, err := s.system.Get(ctx)
	if err != nil {
		return nil, err
	}
	if mode := normalizeAccessMode(global.LLMAccessMode); !ValidAccessMode(mode) {
		return nil, ErrInvalidAccessMode
	}
	apiKey, err := s.decrypt(global.PublicLLMAPIKeyEncrypted)
	if err != nil {
		return nil, err
	}
	out := platformSettings(global, apiKey)
	if strings.TrimSpace(out.BaseURL) == "" || strings.TrimSpace(out.APIKey) == "" || strings.TrimSpace(out.Model) == "" {
		return nil, ErrPlatformIncomplete
	}
	out.ClientRouteEnabled = global.ClientLLMRouteEnabled
	return out, nil
}

func (s *Service) UserView(ctx context.Context, playerID string) (*storage.LLMSettings, error) {
	// 基于 Personal 而非 Get：platform_managed 下客户端直连表单需要回显用户
	// 个人配置，而不是平台配置；平台不完整时设置页仍可打开。
	return s.Personal(ctx, playerID)
}

// UpdateUser preserves the existing key when apiKey is nil. A supplied empty
// key is rejected because user-managed mode cannot operate without one.
// route 为新的 llm_route：切到 client 时忽略 apiKey 并在同一事务中清除服务端 Key
// （切回 server 需重新填写）；client 路由不触发 ErrMissingAPIKey；路由变化递增 revision。
//
// platform_managed 下：开关关闭时一律 ErrUserSettingsDisabled；开关开启时允许
// 保存客户端直连个人配置，或只把 llm_route 写回 server（保留个人配置，其余
// 请求字段被忽略，也不要求 Key）。
func (s *Service) UpdateUser(ctx context.Context, playerID string, next *storage.LLMSettings, apiKey *string, route string) (*storage.LLMSettings, error) {
	global, err := s.system.Get(ctx)
	if err != nil {
		return nil, err
	}
	mode := normalizeAccessMode(global.LLMAccessMode)
	if !ValidAccessMode(mode) {
		return nil, ErrInvalidAccessMode
	}
	route = normalizeLLMRoute(route)
	if !ValidLLMRoute(route) {
		return nil, ErrInvalidLLMRoute
	}
	if mode == AccessModePlatformManaged {
		if !global.ClientLLMRouteEnabled {
			return nil, ErrUserSettingsDisabled
		}
		if route == storage.LLMRouteServer {
			// “使用平台模型”：只改路由，个人配置字段与服务端 Key 原样保留，
			// 下次切回客户端直连时直接可用。
			current, err := s.users.Get(ctx, playerID)
			if err != nil {
				return nil, err
			}
			current.LLMRoute = storage.LLMRouteServer
			current.APIKey = ""
			if err := s.users.Save(ctx, playerID, current); err != nil {
				return nil, err
			}
			return s.UserView(ctx, playerID)
		}
	}
	if route == storage.LLMRouteClient && !global.ClientLLMRouteEnabled {
		return nil, ErrClientRouteDisabled
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
	return &storage.LLMSettings{
		BaseURL: s.PublicLLMBaseURL, Protocol: s.PublicLLMProtocol, APIKey: apiKey, HasAPIKey: apiKey != "",
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
	return a.LLMAccessMode == b.LLMAccessMode && a.PublicLLMBaseURL == b.PublicLLMBaseURL && a.PublicLLMProtocol == b.PublicLLMProtocol &&
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
