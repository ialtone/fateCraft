package auth

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/storage"
)

type CreateInvitationInput struct {
	Label     string     `json:"label"`
	MaxUses   int        `json:"max_uses"`
	ExpiresAt *time.Time `json:"expires_at"`
}

type CreatedInvitation struct {
	Invitation *storage.InvitationCode `json:"invitation"`
	Code       string                  `json:"code"`
}

type UpdateInvitationInput struct {
	Label     string     `json:"label"`
	Enabled   bool       `json:"enabled"`
	MaxUses   int        `json:"max_uses"`
	ExpiresAt *time.Time `json:"expires_at"`
}

func (s *Service) ListInvitations(ctx context.Context) ([]storage.InvitationCode, error) {
	return s.repo.ListInvitationCodes(ctx)
}

func (s *Service) CreateInvitation(ctx context.Context, in CreateInvitationInput, createdBy uuid.UUID) (*CreatedInvitation, error) {
	in.Label = strings.TrimSpace(in.Label)
	if len(in.Label) > 128 {
		return nil, fmt.Errorf("%w: 邀请码备注不能超过 128 个字符", ErrInvalidAuthSettings)
	}
	if in.MaxUses < 0 || in.MaxUses > 1000000 {
		return nil, fmt.Errorf("%w: 最大使用次数必须在 0-1000000 之间，0 表示不限次数", ErrInvalidAuthSettings)
	}
	if in.ExpiresAt != nil && !in.ExpiresAt.After(time.Now()) {
		return nil, fmt.Errorf("%w: 邀请码过期时间必须晚于当前时间", ErrInvalidAuthSettings)
	}
	code, err := generateInvitationCode()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	invitation := &storage.InvitationCode{
		ID: uuid.New(), CodeHash: sha256Hex(normalizeInvitationCode(code)), CodeHint: code[len(code)-4:],
		Label: in.Label, Enabled: true, MaxUses: in.MaxUses, ExpiresAt: in.ExpiresAt,
		CreatedBy: &createdBy, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.repo.CreateInvitationCode(ctx, invitation); err != nil {
		return nil, err
	}
	return &CreatedInvitation{Invitation: invitation, Code: code}, nil
}

func (s *Service) UpdateInvitation(ctx context.Context, id uuid.UUID, in UpdateInvitationInput) (*storage.InvitationCode, error) {
	invitation, err := s.repo.InvitationCodeByID(ctx, id)
	if err != nil {
		return nil, err
	}
	in.Label = strings.TrimSpace(in.Label)
	if len(in.Label) > 128 {
		return nil, fmt.Errorf("%w: 邀请码备注不能超过 128 个字符", ErrInvalidAuthSettings)
	}
	if in.MaxUses < 0 || in.MaxUses > 1000000 || (in.MaxUses > 0 && in.MaxUses < invitation.UsedCount) {
		return nil, fmt.Errorf("%w: 最大使用次数不能小于已使用次数", ErrInvalidAuthSettings)
	}
	invitation.Label = in.Label
	invitation.Enabled = in.Enabled
	invitation.MaxUses = in.MaxUses
	invitation.ExpiresAt = in.ExpiresAt
	invitation.UpdatedAt = time.Now()
	if err := s.repo.UpdateInvitationCode(ctx, invitation); err != nil {
		return nil, err
	}
	return invitation, nil
}

func generateInvitationCode() (string, error) {
	raw := make([]byte, 10)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
	return "FC-" + strings.Join([]string{encoded[0:4], encoded[4:8], encoded[8:12], encoded[12:16]}, "-"), nil
}
