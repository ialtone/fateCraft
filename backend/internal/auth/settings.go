package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/identity"
	"github.com/ialtone/fatecraft/internal/secretbox"
	"github.com/ialtone/fatecraft/internal/storage"
	"golang.org/x/crypto/bcrypt"
)

const registrationPurpose = "registration"

type AdminAuthSettings struct {
	ID                         int       `json:"id"`
	EmailAuthEnabled           bool      `json:"email_auth_enabled"`
	LinuxDOLoginEnabled        bool      `json:"linuxdo_login_enabled"`
	EmailRegistrationEnabled   bool      `json:"email_registration_enabled"`
	EmailVerificationEnabled   bool      `json:"email_verification_enabled"`
	InvitationRequired         bool      `json:"invitation_required"`
	SMTPHost                   string    `json:"smtp_host"`
	SMTPPort                   int       `json:"smtp_port"`
	SMTPSecurity               string    `json:"smtp_security"`
	SMTPUsername               string    `json:"smtp_username"`
	HasSMTPPassword            bool      `json:"has_smtp_password"`
	SenderEmail                string    `json:"sender_email"`
	SenderName                 string    `json:"sender_name"`
	VerificationSubject        string    `json:"verification_subject"`
	VerificationTemplate       string    `json:"verification_template"`
	VerificationCodeTTLMinutes int       `json:"verification_code_ttl_minutes"`
	VerificationResendSeconds  int       `json:"verification_resend_seconds"`
	UpdatedAt                  time.Time `json:"updated_at"`
}

type UpdateAuthSettingsInput struct {
	EmailAuthEnabled           *bool   `json:"email_auth_enabled"`
	LinuxDOLoginEnabled        *bool   `json:"linuxdo_login_enabled"`
	EmailRegistrationEnabled   *bool   `json:"email_registration_enabled"`
	EmailVerificationEnabled   *bool   `json:"email_verification_enabled"`
	InvitationRequired         *bool   `json:"invitation_required"`
	SMTPHost                   *string `json:"smtp_host"`
	SMTPPort                   *int    `json:"smtp_port"`
	SMTPSecurity               *string `json:"smtp_security"`
	SMTPUsername               *string `json:"smtp_username"`
	SMTPPassword               string  `json:"smtp_password"`
	ClearSMTPPassword          bool    `json:"clear_smtp_password"`
	SenderEmail                *string `json:"sender_email"`
	SenderName                 *string `json:"sender_name"`
	VerificationSubject        *string `json:"verification_subject"`
	VerificationTemplate       *string `json:"verification_template"`
	VerificationCodeTTLMinutes *int    `json:"verification_code_ttl_minutes"`
	VerificationResendSeconds  *int    `json:"verification_resend_seconds"`
}

func (s *Service) AdminSettings(ctx context.Context) (*AdminAuthSettings, error) {
	settings, err := s.repo.GetAuthSettings(ctx)
	if err != nil {
		return nil, err
	}
	return adminSettingsView(settings), nil
}

// UpdateAdminSettings 由 owner（actorID）更新认证策略与发信配置。
// 关闭密码登录前必须确认 owner 仍有其他登录方式：LinuxDo 已配置、开关开启
// 且 owner 已绑定 LinuxDo 身份，否则拒绝保存，避免把 owner 锁在站外。
// 注意：本站允许同时关闭两种登录方式（之后无人能登录，已有会话过期前仍有效），
// 仅关闭密码登录时才做上述检查。
func (s *Service) UpdateAdminSettings(ctx context.Context, actorID uuid.UUID, in UpdateAuthSettingsInput) (*AdminAuthSettings, error) {
	ctx = identity.WithPlayerID(ctx, actorID.String())
	current, err := s.repo.GetAuthSettings(ctx)
	if err != nil {
		return nil, err
	}
	// PATCH fields omitted by another settings tab retain their persisted values.
	next := *current
	current = &next
	if in.EmailAuthEnabled != nil {
		current.EmailAuthEnabled = *in.EmailAuthEnabled
	}
	if in.LinuxDOLoginEnabled != nil {
		current.LinuxDOLoginEnabled = *in.LinuxDOLoginEnabled
	}
	if in.EmailRegistrationEnabled != nil {
		current.EmailRegistrationEnabled = *in.EmailRegistrationEnabled
	}
	if in.EmailVerificationEnabled != nil {
		current.EmailVerificationEnabled = *in.EmailVerificationEnabled
	}
	if in.InvitationRequired != nil {
		current.InvitationRequired = *in.InvitationRequired
	}
	if in.SMTPHost != nil {
		current.SMTPHost = strings.TrimSpace(*in.SMTPHost)
	}
	if in.SMTPPort != nil {
		current.SMTPPort = *in.SMTPPort
	}
	if in.SMTPSecurity != nil {
		current.SMTPSecurity = *in.SMTPSecurity
	}
	if in.SMTPUsername != nil {
		current.SMTPUsername = strings.TrimSpace(*in.SMTPUsername)
	}
	if in.SenderEmail != nil {
		current.SenderEmail = strings.ToLower(strings.TrimSpace(*in.SenderEmail))
	}
	if in.SenderName != nil {
		current.SenderName = strings.TrimSpace(*in.SenderName)
	}
	if in.VerificationSubject != nil {
		current.VerificationSubject = strings.TrimSpace(*in.VerificationSubject)
	}
	if in.VerificationTemplate != nil {
		current.VerificationTemplate = strings.TrimSpace(*in.VerificationTemplate)
	}
	if in.VerificationCodeTTLMinutes != nil {
		current.VerificationCodeTTLMinutes = *in.VerificationCodeTTLMinutes
	}
	if in.VerificationResendSeconds != nil {
		current.VerificationResendSeconds = *in.VerificationResendSeconds
	}
	if err := validateSettingsInput(current); err != nil {
		return nil, err
	}
	if current.EmailRegistrationEnabled && !current.EmailAuthEnabled {
		return nil, fmt.Errorf("%w: 开启邮箱注册时必须同时开启邮箱登录", ErrInvalidAuthSettings)
	}
	// 关闭本地密码登录后 owner 只能经 LinuxDo 登录，因此必须保留一条可用路径，不允许双关锁死实例。
	if !current.EmailAuthEnabled {
		if !current.LinuxDOLoginEnabled {
			return nil, fmt.Errorf("%w: 本地密码登录与 LinuxDo 登录不能同时关闭，否则所有账号都将无法登录", ErrInvalidAuthSettings)
		}
		if !s.cfg.LinuxDOEnabled() {
			return nil, fmt.Errorf("%w: LinuxDo 登录尚未配置，关闭密码登录后所有账号都将无法登录", ErrInvalidAuthSettings)
		}
		bound, err := s.hasLinuxDOIdentity(ctx, actorID)
		if err != nil {
			return nil, err
		}
		if !bound {
			return nil, fmt.Errorf("%w: 关闭密码登录前，请先在“设置 → 第三方账户”中为当前 owner 账号绑定 LinuxDo，否则 owner 将无法登录", ErrInvalidAuthSettings)
		}
	}

	if in.ClearSMTPPassword {
		current.SMTPPasswordEncrypted = ""
	} else if in.SMTPPassword != "" {
		if s.cipherErr != nil {
			return nil, ErrEncryptionNotConfigured
		}
		current.SMTPPasswordEncrypted, err = s.cipher.Encrypt(in.SMTPPassword)
		if err != nil {
			return nil, err
		}
	}
	if current.EmailVerificationEnabled && current.EmailRegistrationEnabled {
		if err := validateMailerSettings(current); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidAuthSettings, err)
		}
	}
	if err := s.repo.SaveAuthSettings(ctx, current); err != nil {
		return nil, err
	}
	return adminSettingsView(current), nil
}

func (s *Service) SendRegistrationCode(ctx context.Context, rawEmail string) error {
	settings, err := s.repo.GetAuthSettings(ctx)
	if err != nil {
		return err
	}
	if !settings.EmailRegistrationEnabled {
		return ErrEmailRegistrationDisabled
	}
	if !settings.EmailVerificationEnabled {
		return ErrEmailVerificationDisabled
	}
	email, err := normalizeEmail(rawEmail)
	if err != nil {
		return err
	}
	if _, err := s.repo.UserByEmail(ctx, email); err == nil {
		return ErrEmailInUse
	} else if !errors.Is(err, storage.ErrNotFound) {
		return err
	}
	if previous, err := s.repo.LatestEmailVerification(ctx, email, registrationPurpose); err == nil {
		if wait := time.Duration(settings.VerificationResendSeconds)*time.Second - time.Since(previous.CreatedAt); wait > 0 {
			return fmt.Errorf("%w，请在 %d 秒后重试", ErrVerificationCooldown, int(wait.Seconds())+1)
		}
	} else if !errors.Is(err, storage.ErrNotFound) {
		return err
	}
	if err := validateMailerSettings(settings); err != nil {
		return fmt.Errorf("%w: 发信配置不可用", ErrEmailSendFailed)
	}
	password, err := s.decryptSMTPPassword(settings)
	if err != nil {
		return err
	}
	code, err := numericCode(6)
	if err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(code), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	now := time.Now()
	record := &storage.EmailVerificationCode{
		ID: uuid.New(), Email: email, Purpose: registrationPurpose, CodeHash: string(hash),
		ExpiresAt: now.Add(time.Duration(settings.VerificationCodeTTLMinutes) * time.Minute), CreatedAt: now,
	}
	if err := s.repo.CreateEmailVerification(ctx, record); err != nil {
		return err
	}
	body := strings.NewReplacer(
		"{{code}}", code,
		"{{expires_minutes}}", fmt.Sprintf("%d", settings.VerificationCodeTTLMinutes),
	).Replace(settings.VerificationTemplate)
	err = s.mailer.Send(ctx, SMTPConfig{
		Host: settings.SMTPHost, Port: settings.SMTPPort, Security: settings.SMTPSecurity,
		Username: settings.SMTPUsername, Password: password,
		SenderEmail: settings.SenderEmail, SenderName: settings.SenderName,
	}, MailMessage{To: email, Subject: settings.VerificationSubject, Body: body})
	if err != nil {
		_ = s.repo.DeleteEmailVerification(ctx, record.ID)
		return fmt.Errorf("%w: %v", ErrEmailSendFailed, err)
	}
	return nil
}

func (s *Service) SendTestEmail(ctx context.Context, recipient string) error {
	settings, err := s.repo.GetAuthSettings(ctx)
	if err != nil {
		return err
	}
	recipient, err = normalizeEmail(recipient)
	if err != nil {
		return err
	}
	if err := validateMailerSettings(settings); err != nil {
		return err
	}
	password, err := s.decryptSMTPPassword(settings)
	if err != nil {
		return err
	}
	return s.mailer.Send(ctx, SMTPConfig{
		Host: settings.SMTPHost, Port: settings.SMTPPort, Security: settings.SMTPSecurity,
		Username: settings.SMTPUsername, Password: password,
		SenderEmail: settings.SenderEmail, SenderName: settings.SenderName,
	}, MailMessage{To: recipient, Subject: "fateCraft 发信测试", Body: "这是一封来自 fateCraft 管理后台的 SMTP 测试邮件。"})
}

func (s *Service) verifyRegistrationCode(ctx context.Context, email, code string, now time.Time) (*storage.EmailVerificationCode, error) {
	verification, err := s.repo.LatestEmailVerification(ctx, email, registrationPurpose)
	if err != nil || verification.ConsumedAt != nil || !verification.ExpiresAt.After(now) || verification.AttemptCount >= 5 {
		return nil, ErrInvalidVerificationCode
	}
	if bcrypt.CompareHashAndPassword([]byte(verification.CodeHash), []byte(strings.TrimSpace(code))) != nil {
		_ = s.repo.IncrementEmailVerificationAttempts(ctx, verification.ID)
		return nil, ErrInvalidVerificationCode
	}
	return verification, nil
}

func (s *Service) decryptSMTPPassword(settings *storage.AuthSettings) (string, error) {
	if settings.SMTPPasswordEncrypted == "" {
		return "", nil
	}
	if s.cipherErr != nil {
		return "", ErrEncryptionNotConfigured
	}
	return s.cipher.Decrypt(settings.SMTPPasswordEncrypted)
}

func adminSettingsView(s *storage.AuthSettings) *AdminAuthSettings {
	return &AdminAuthSettings{
		ID: s.ID, EmailAuthEnabled: s.EmailAuthEnabled, LinuxDOLoginEnabled: s.LinuxDOLoginEnabled, EmailRegistrationEnabled: s.EmailRegistrationEnabled,
		EmailVerificationEnabled: s.EmailVerificationEnabled, InvitationRequired: s.InvitationRequired,
		SMTPHost: s.SMTPHost, SMTPPort: s.SMTPPort, SMTPSecurity: s.SMTPSecurity, SMTPUsername: s.SMTPUsername,
		HasSMTPPassword: s.SMTPPasswordEncrypted != "", SenderEmail: s.SenderEmail, SenderName: s.SenderName,
		VerificationSubject: s.VerificationSubject, VerificationTemplate: s.VerificationTemplate,
		VerificationCodeTTLMinutes: s.VerificationCodeTTLMinutes, VerificationResendSeconds: s.VerificationResendSeconds,
		UpdatedAt: s.UpdatedAt,
	}
}

func validateSettingsInput(in *storage.AuthSettings) error {
	if in.SMTPPort < 1 || in.SMTPPort > 65535 {
		return fmt.Errorf("%w: SMTP 端口必须在 1-65535 之间", ErrInvalidAuthSettings)
	}
	if in.SMTPSecurity != "none" && in.SMTPSecurity != "starttls" && in.SMTPSecurity != "tls" {
		return fmt.Errorf("%w: SMTP 安全模式无效", ErrInvalidAuthSettings)
	}
	if in.VerificationCodeTTLMinutes < 1 || in.VerificationCodeTTLMinutes > 60 {
		return fmt.Errorf("%w: 验证码有效期必须在 1-60 分钟之间", ErrInvalidAuthSettings)
	}
	if in.VerificationResendSeconds < 30 || in.VerificationResendSeconds > 3600 {
		return fmt.Errorf("%w: 验证码重发间隔必须在 30-3600 秒之间", ErrInvalidAuthSettings)
	}
	if len(strings.TrimSpace(in.SenderName)) > 128 {
		return fmt.Errorf("%w: 发件人名称不能超过 128 个字符", ErrInvalidAuthSettings)
	}
	if strings.ContainsAny(in.SenderName, "\r\n") || strings.ContainsAny(in.VerificationSubject, "\r\n") {
		return fmt.Errorf("%w: 发件人名称和邮件主题不能包含换行", ErrInvalidAuthSettings)
	}
	if strings.TrimSpace(in.SenderEmail) != "" {
		if _, err := normalizeEmail(in.SenderEmail); err != nil {
			return fmt.Errorf("%w: 发件邮箱格式无效", ErrInvalidAuthSettings)
		}
	}
	if !strings.Contains(in.VerificationTemplate, "{{code}}") {
		return fmt.Errorf("%w: 验证码模板必须包含 {{code}} 占位符", ErrInvalidAuthSettings)
	}
	return nil
}

func validateMailerSettings(s *storage.AuthSettings) error {
	if strings.TrimSpace(s.SMTPHost) == "" || s.SMTPPort < 1 || strings.TrimSpace(s.SenderEmail) == "" {
		return fmt.Errorf("%w: 请先完整配置 SMTP 主机、端口和发件邮箱", ErrInvalidAuthSettings)
	}
	if _, err := normalizeEmail(s.SenderEmail); err != nil {
		return fmt.Errorf("%w: 发件邮箱格式无效", ErrInvalidAuthSettings)
	}
	return nil
}

func numericCode(length int) (string, error) {
	const digits = "0123456789"
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i := range buf {
		buf[i] = digits[int(buf[i])%len(digits)]
	}
	return string(buf), nil
}

func normalizeInvitationCode(raw string) string {
	normalized := strings.ToUpper(strings.TrimSpace(raw))
	return strings.NewReplacer("-", "", " ", "", "\t", "", "\r", "", "\n", "").Replace(normalized)
}

type credentialCipher interface {
	Encrypt(string) (string, error)
	Decrypt(string) (string, error)
}

func newCredentialCipher(rawKey string) (credentialCipher, error) { return secretbox.New(rawKey) }
