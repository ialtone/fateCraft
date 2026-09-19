package storage

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	UserStatusActive = "active"
	UserRoleUser     = "user"
	UserRoleAdmin    = "admin"
)

// User 是站内用户。PasswordHash 仅用于可选的邮箱登录，外部登录用户可为空。
type User struct {
	ID           uuid.UUID  `gorm:"column:id;primaryKey;type:uuid" json:"id"`
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

func (u User) IsAdmin() bool { return u.Role == UserRoleAdmin }

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
type OAuthLoginFlow struct {
	StateHash    string    `gorm:"column:state_hash;primaryKey;size:64"`
	Provider     string    `gorm:"column:provider;not null"`
	CodeVerifier string    `gorm:"column:code_verifier;not null"`
	ReturnTo     string    `gorm:"column:return_to;not null"`
	ExpiresAt    time.Time `gorm:"column:expires_at;not null;index"`
	CreatedAt    time.Time `gorm:"column:created_at;not null"`
}

func (OAuthLoginFlow) TableName() string { return "oauth_login_flows" }

// AuthSettings 是管理员维护的站点认证策略与发信配置，整站只有 id=1 一行。
type AuthSettings struct {
	ID                         int       `gorm:"column:id;primaryKey" json:"id"`
	EmailAuthEnabled           bool      `gorm:"column:email_auth_enabled;not null;default:true" json:"email_auth_enabled"`
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
		ID: 1, EmailAuthEnabled: true, EmailVerificationEnabled: true,
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

func (r *AuthRepository) CreateEmailUser(ctx context.Context, user *User) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := assignInitialUserRole(tx, user); err != nil {
			return err
		}
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

		if err := assignInitialUserRole(tx, user); err != nil {
			return err
		}
		if err := tx.Create(user).Error; err != nil {
			return err
		}
		return grantInitialRewards(ctx, tx, user.ID)
	})
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
			"email_auth_enabled", "email_registration_enabled", "email_verification_enabled", "invitation_required",
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

// UpsertExternalUser 按 provider subject 查找用户，不通过邮箱自动合并账号。
func (r *AuthRepository) UpsertExternalUser(ctx context.Context, in ExternalIdentityInput) (*User, error) {
	var user User
	now := time.Now()
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var ident AuthIdentity
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("provider = ? AND provider_user_id = ?", in.Provider, in.ProviderUserID).
			First(&ident).Error
		if err == nil {
			if err := tx.First(&user, "id = ?", ident.UserID).Error; err != nil {
				return err
			}
			updates := map[string]any{
				"display_name":  in.DisplayName,
				"avatar_url":    in.AvatarURL,
				"last_login_at": now,
				"updated_at":    now,
			}
			if user.Email == nil && in.Email != nil && !r.emailExists(tx, *in.Email) {
				updates["email"] = *in.Email
			}
			if err := tx.Model(&user).Updates(updates).Error; err != nil {
				return err
			}
			if err := tx.Model(&ident).Updates(map[string]any{
				"username": in.Username, "provider_email": in.Email,
				"trust_level": in.TrustLevel, "profile": in.Profile, "updated_at": now,
			}).Error; err != nil {
				return err
			}
			return tx.First(&user, "id = ?", ident.UserID).Error
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		email := in.Email
		if email != nil && r.emailExists(tx, *email) {
			email = nil
		}
		user = User{
			ID: uuid.New(), Email: email, DisplayName: in.DisplayName, AvatarURL: in.AvatarURL,
			Status: UserStatusActive, LastLoginAt: &now, CreatedAt: now, UpdatedAt: now,
		}
		if err := assignInitialUserRole(tx, &user); err != nil {
			return err
		}
		if err := tx.Create(&user).Error; err != nil {
			return err
		}
		if err := grantInitialRewards(ctx, tx, user.ID); err != nil {
			return err
		}
		ident = AuthIdentity{
			ID: uuid.New(), UserID: user.ID, Provider: in.Provider, ProviderUserID: in.ProviderUserID,
			Username: in.Username, ProviderEmail: in.Email, TrustLevel: in.TrustLevel,
			Profile: in.Profile, CreatedAt: now, UpdatedAt: now,
		}
		return tx.Create(&ident).Error
	})
	return &user, err
}

// assignInitialUserRole 只在创建账号时调用。首个站内账号自动成为管理员，
// 后续账号一律默认为普通用户。表锁避免两个首次登录并发创建出多个管理员。
func assignInitialUserRole(tx *gorm.DB, user *User) error {
	if err := tx.Exec("LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE").Error; err != nil {
		return err
	}
	var count int64
	if err := tx.Model(&User{}).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		user.Role = UserRoleAdmin
	} else if user.Role == "" {
		user.Role = UserRoleUser
	}
	return nil
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
