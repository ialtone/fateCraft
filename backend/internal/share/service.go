package share

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/storage"
)

var (
	ErrInvalidInput = errors.New("分享参数无效")
	ErrTooLarge     = errors.New("分享内容超过大小限制")
	ErrRateLimited  = errors.New("请求过于频繁，请稍后再试")
)

type Service struct {
	repo      *storage.ShareRepository
	signer    *signer
	signerErr error
	limiter   *rateLimiter
}

func New(repo *storage.ShareRepository, signingKey string) *Service {
	signer, err := newSigner(signingKey)
	return &Service{repo: repo, signer: signer, signerErr: err, limiter: newRateLimiter()}
}

func (s *Service) Create(ctx context.Context, playerID, displayName, avatarURL string, conversationID uuid.UUID, input CreateInput) (*View, error) {
	return s.create(ctx, playerID, displayName, avatarURL, conversationID, input, MaxActiveShares)
}

func (s *Service) create(ctx context.Context, playerID, displayName, avatarURL string, conversationID uuid.UUID, input CreateInput, activeLimit int) (*View, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	ownerID, err := uuid.Parse(playerID)
	if err != nil || input.RequestID == uuid.Nil {
		return nil, ErrInvalidInput
	}
	if !s.limiter.allow(s.signer.fingerprint("create|"+playerID), 30, time.Minute) {
		return nil, ErrRateLimited
	}
	normalized, expiresAt, err := normalizeCreateInput(input, displayName, avatarURL)
	if err != nil {
		return nil, err
	}
	requestHash := hashRequest(conversationID, normalized)
	row, replayed, err := s.repo.CreateFromConversation(ctx, conversationID, ownerID, normalized.RequestID, requestHash,
		normalized.Scope, normalized.StartMessageID, normalized.EndMessageID, activeLimit,
		s.buildShare(normalized, expiresAt))
	if err != nil {
		return nil, err
	}
	view := s.view(row)
	view.Replayed = replayed
	return &view, nil
}

func (s *Service) Replace(ctx context.Context, playerID, displayName, avatarURL string, oldID uuid.UUID, input CreateInput) (*View, error) {
	ownerID, err := uuid.Parse(playerID)
	if err != nil || input.RequestID == uuid.Nil {
		return nil, ErrInvalidInput
	}
	if err := s.available(); err != nil {
		return nil, err
	}
	if !s.limiter.allow(s.signer.fingerprint("replace|"+playerID), 30, time.Minute) {
		return nil, ErrRateLimited
	}
	old, err := s.repo.GetOwned(ctx, oldID, ownerID)
	if err != nil {
		return nil, err
	}
	normalized, expiresAt, err := normalizeCreateInput(input, displayName, avatarURL)
	if err != nil {
		return nil, err
	}
	requestHash := hashReplaceRequest(old.ID, old.ConversationID, normalized)
	row, replayed, err := s.repo.ReplaceFromConversation(ctx, old.ID, old.ConversationID, ownerID,
		normalized.RequestID, requestHash, normalized.Scope, normalized.StartMessageID, normalized.EndMessageID,
		MaxActiveShares, s.buildShare(normalized, expiresAt))
	if err != nil {
		return nil, err
	}
	view := s.view(row)
	view.Replayed = replayed
	return &view, nil
}

func (s *Service) buildShare(input CreateInput, expiresAt *time.Time) storage.ShareBuildFunc {
	return func(source storage.ShareSource) (*storage.ConversationShare, error) {
		messages, err := projectMessages(source.Messages)
		if err != nil {
			return nil, err
		}
		title := input.Title
		if title == "" {
			title = boundedString(source.Conversation.Title, 120)
		}
		snapshot := Snapshot{
			SchemaVersion: SnapshotVersion, Title: title, Attribution: input.PublicAttribution,
			ShowUsername: input.ShowUsername, AvatarURL: input.PublicAvatarURL,
			Challenge: ChallengeSnapshot{Slug: source.Challenge.Slug, Name: source.Challenge.Name, Version: source.Challenge.Version},
			CreatedAt: time.Now().UTC(), Messages: messages,
		}
		encoded, err := json.Marshal(snapshot)
		if err != nil {
			return nil, err
		}
		if len(encoded) > MaxSnapshotBytes {
			return nil, ErrTooLarge
		}
		hash := sha256.Sum256(encoded)
		return &storage.ConversationShare{
			SnapshotVersion: SnapshotVersion, Snapshot: storage.JSONDoc(encoded), SnapshotHash: hex.EncodeToString(hash[:]),
			Title: title, PublicAttribution: input.PublicAttribution, ExpiresAt: expiresAt,
		}, nil
	}
}

type ownedCursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        uuid.UUID `json:"id"`
}

func (s *Service) ListOwned(ctx context.Context, playerID, status, search, cursor string, pageSize int) (*OwnedPage, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	ownerID, err := uuid.Parse(playerID)
	if err != nil {
		return nil, ErrInvalidInput
	}
	status = strings.TrimSpace(status)
	if status != "" && status != "active" && status != "expired" && status != "revoked" && status != "moderated" {
		return nil, ErrInvalidInput
	}
	search = strings.TrimSpace(search)
	if len([]rune(search)) > 120 || pageSize < 1 || pageSize > 100 {
		return nil, ErrInvalidInput
	}
	var beforeCreatedAt *time.Time
	var beforeID *uuid.UUID
	if cursor != "" {
		decoded, err := decodeOwnedCursor(cursor)
		if err != nil {
			return nil, ErrInvalidInput
		}
		beforeCreatedAt, beforeID = &decoded.CreatedAt, &decoded.ID
	}
	rows, err := s.repo.ListOwnedPage(ctx, ownerID, storage.OwnedShareListOptions{
		Status: status, Search: search, BeforeCreatedAt: beforeCreatedAt, BeforeID: beforeID, Limit: pageSize + 1,
	})
	if err != nil {
		return nil, err
	}
	hasMore := len(rows) > pageSize
	if hasMore {
		rows = rows[:pageSize]
	}
	out := make([]View, 0, len(rows))
	for i := range rows {
		out = append(out, s.view(&rows[i]))
	}
	nextCursor := ""
	if hasMore && len(rows) > 0 {
		nextCursor = encodeOwnedCursor(ownedCursor{CreatedAt: rows[len(rows)-1].CreatedAt, ID: rows[len(rows)-1].ID})
	}
	return &OwnedPage{Shares: out, NextCursor: nextCursor, HasMore: hasMore, PageSize: pageSize}, nil
}

func encodeOwnedCursor(cursor ownedCursor) string {
	encoded, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func decodeOwnedCursor(value string) (ownedCursor, error) {
	var cursor ownedCursor
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || json.Unmarshal(decoded, &cursor) != nil || cursor.ID == uuid.Nil || cursor.CreatedAt.IsZero() {
		return ownedCursor{}, ErrInvalidInput
	}
	return cursor, nil
}

func (s *Service) Revoke(ctx context.Context, playerID string, id uuid.UUID) error {
	if err := s.available(); err != nil {
		return err
	}
	ownerID, err := uuid.Parse(playerID)
	if err != nil {
		return ErrInvalidInput
	}
	return s.repo.Revoke(ctx, id, ownerID)
}

func (s *Service) RevokeConversation(ctx context.Context, playerID string, conversationID uuid.UUID) error {
	if err := s.available(); err != nil {
		return err
	}
	ownerID, err := uuid.Parse(playerID)
	if err != nil {
		return ErrInvalidInput
	}
	return s.repo.RevokeConversation(ctx, conversationID, ownerID)
}

func (s *Service) Public(ctx context.Context, token string) (*PublicView, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	id, err := s.signer.parse(token)
	if err != nil {
		return nil, storage.ErrNotFound
	}
	row, err := s.repo.GetPublic(ctx, id)
	if err != nil {
		return nil, err
	}
	var snapshot Snapshot
	if err := json.Unmarshal(row.Snapshot, &snapshot); err != nil || snapshot.SchemaVersion != SnapshotVersion {
		return nil, storage.ErrNotFound
	}
	return &PublicView{ID: row.ID, SnapshotHash: row.SnapshotHash, Snapshot: snapshot}, nil
}

func (s *Service) AllowPublicRead(token, visitor string) bool {
	if s == nil || s.signer == nil || s.limiter == nil {
		return true
	}
	key := s.signer.fingerprint("read|" + token + "|" + visitor)
	return s.limiter.allow(key, 120, time.Minute)
}

func (s *Service) Report(ctx context.Context, token, reporter string, input ReportInput) error {
	if err := s.available(); err != nil {
		return err
	}
	if !s.limiter.allow(s.signer.fingerprint("report|"+token+"|"+reporter), 5, time.Hour) {
		return ErrRateLimited
	}
	id, err := s.signer.parse(token)
	if err != nil {
		return storage.ErrNotFound
	}
	if _, err := s.repo.GetPublic(ctx, id); err != nil {
		return err
	}
	reason := strings.TrimSpace(input.Reason)
	if reason != "privacy" && reason != "abuse" && reason != "copyright" && reason != "other" {
		return ErrInvalidInput
	}
	details := boundedString(input.Details, 500)
	return s.repo.CreateReport(ctx, &storage.ConversationShareReport{
		ID: uuid.New(), ShareID: id, ReporterFingerprint: hexFingerprint(s.signer.fingerprint(reporter)),
		Reason: reason, Details: details, Status: "open", CreatedAt: time.Now().UTC(),
	})
}

func (s *Service) ListAdmin(ctx context.Context, status string, limit int) ([]View, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	rows, err := s.repo.ListAdmin(ctx, status, limit)
	if err != nil {
		return nil, err
	}
	out := make([]View, 0, len(rows))
	for i := range rows {
		out = append(out, s.view(&rows[i]))
	}
	return out, nil
}

func (s *Service) Moderate(ctx context.Context, id, adminID uuid.UUID, reason string) error {
	if err := s.available(); err != nil {
		return err
	}
	reason = boundedString(reason, 64)
	if reason == "" {
		return ErrInvalidInput
	}
	return s.repo.Moderate(ctx, id, adminID, reason)
}

func (s *Service) ListReports(ctx context.Context, status string, limit int) ([]storage.ConversationShareReport, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	return s.repo.ListReports(ctx, status, limit)
}

func (s *Service) ResolveReport(ctx context.Context, id, adminID uuid.UUID, status string) error {
	if err := s.available(); err != nil {
		return err
	}
	if status != "resolved" && status != "dismissed" {
		return ErrInvalidInput
	}
	return s.repo.ResolveReport(ctx, id, adminID, status)
}

func (s *Service) view(row *storage.ConversationShare) View {
	status := row.Status
	if status == "active" && row.ExpiresAt != nil && !row.ExpiresAt.After(time.Now().UTC()) {
		status = "expired"
	}
	return View{
		ID: row.ID, ConversationID: row.ConversationID, BranchEpoch: row.BranchEpoch, Scope: row.Scope,
		SourceStartMessageID: row.SourceStartMessageID, SourceEndMessageID: row.SourceEndMessageID,
		Title: row.Title, ShowUsername: row.PublicAttribution != AnonymousAttribution,
		PublicAttribution: row.PublicAttribution, MessageCount: row.MessageCount,
		Status: status, ExpiresAt: row.ExpiresAt, CreatedAt: row.CreatedAt,
		RevokedAt: row.RevokedAt, ModeratedAt: row.ModeratedAt,
		PublicURL: "/s/" + s.signer.sign(row.ID), ConversationTitle: row.ConversationTitle,
	}
}

func (s *Service) available() error {
	if s == nil || s.repo == nil || s.signer == nil || s.signerErr != nil {
		return ErrSigningUnavailable
	}
	return nil
}

func normalizeCreateInput(input CreateInput, displayName, avatarURL string) (CreateInput, *time.Time, error) {
	input.Scope = strings.TrimSpace(input.Scope)
	input.Title = boundedString(input.Title, 120)
	input.PublicAttribution = AnonymousAttribution
	if input.ShowUsername {
		input.PublicAttribution = boundedString(displayName, 48)
		if input.PublicAttribution == "" {
			return input, nil, ErrInvalidInput
		}
		input.PublicAvatarURL = normalizeAvatarURL(avatarURL)
	}
	if input.Scope != "all" && input.Scope != "range" {
		return input, nil, ErrInvalidInput
	}
	if input.Scope == "all" && (input.StartMessageID != nil || input.EndMessageID != nil) {
		return input, nil, ErrInvalidInput
	}
	if input.Scope == "range" && (input.StartMessageID == nil || input.EndMessageID == nil) {
		return input, nil, ErrInvalidInput
	}
	days := DefaultExpiryDays
	if input.ExpiresInDays != nil {
		days = *input.ExpiresInDays
	}
	if days != 0 && days != 7 && days != 30 {
		return input, nil, ErrInvalidInput
	}
	if days == 0 {
		return input, nil, nil
	}
	expiresAt := time.Now().UTC().Add(time.Duration(days) * 24 * time.Hour)
	return input, &expiresAt, nil
}

func normalizeAvatarURL(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	if parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "cdn.ldstatic.com" && host != "linux.do" && !strings.HasSuffix(host, ".linux.do") {
		return ""
	}
	return value
}

func hashRequest(conversationID uuid.UUID, input CreateInput) string {
	payload, _ := json.Marshal(struct {
		ConversationID uuid.UUID   `json:"conversation_id"`
		Input          CreateInput `json:"input"`
	}{conversationID, input})
	hash := sha256.Sum256(payload)
	return hex.EncodeToString(hash[:])
}

func hashReplaceRequest(oldID, conversationID uuid.UUID, input CreateInput) string {
	payload, _ := json.Marshal(struct {
		Operation      string      `json:"operation"`
		OldID          uuid.UUID   `json:"old_id"`
		ConversationID uuid.UUID   `json:"conversation_id"`
		Input          CreateInput `json:"input"`
	}{"replace", oldID, conversationID, input})
	hash := sha256.Sum256(payload)
	return hex.EncodeToString(hash[:])
}

func hexFingerprint(encoded string) string {
	hash := sha256.Sum256([]byte(encoded))
	return hex.EncodeToString(hash[:])
}

func (s Snapshot) String() string {
	return fmt.Sprintf("share snapshot v%d (%d messages)", s.SchemaVersion, len(s.Messages))
}
