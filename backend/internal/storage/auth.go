package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	UserStatusActive   = "active"
	UserStatusDisabled = "disabled"
	UserRoleUser       = "user"
	UserRoleAdmin      = "admin"
	UserRoleOwner      = "owner"

	// OAuthPurposeLogin 与 OAuthPurposeBind 对应 oauth_login_flows.purpose 的 CHECK 约束取值。
	OAuthPurposeLogin = "login"
	OAuthPurposeBind  = "bind"
)

// users 表唯一约束/索引的名称，必须与 init.sql 保持一致；冲突识别依赖名称而不是错误文本。
const (
	usersUsernameUniqueIndex   = "idx_users_username_lower"
	usersEmailUniqueConstraint = "users_email_key"
	pgUniqueViolationCode      = "23505"
)

var (
	ErrInstallationComplete = errors.New("实例已完成初始化")
	ErrOwnerRequired        = errors.New("需要平台拥有者权限")
	ErrLastOwner            = errors.New("不能移除、停用或降级唯一的 owner")
	ErrInvalidUserRole      = errors.New("无效的用户角色")
	ErrInvalidUserStatus    = errors.New("无效的用户状态")
	ErrInstallationHasUsers = errors.New("实例已有用户，不能通过首次安装创建 owner")
	ErrIdentityAlreadyBound = errors.New("该第三方账户已经绑定到其他用户")
	ErrIdentityNotFound     = errors.New("第三方账户不存在")
	ErrLastAuthMethod       = errors.New("不能解除账号的最后一种登录方式")
	ErrUsernameInUse        = errors.New("用户名已被使用")
	ErrEmailInUse           = errors.New("邮箱已被注册")
	ErrTargetUserDisabled   = errors.New("目标账号已停用")
	ErrUserNotBindable      = errors.New("账号当前不可绑定第三方账户")
)

// User 是站内用户。Username 是本地登录标识，按 lower(username) 大小写不敏感唯一；
// PasswordHash 用于本地密码登录，外部身份用户可为空。
type User struct {
	ID           uuid.UUID  `gorm:"column:id;primaryKey;type:uuid" json:"id"`
	Username     string     `gorm:"column:username;not null" json:"username"`
	Email        *string    `gorm:"column:email;uniqueIndex" json:"email,omitempty"`
	PasswordHash *string    `gorm:"column:password_hash" json:"-"`
	DisplayName  string     `gorm:"column:display_name;not null" json:"display_name"`
	AvatarURL    *string    `gorm:"column:avatar_url" json:"avatar_url,omitempty"`
	Status       string     `gorm:"column:status;not null;default:active" json:"status"`
	Role         string     `gorm:"column:role;size:16;not null;default:user" json:"role"`
	LastLoginAt  *time.Time `gorm:"column:last_login_at" json:"last_login_at,omitempty"`
	CreatedAt    time.Time  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt    time.Time  `gorm:"column:updated_at" json:"updated_at"`
}

func (User) TableName() string { return "users" }

func (u User) IsAdmin() bool { return u.Role == UserRoleAdmin || u.Role == UserRoleOwner }

func (u User) IsOwner() bool { return u.Role == UserRoleOwner }

// AuthIdentity 把 LinuxDo 等外部账号绑定到站内用户。
type AuthIdentity struct {
	ID             uuid.UUID `gorm:"column:id;primaryKey;type:uuid" json:"id"`
	UserID         uuid.UUID `gorm:"column:user_id;type:uuid;not null;index" json:"user_id"`
	Provider       string    `gorm:"column:provider;not null;uniqueIndex:idx_auth_identity_provider_subject" json:"provider"`
	ProviderUserID string    `gorm:"column:provider_user_id;not null;uniqueIndex:idx_auth_identity_provider_subject" json:"provider_user_id"`
	Username       string    `gorm:"column:username" json:"username"`
	ProviderEmail  *string   `gorm:"column:provider_email" json:"provider_email,omitempty"`
	TrustLevel     *int      `gorm:"column:trust_level" json:"trust_level,omitempty"`
	Profile        JSONMap   `gorm:"column:profile;type:jsonb;not null" json:"profile"`
	CreatedAt      time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt      time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (AuthIdentity) TableName() string { return "auth_identities" }

// AuthSession 只保存随机会话令牌的 SHA-256 摘要，原始令牌仅存在于 Cookie。
type AuthSession struct {
	TokenHash string    `gorm:"column:token_hash;primaryKey;size:64"`
	UserID    uuid.UUID `gorm:"column:user_id;type:uuid;not null;index"`
	ExpiresAt time.Time `gorm:"column:expires_at;not null;index"`
	CreatedAt time.Time `gorm:"column:created_at;not null"`
}

func (AuthSession) TableName() string { return "auth_sessions" }

// OAuthLoginFlow 保存短期、一次性的 state 与 PKCE verifier。
// BrowserBindingHash 是发起授权时下发给浏览器的 nonce Cookie 的 SHA-256，
// 回调必须带回同一个 nonce，防止授权结果被转交给其他浏览器使用。
type OAuthLoginFlow struct {
	StateHash          string     `gorm:"column:state_hash;primaryKey;size:64"`
	Provider           string     `gorm:"column:provider;not null"`
	CodeVerifier       string     `gorm:"column:code_verifier;not null"`
	Purpose            string     `gorm:"column:purpose;not null;default:login"`
	UserID             *uuid.UUID `gorm:"column:user_id;type:uuid"`
	BrowserBindingHash string     `gorm:"column:browser_binding_hash;size:64;not null"`
	ReturnTo           string     `gorm:"column:return_to;not null"`
	ExpiresAt          time.Time  `gorm:"column:expires_at;not null;index"`
	CreatedAt          time.Time  `gorm:"column:created_at;not null"`
}

func (OAuthLoginFlow) TableName() string { return "oauth_login_flows" }

// AuthSettings 是管理员维护的站点认证策略与发信配置，整站只有 id=1 一行。
type AuthSettings struct {
	ID                         int       `gorm:"column:id;primaryKey" json:"id"`
	EmailAuthEnabled           bool      `gorm:"column:email_auth_enabled;not null;default:true" json:"email_auth_enabled"`
	LinuxDOLoginEnabled        bool      `gorm:"column:linuxdo_login_enabled;not null;default:true" json:"linuxdo_login_enabled"`
	EmailRegistrationEnabled   bool      `gorm:"column:email_registration_enabled;not null;default:false" json:"email_registration_enabled"`
	EmailVerificationEnabled   bool      `gorm:"column:email_verification_enabled;not null;default:true" json:"email_verification_enabled"`
	InvitationRequired         bool      `gorm:"column:invitation_required;not null;default:false" json:"invitation_required"`
	SMTPHost                   string    `gorm:"column:smtp_host;not null;default:''" json:"smtp_host"`
	SMTPPort                   int       `gorm:"column:smtp_port;not null;default:587" json:"smtp_port"`
	SMTPSecurity               string    `gorm:"column:smtp_security;size:16;not null;default:starttls" json:"smtp_security"`
	SMTPUsername               string    `gorm:"column:smtp_username;not null;default:''" json:"smtp_username"`
	SMTPPasswordEncrypted      string    `gorm:"column:smtp_password_encrypted;not null;default:''" json:"-"`
	SenderEmail                string    `gorm:"column:sender_email;not null;default:''" json:"sender_email"`
	SenderName                 string    `gorm:"column:sender_name;not null;default:Fatecraft" json:"sender_name"`
	VerificationSubject        string    `gorm:"column:verification_subject;not null" json:"verification_subject"`
	VerificationTemplate       string    `gorm:"column:verification_template;not null" json:"verification_template"`
	VerificationCodeTTLMinutes int       `gorm:"column:verification_code_ttl_minutes;not null;default:10" json:"verification_code_ttl_minutes"`
	VerificationResendSeconds  int       `gorm:"column:verification_resend_seconds;not null;default:60" json:"verification_resend_seconds"`
	UpdatedAt                  time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (AuthSettings) TableName() string { return "auth_settings" }

func DefaultAuthSettings() *AuthSettings {
	return &AuthSettings{
		ID: 1, EmailAuthEnabled: true, LinuxDOLoginEnabled: true, EmailVerificationEnabled: true,
		SMTPPort: 587, SMTPSecurity: "starttls", SenderName: "Fatecraft",
		VerificationSubject:        "【Fatecraft】邮箱验证码",
		VerificationTemplate:       "你的验证码是：{{code}}\n\n验证码将在 {{expires_minutes}} 分钟后失效。如非本人操作，请忽略此邮件。",
		VerificationCodeTTLMinutes: 10, VerificationResendSeconds: 60,
	}
}

// EmailVerificationCode 只保存验证码的 bcrypt 摘要，验证码本身仅存在于邮件中。
type EmailVerificationCode struct {
	ID           uuid.UUID  `gorm:"column:id;primaryKey;type:uuid"`
	Email        string     `gorm:"column:email;size:320;not null;index"`
	Purpose      string     `gorm:"column:purpose;size:32;not null"`
	CodeHash     string     `gorm:"column:code_hash;not null"`
	AttemptCount int        `gorm:"column:attempt_count;not null;default:0"`
	ExpiresAt    time.Time  `gorm:"column:expires_at;not null;index"`
	ConsumedAt   *time.Time `gorm:"column:consumed_at"`
	CreatedAt    time.Time  `gorm:"column:created_at;not null"`
}

func (EmailVerificationCode) TableName() string { return "email_verification_codes" }

// InvitationCode 保存高熵邀请码的 SHA-256 摘要；CodeHint 仅用于管理员识别。
type InvitationCode struct {
	ID        uuid.UUID  `gorm:"column:id;primaryKey;type:uuid" json:"id"`
	CodeHash  string     `gorm:"column:code_hash;size:64;not null;uniqueIndex" json:"-"`
	CodeHint  string     `gorm:"column:code_hint;size:8;not null" json:"code_hint"`
	Label     string     `gorm:"column:label;size:128;not null;default:''" json:"label"`
	Enabled   bool       `gorm:"column:enabled;not null;default:true" json:"enabled"`
	MaxUses   int        `gorm:"column:max_uses;not null;default:1" json:"max_uses"`
	UsedCount int        `gorm:"column:used_count;not null;default:0" json:"used_count"`
	ExpiresAt *time.Time `gorm:"column:expires_at" json:"expires_at,omitempty"`
	CreatedBy *uuid.UUID `gorm:"column:created_by;type:uuid" json:"created_by,omitempty"`
	CreatedAt time.Time  `gorm:"column:created_at;not null" json:"created_at"`
	UpdatedAt time.Time  `gorm:"column:updated_at;not null" json:"updated_at"`
}

func (InvitationCode) TableName() string { return "invitation_codes" }

type ExternalIdentityInput struct {
	Provider       string
	ProviderUserID string
	Username       string
	Email          *string
	DisplayName    string
	AvatarURL      *string
	TrustLevel     *int
	Profile        JSONMap
}

type ExternalIdentitySummary struct {
	ID             uuid.UUID `json:"id"`
	Provider       string    `json:"provider"`
	ProviderUserID string    `json:"provider_user_id"`
	Username       string    `json:"username"`
	ProviderEmail  *string   `json:"provider_email,omitempty"`
	TrustLevel     *int      `json:"trust_level,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// AdminExternalIdentitySummary is intentionally redacted: administrators can
// audit bindings without receiving provider profile payloads or credentials.
type AdminExternalIdentitySummary struct {
	ID             uuid.UUID `json:"id"`
	UserID         uuid.UUID `json:"user_id"`
	Provider       string    `json:"provider"`
	ProviderUserID string    `json:"provider_user_id"`
	Username       string    `json:"username"`
	ProviderEmail  *string   `json:"provider_email,omitempty"`
	UserName       string    `json:"user_name"`
	UserEmail      *string   `json:"user_email,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// AuthRepository 管理用户、登录身份、会话和一次性 OAuth 流程。
type AuthRepository struct{ db *gorm.DB }

func NewAuthRepository(db *gorm.DB) *AuthRepository { return &AuthRepository{db: db} }

func (r *AuthRepository) CreateOAuthFlow(ctx context.Context, flow *OAuthLoginFlow) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("expires_at <= ?", time.Now()).Delete(&OAuthLoginFlow{}).Error; err != nil {
			return err
		}
		return tx.Create(flow).Error
	})
}

func (r *AuthRepository) ConsumeOAuthFlow(ctx context.Context, provider, stateHash string, now time.Time) (*OAuthLoginFlow, error) {
	var flow OAuthLoginFlow
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("provider = ? AND state_hash = ?", provider, stateHash).
			First(&flow).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if !flow.ExpiresAt.After(now) {
			_ = tx.Delete(&flow).Error
			return ErrNotFound
		}
		return tx.Delete(&flow).Error
	})
	return &flow, err
}

func (r *AuthRepository) CreateSession(ctx context.Context, session *AuthSession) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("expires_at <= ?", time.Now()).Delete(&AuthSession{}).Error; err != nil {
			return err
		}
		return tx.Create(session).Error
	})
}

func (r *AuthRepository) DeleteSession(ctx context.Context, tokenHash string) error {
	return r.db.WithContext(ctx).Where("token_hash = ?", tokenHash).Delete(&AuthSession{}).Error
}

func (r *AuthRepository) UserBySession(ctx context.Context, tokenHash string, now time.Time) (*User, error) {
	var user User
	err := r.db.WithContext(ctx).Table("users").
		Select("users.*").
		Joins("JOIN auth_sessions ON auth_sessions.user_id = users.id").
		Where("auth_sessions.token_hash = ? AND auth_sessions.expires_at > ? AND users.status = ?", tokenHash, now, UserStatusActive).
		First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &user, err
}

func (r *AuthRepository) UserByEmail(ctx context.Context, email string) (*User, error) {
	var user User
	err := r.db.WithContext(ctx).Where("email = ?", email).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &user, err
}

// UserByIdentifier 按登录标识查找用户：含 "@" 时只按（调用方已规范化的）邮箱匹配，
// 否则按 lower(username) 大小写不敏感匹配。用户名禁止包含 "@"，两类标识不会互相冒用。
func (r *AuthRepository) UserByIdentifier(ctx context.Context, identifier string) (*User, error) {
	var user User
	query := r.db.WithContext(ctx)
	if strings.Contains(identifier, "@") {
		query = query.Where("email = ?", identifier)
	} else {
		query = query.Where("lower(username) = lower(?)", identifier)
	}
	err := query.First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &user, err
}

func (r *AuthRepository) CreateEmailUser(ctx context.Context, user *User) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if strings.TrimSpace(user.Username) == "" {
			return errors.New("本地用户缺少用户名")
		}
		user.Role = UserRoleUser
		if err := tx.Create(user).Error; err != nil {
			return err
		}
		return grantInitialRewards(ctx, tx, user.ID)
	})
}

var (
	ErrInvalidVerificationCode = errors.New("邮箱验证码无效或已失效")
	ErrInvalidInvitationCode   = errors.New("邀请码无效或已失效")
)

// RegisterEmailUser 在同一事务中消耗验证码、邀请码并创建用户，避免凭据被并发重复使用。
func (r *AuthRepository) RegisterEmailUser(ctx context.Context, user *User, verificationID *uuid.UUID, invitationHash *string, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if user.Email == nil {
			return errors.New("邮箱用户缺少邮箱地址")
		}
		if strings.TrimSpace(user.Username) == "" {
			return errors.New("邮箱用户缺少用户名")
		}
		if verificationID != nil {
			result := tx.Model(&EmailVerificationCode{}).
				Where("id = ? AND email = ? AND purpose = ? AND consumed_at IS NULL AND expires_at > ? AND attempt_count < 5",
					*verificationID, *user.Email, "registration", now).
				Update("consumed_at", now)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrInvalidVerificationCode
			}
		}

		if invitationHash != nil {
			var invitation InvitationCode
			err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("code_hash = ?", *invitationHash).First(&invitation).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrInvalidInvitationCode
			}
			if err != nil {
				return err
			}
			if !invitation.Enabled || (invitation.ExpiresAt != nil && !invitation.ExpiresAt.After(now)) ||
				(invitation.MaxUses > 0 && invitation.UsedCount >= invitation.MaxUses) {
				return ErrInvalidInvitationCode
			}
			invitation.UsedCount++
			if invitation.MaxUses > 0 && invitation.UsedCount >= invitation.MaxUses {
				invitation.Enabled = false
			}
			invitation.UpdatedAt = now
			if err := tx.Save(&invitation).Error; err != nil {
				return err
			}
		}

		user.Role = UserRoleUser
		if err := tx.Create(user).Error; err != nil {
			return translateUserUniqueViolation(err)
		}
		return grantInitialRewards(ctx, tx, user.ID)
	})
}

// translateUserUniqueViolation 按 PostgreSQL 唯一约束/索引名称识别用户名或邮箱冲突，
// 转换为对应哨兵错误；不依赖错误文本匹配，其余错误原样返回。
func translateUserUniqueViolation(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != pgUniqueViolationCode {
		return err
	}
	switch pgErr.ConstraintName {
	case usersUsernameUniqueIndex:
		return ErrUsernameInUse
	case usersEmailUniqueConstraint:
		return ErrEmailInUse
	default:
		return err
	}
}

// asNotFound 把 GORM 的“记录不存在”统一转换为 ErrNotFound，其余错误原样返回。
func asNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}

func (r *AuthRepository) GetAuthSettings(ctx context.Context) (*AuthSettings, error) {
	var settings AuthSettings
	err := r.db.WithContext(ctx).First(&settings, "id = ?", 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return DefaultAuthSettings(), nil
	}
	return &settings, err
}

func (r *AuthRepository) SaveAuthSettings(ctx context.Context, settings *AuthSettings) error {
	settings.ID = 1
	settings.UpdatedAt = time.Now()
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"email_auth_enabled", "linuxdo_login_enabled", "email_registration_enabled", "email_verification_enabled", "invitation_required",
			"smtp_host", "smtp_port", "smtp_security", "smtp_username", "smtp_password_encrypted",
			"sender_email", "sender_name", "verification_subject", "verification_template",
			"verification_code_ttl_minutes", "verification_resend_seconds", "updated_at",
		}),
	}).Create(settings).Error
}

func (r *AuthRepository) LatestEmailVerification(ctx context.Context, email, purpose string) (*EmailVerificationCode, error) {
	var code EmailVerificationCode
	err := r.db.WithContext(ctx).
		Where("email = ? AND purpose = ? AND consumed_at IS NULL", email, purpose).
		Order("created_at DESC").First(&code).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &code, err
}

func (r *AuthRepository) CreateEmailVerification(ctx context.Context, code *EmailVerificationCode) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("expires_at <= ? OR consumed_at IS NOT NULL", time.Now()).Delete(&EmailVerificationCode{}).Error; err != nil {
			return err
		}
		if err := tx.Where("email = ? AND purpose = ? AND consumed_at IS NULL", code.Email, code.Purpose).
			Delete(&EmailVerificationCode{}).Error; err != nil {
			return err
		}
		return tx.Create(code).Error
	})
}

func (r *AuthRepository) DeleteEmailVerification(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&EmailVerificationCode{}, "id = ?", id).Error
}

func (r *AuthRepository) IncrementEmailVerificationAttempts(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Model(&EmailVerificationCode{}).Where("id = ?", id).
		UpdateColumn("attempt_count", gorm.Expr("attempt_count + 1")).Error
}

func (r *AuthRepository) ListInvitationCodes(ctx context.Context) ([]InvitationCode, error) {
	var invitations []InvitationCode
	err := r.db.WithContext(ctx).Order("created_at DESC").Find(&invitations).Error
	return invitations, err
}

func (r *AuthRepository) InvitationCodeByID(ctx context.Context, id uuid.UUID) (*InvitationCode, error) {
	var invitation InvitationCode
	err := r.db.WithContext(ctx).First(&invitation, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &invitation, err
}

func (r *AuthRepository) CreateInvitationCode(ctx context.Context, invitation *InvitationCode) error {
	return r.db.WithContext(ctx).Create(invitation).Error
}

func (r *AuthRepository) UpdateInvitationCode(ctx context.Context, invitation *InvitationCode) error {
	return r.db.WithContext(ctx).Model(&InvitationCode{}).Where("id = ?", invitation.ID).Updates(map[string]any{
		"label": invitation.Label, "enabled": invitation.Enabled, "max_uses": invitation.MaxUses,
		"expires_at": invitation.ExpiresAt, "updated_at": invitation.UpdatedAt,
	}).Error
}

// InstallationComplete reports whether the fresh instance has completed the
// browser installation wizard.
func (r *AuthRepository) InstallationComplete(ctx context.Context) (bool, error) {
	var row struct {
		CompletedAt *time.Time `gorm:"column:installation_completed_at"`
	}
	err := r.db.WithContext(ctx).Table("system_settings").Select("installation_completed_at").Where("id = 1").Scan(&row).Error
	return row.CompletedAt != nil, err
}

// InitializeOwner 在一个事务中创建第一个本地账号并完成首次安装。
// 它只接受完全空的数据库，因此未认证的安装请求既不能提权已有用户，也不能劫持已安装实例。
func (r *AuthRepository) InitializeOwner(ctx context.Context, user *User) (*User, error) {
	var created User
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var settings struct {
			InstallationCompletedAt *time.Time `gorm:"column:installation_completed_at"`
		}
		if err := tx.Raw("SELECT installation_completed_at FROM system_settings WHERE id = 1 FOR UPDATE").Scan(&settings).Error; err != nil {
			return err
		}
		if settings.InstallationCompletedAt != nil {
			return ErrInstallationComplete
		}
		var count int64
		if err := tx.Model(&User{}).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return ErrInstallationHasUsers
		}
		user.Role = UserRoleOwner
		user.Status = UserStatusActive
		if err := tx.Create(user).Error; err != nil {
			return translateUserUniqueViolation(err)
		}
		if err := grantInitialRewards(ctx, tx, user.ID); err != nil {
			return err
		}
		now := time.Now()
		if err := tx.Model(&SystemSettings{}).Where("id = 1").Updates(map[string]any{"installation_completed_at": now, "updated_at": now}).Error; err != nil {
			return err
		}
		role := UserRoleOwner
		if err := tx.Create(&AdminAuditLog{ID: uuid.New(), TargetUserID: &user.ID, Action: "installation_complete", NewRole: &role, Result: "success", Metadata: JSONMap{"method": "first_account"}, CreatedAt: now}).Error; err != nil {
			return err
		}
		return tx.First(&created, "id = ?", user.ID).Error
	})
	return &created, err
}

// ListUsers 按创建时间倒序分页读取用户；limit/offset 的取值范围由调用方校验。
func (r *AuthRepository) ListUsers(ctx context.Context, limit, offset int) ([]User, error) {
	var users []User
	err := r.db.WithContext(ctx).Order("created_at DESC, id DESC").Limit(limit).Offset(offset).Find(&users).Error
	return users, err
}

// UpdateUserRole 由 owner 修改目标用户角色；目标为 owner 时表示转交，原 owner 同一事务内降为 admin。
func (r *AuthRepository) UpdateUserRole(ctx context.Context, actorID, targetID uuid.UUID, role string) (*User, error) {
	if role != UserRoleUser && role != UserRoleAdmin && role != UserRoleOwner {
		return nil, ErrInvalidUserRole
	}
	var target User
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var actor User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&actor, "id = ?", actorID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				// 操作者已不存在，按无权限处理，避免误报“目标用户不存在”。
				return ErrOwnerRequired
			}
			return err
		}
		if !actor.IsOwner() || actor.Status != UserStatusActive {
			return ErrOwnerRequired
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&target, "id = ?", targetID).Error; err != nil {
			return asNotFound(err)
		}
		if target.Status != UserStatusActive {
			return ErrTargetUserDisabled
		}
		if target.Role == UserRoleOwner && role != UserRoleOwner {
			return ErrLastOwner
		}
		if target.Role == role {
			return nil
		}
		now := time.Now()
		oldRole := target.Role
		if role == UserRoleOwner {
			var currentOwner User
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("role = ?", UserRoleOwner).First(&currentOwner).Error; err != nil {
				return asNotFound(err)
			}
			if currentOwner.ID != actor.ID {
				return ErrOwnerRequired
			}
			if err := tx.Model(&currentOwner).Updates(map[string]any{"role": UserRoleAdmin, "updated_at": now}).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&target).Updates(map[string]any{"role": role, "updated_at": now}).Error; err != nil {
			return err
		}
		actorCopy, targetCopy := actor.ID, target.ID
		old, next := oldRole, role
		if err := tx.Create(&AdminAuditLog{ID: uuid.New(), ActorUserID: &actorCopy, TargetUserID: &targetCopy, Action: "role_change", OldRole: &old, NewRole: &next, Result: "success", Metadata: JSONMap{}, CreatedAt: now}).Error; err != nil {
			return err
		}
		return tx.First(&target, "id = ?", target.ID).Error
	})
	return &target, err
}

// UpdateUserStatus 由 owner 启用或停用目标账号；owner 本身不能被停用。
func (r *AuthRepository) UpdateUserStatus(ctx context.Context, actorID, targetID uuid.UUID, status string) (*User, error) {
	if status != UserStatusActive && status != UserStatusDisabled {
		return nil, ErrInvalidUserStatus
	}
	var target User
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var actor User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&actor, "id = ?", actorID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				// 操作者已不存在，按无权限处理，避免误报“目标用户不存在”。
				return ErrOwnerRequired
			}
			return err
		}
		if !actor.IsOwner() || actor.Status != UserStatusActive {
			return ErrOwnerRequired
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&target, "id = ?", targetID).Error; err != nil {
			return asNotFound(err)
		}
		if target.Role == UserRoleOwner && status != UserStatusActive {
			return ErrLastOwner
		}
		now := time.Now()
		if err := tx.Model(&target).Updates(map[string]any{"status": status, "updated_at": now}).Error; err != nil {
			return err
		}
		actorCopy, targetCopy := actor.ID, target.ID
		if err := tx.Create(&AdminAuditLog{ID: uuid.New(), ActorUserID: &actorCopy, TargetUserID: &targetCopy, Action: "status_change", Result: "success", Metadata: JSONMap{"status": status}, CreatedAt: now}).Error; err != nil {
			return err
		}
		return tx.First(&target, "id = ?", target.ID).Error
	})
	return &target, err
}

// ListAdminAuditLogs 按时间倒序读取最近的管理员审计记录；limit 由调用方校验，这里只做兜底。
func (r *AuthRepository) ListAdminAuditLogs(ctx context.Context, limit int) ([]AdminAuditLog, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var logs []AdminAuditLog
	err := r.db.WithContext(ctx).Order("created_at DESC, id DESC").Limit(limit).Find(&logs).Error
	return logs, err
}

func (r *AuthRepository) ListExternalIdentities(ctx context.Context, userID uuid.UUID) ([]ExternalIdentitySummary, error) {
	var rows []ExternalIdentitySummary
	err := r.db.WithContext(ctx).Model(&AuthIdentity{}).
		Where("user_id = ?", userID).Order("created_at ASC").Find(&rows).Error
	return rows, err
}

// ListAllExternalIdentities 按绑定时间倒序分页读取全站第三方身份的脱敏摘要；limit/offset 由调用方校验。
func (r *AuthRepository) ListAllExternalIdentities(ctx context.Context, limit, offset int) ([]AdminExternalIdentitySummary, error) {
	var rows []AdminExternalIdentitySummary
	err := r.db.WithContext(ctx).Table("auth_identities AS ai").
		Select("ai.id, ai.user_id, ai.provider, ai.provider_user_id, ai.username, ai.provider_email, u.display_name AS user_name, u.email AS user_email, ai.created_at").
		Joins("JOIN users AS u ON u.id = ai.user_id").
		Order("ai.created_at DESC, ai.id DESC").Limit(limit).Offset(offset).Find(&rows).Error
	return rows, err
}

func (r *AuthRepository) ExternalUserByProviderSubject(ctx context.Context, provider, subject string) (*User, error) {
	var user User
	err := r.db.WithContext(ctx).Table("users").Select("users.*").
		Joins("JOIN auth_identities ON auth_identities.user_id = users.id").
		Where("auth_identities.provider = ? AND auth_identities.provider_user_id = ? AND users.status = ?", provider, subject, UserStatusActive).
		First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrIdentityNotFound
	}
	return &user, err
}

// BindExternalIdentity 把外部身份绑定到指定的本地用户；同一外部身份已属于其他用户时拒绝，
// 已属于该用户时只刷新身份快照。
func (r *AuthRepository) BindExternalIdentity(ctx context.Context, userID uuid.UUID, in ExternalIdentityInput) (*ExternalIdentitySummary, error) {
	var summary ExternalIdentitySummary
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user User
		if err := tx.First(&user, "id = ?", userID).Error; err != nil {
			return asNotFound(err)
		}
		if user.Status != UserStatusActive {
			return ErrUserNotBindable
		}
		if (user.AvatarURL == nil || strings.TrimSpace(*user.AvatarURL) == "") && in.AvatarURL != nil {
			if err := tx.Model(&user).Updates(map[string]any{"avatar_url": *in.AvatarURL, "updated_at": time.Now()}).Error; err != nil {
				return err
			}
		}
		var existing AuthIdentity
		err := tx.Where("provider = ? AND provider_user_id = ?", in.Provider, in.ProviderUserID).First(&existing).Error
		if err == nil {
			if existing.UserID != userID {
				return ErrIdentityAlreadyBound
			}
			if err := tx.Model(&existing).Updates(map[string]any{
				"username": in.Username, "provider_email": in.Email, "trust_level": in.TrustLevel,
				"profile": in.Profile, "updated_at": time.Now(),
			}).Error; err != nil {
				return err
			}
			return tx.Model(&existing).Select("id", "provider", "provider_user_id", "username", "provider_email", "trust_level", "created_at", "updated_at").First(&summary).Error
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		row := AuthIdentity{
			ID: uuid.New(), UserID: userID, Provider: in.Provider, ProviderUserID: in.ProviderUserID,
			Username: in.Username, ProviderEmail: in.Email, TrustLevel: in.TrustLevel,
			Profile: in.Profile, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return tx.Model(&row).Select("id", "provider", "provider_user_id", "username", "provider_email", "trust_level", "created_at", "updated_at").First(&summary).Error
	})
	return &summary, err
}

// UnbindExternalIdentity 删除用户自己的一个外部身份，并保证删除后账号仍至少保留一种可用的登录方式：
// 只有全站允许密码登录（passwordLoginEnabled）且账号设置了密码时，密码才算可用；
// 否则必须至少还剩一个外部身份。
func (r *AuthRepository) UnbindExternalIdentity(ctx context.Context, userID, identityID uuid.UUID, passwordLoginEnabled bool) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 先锁用户行，串行化同一账号的并发解绑，避免两个请求各自看到“还剩一个”后同时删除。
		var user User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, "id = ?", userID).Error; err != nil {
			return asNotFound(err)
		}
		var identity AuthIdentity
		if err := tx.Where("id = ? AND user_id = ?", identityID, userID).First(&identity).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrIdentityNotFound
			}
			return err
		}
		hasPassword := user.PasswordHash != nil && strings.TrimSpace(*user.PasswordHash) != ""
		if !passwordLoginEnabled || !hasPassword {
			var count int64
			if err := tx.Model(&AuthIdentity{}).Where("user_id = ?", userID).Count(&count).Error; err != nil {
				return err
			}
			if count <= 1 {
				if hasPassword {
					return fmt.Errorf("%w：站点已关闭密码登录，至少需要保留一个第三方账户", ErrLastAuthMethod)
				}
				return ErrLastAuthMethod
			}
		}
		return tx.Delete(&identity).Error
	})
}

func (r *AuthRepository) emailExists(db *gorm.DB, email string) bool {
	var count int64
	return db.Model(&User{}).Where("email = ?", email).Count(&count).Error == nil && count > 0
}

func grantInitialRewards(ctx context.Context, db *gorm.DB, playerID uuid.UUID) error {
	_, err := NewRewardRepository(db).GrantInCurrentTx(ctx, RewardMutation{
		PlayerID: playerID, RewardKey: MessageRewriteCreditKey, Quantity: 10,
		SourceType: "user_registration", SourceID: playerID.String(),
		IdempotencyKey: "user_registration:" + playerID.String() + ":" + MessageRewriteCreditKey,
		Metadata:       JSONMap{"reason": "initial_allowance"},
	})
	return err
}
