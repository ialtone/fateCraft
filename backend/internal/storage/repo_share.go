package storage

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrShareBusy                = errors.New("对话正在生成，暂时不能创建分享")
	ErrShareEmpty               = errors.New("没有可分享的已提交消息")
	ErrShareRange               = errors.New("分享消息范围无效")
	ErrShareLimit               = errors.New("有效分享数量已达上限")
	ErrShareIdempotencyConflict = errors.New("分享请求 ID 已用于不同参数")
)

const maxShareMessagesQueryLimit = 501

type ShareChallengeSource struct {
	Slug    string
	Name    string
	Version string
}

type ShareSource struct {
	Conversation Conversation
	Challenge    ShareChallengeSource
	Messages     []DisplayMessage
}

type ShareBuildFunc func(ShareSource) (*ConversationShare, error)

type ShareRepository struct{ db *gorm.DB }

func NewShareRepository(db *gorm.DB) *ShareRepository { return &ShareRepository{db: db} }

func (r *ShareRepository) CreateFromConversation(
	ctx context.Context,
	conversationID, playerID, requestID uuid.UUID,
	requestHash, scope string,
	startMessageID, endMessageID *uuid.UUID,
	activeLimit int,
	build ShareBuildFunc,
) (*ConversationShare, bool, error) {
	var result *ConversationShare
	var replayed bool
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		result, replayed, err = createShareFromConversationTx(tx, conversationID, playerID, requestID, requestHash,
			scope, startMessageID, endMessageID, activeLimit, build)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	return result, replayed, err
}

// ReplaceFromConversation creates the replacement and revokes the old share in
// one transaction. A failed revoke therefore cannot leave two active links.
func (r *ShareRepository) ReplaceFromConversation(
	ctx context.Context,
	oldID, conversationID, playerID, requestID uuid.UUID,
	requestHash, scope string,
	startMessageID, endMessageID *uuid.UUID,
	activeLimit int,
	build ShareBuildFunc,
) (*ConversationShare, bool, error) {
	var result *ConversationShare
	var replayed bool
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var old ConversationShare
		if err := tx.First(&old, "id = ? AND conversation_id = ? AND player_id = ?", oldID, conversationID, playerID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if old.Status != "active" {
			var existing ConversationShare
			if err := tx.First(&existing, "player_id = ? AND request_id = ?", playerID, requestID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return ErrNotFound
				}
				return err
			}
			if existing.RequestHash != requestHash {
				return ErrShareIdempotencyConflict
			}
			result = &existing
			replayed = true
			return nil
		}

		replacementLimit := activeLimit
		if old.ExpiresAt == nil || old.ExpiresAt.After(time.Now().UTC()) {
			replacementLimit++
		}

		var err error
		result, replayed, err = createShareFromConversationTx(tx, conversationID, playerID, requestID, requestHash,
			scope, startMessageID, endMessageID, replacementLimit, build)
		if err != nil {
			return err
		}
		if replayed || result.ID == old.ID {
			return nil
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&old, "id = ? AND player_id = ?", old.ID, playerID).Error; err != nil {
			return err
		}
		if old.Status != "active" {
			return ErrNotFound
		}
		now := time.Now().UTC()
		return tx.Model(&ConversationShare{}).
			Where("id = ? AND player_id = ? AND status = 'active'", old.ID, playerID).
			Updates(map[string]any{"status": "revoked", "revoked_at": now}).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	return result, replayed, err
}

func createShareFromConversationTx(
	tx *gorm.DB,
	conversationID, playerID, requestID uuid.UUID,
	requestHash, scope string,
	startMessageID, endMessageID *uuid.UUID,
	activeLimit int,
	build ShareBuildFunc,
) (*ConversationShare, bool, error) {
	var existing ConversationShare
	err := tx.First(&existing, "player_id = ? AND request_id = ?", playerID, requestID).Error
	if err == nil {
		if existing.RequestHash != requestHash {
			return nil, false, ErrShareIdempotencyConflict
		}
		return &existing, true, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}

	var conversation Conversation
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&conversation, "id = ? AND player_id = ?", conversationID, playerID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, ErrNotFound
		}
		return nil, false, err
	}
	if conversation.Status == StatusDeleted {
		return nil, false, ErrNotFound
	}
	if conversation.IsProcessing {
		return nil, false, ErrShareBusy
	}

	if activeLimit > 0 {
		var count int64
		if err := tx.Model(&ConversationShare{}).
			Where("player_id = ? AND status = 'active' AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)", playerID).
			Count(&count).Error; err != nil {
			return nil, false, err
		}
		if count >= int64(activeLimit) {
			return nil, false, ErrShareLimit
		}
	}

	var challenge ShareChallengeSource
	if err := tx.Table("challenge_versions AS v").
		Select("p.slug, p.name, v.version").
		Joins("JOIN challenge_packs AS p ON p.id = v.challenge_pack_id").
		Where("v.id = ?", conversation.ChallengeVersionID).
		Take(&challenge).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, ErrNotFound
		}
		return nil, false, err
	}

	eligibleMessages := func() *gorm.DB {
		completedTurns := tx.Model(&ConversationTurn{}).Select("id").
			Where("conversation_id = ? AND status = ? AND invalidated_at IS NULL", conversationID, TurnStatusCompleted)
		return tx.Model(&DisplayMessage{}).
			Where("conversation_id = ? AND invalidated_at IS NULL", conversationID).
			Where("turn_id IS NULL OR turn_id IN (?)", completedTurns)
	}
	var selected []DisplayMessage
	if scope == "range" {
		if startMessageID == nil || endMessageID == nil {
			return nil, false, ErrShareRange
		}
		var start, end DisplayMessage
		if err := eligibleMessages().First(&start, "id = ?", *startMessageID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, false, ErrShareRange
			}
			return nil, false, err
		}
		if err := eligibleMessages().First(&end, "id = ?", *endMessageID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, false, ErrShareRange
			}
			return nil, false, err
		}
		if start.CreatedAt.After(end.CreatedAt) || (start.CreatedAt.Equal(end.CreatedAt) && bytes.Compare(start.ID[:], end.ID[:]) > 0) {
			return nil, false, ErrShareRange
		}
		if err := eligibleMessages().
			Where("(created_at > ? OR (created_at = ? AND id >= ?))", start.CreatedAt, start.CreatedAt, start.ID).
			Where("(created_at < ? OR (created_at = ? AND id <= ?))", end.CreatedAt, end.CreatedAt, end.ID).
			Order("created_at ASC, id ASC").Limit(maxShareMessagesQueryLimit).Find(&selected).Error; err != nil {
			return nil, false, err
		}
	} else if scope != "all" || startMessageID != nil || endMessageID != nil {
		return nil, false, ErrShareRange
	} else if err := eligibleMessages().Order("created_at ASC, id ASC").Limit(maxShareMessagesQueryLimit).Find(&selected).Error; err != nil {
		return nil, false, err
	}
	if len(selected) == 0 {
		return nil, false, ErrShareEmpty
	}

	row, err := build(ShareSource{Conversation: conversation, Challenge: challenge, Messages: selected})
	if err != nil {
		return nil, false, err
	}
	row.ID = uuid.New()
	row.ConversationID = conversationID
	row.PlayerID = playerID
	row.RequestID = requestID
	row.RequestHash = requestHash
	row.BranchEpoch = conversation.BranchEpoch
	row.Scope = scope
	row.SourceStartMessageID = selected[0].ID
	row.SourceEndMessageID = selected[len(selected)-1].ID
	row.MessageCount = len(selected)
	row.Status = "active"
	row.CreatedAt = time.Now().UTC()
	if err := tx.Create(row).Error; err != nil {
		return nil, false, err
	}
	return row, false, nil
}

type OwnedShareListOptions struct {
	Status          string
	Search          string
	BeforeCreatedAt *time.Time
	BeforeID        *uuid.UUID
	Limit           int
}

func (r *ShareRepository) ListOwnedPage(ctx context.Context, playerID uuid.UUID, options OwnedShareListOptions) ([]ConversationShare, error) {
	query := r.db.WithContext(ctx).
		Table("conversation_shares AS shares").
		Joins("LEFT JOIN conversations AS conversations ON conversations.id = shares.conversation_id").
		Where("shares.player_id = ?", playerID)

	switch options.Status {
	case "active":
		query = query.Where("shares.status = 'active' AND (shares.expires_at IS NULL OR shares.expires_at > CURRENT_TIMESTAMP)")
	case "expired":
		query = query.Where("shares.status = 'active' AND shares.expires_at <= CURRENT_TIMESTAMP")
	case "revoked", "moderated":
		query = query.Where("shares.status = ?", options.Status)
	}
	if options.Search != "" {
		term := "%" + options.Search + "%"
		query = query.Where("shares.title ILIKE ? OR conversations.title ILIKE ?", term, term)
	}
	if options.BeforeCreatedAt != nil && options.BeforeID != nil {
		query = query.Where("shares.created_at < ? OR (shares.created_at = ? AND shares.id < ?)", *options.BeforeCreatedAt, *options.BeforeCreatedAt, *options.BeforeID)
	}
	var rows []ConversationShare
	err := query.
		Select("shares.id", "shares.conversation_id", "shares.branch_epoch", "shares.scope", "shares.source_start_message_id", "shares.source_end_message_id", "shares.snapshot_version", "shares.snapshot_hash", "shares.title", "shares.public_attribution", "shares.message_count", "shares.status", "shares.expires_at", "shares.created_at", "shares.revoked_at", "shares.moderated_at", "COALESCE(conversations.title, '已删除的对话') AS conversation_title").
		Order("shares.created_at DESC, shares.id DESC").
		Limit(options.Limit).
		Scan(&rows).Error
	return rows, err
}

func (r *ShareRepository) GetOwned(ctx context.Context, id, playerID uuid.UUID) (*ConversationShare, error) {
	var row ConversationShare
	if err := r.db.WithContext(ctx).First(&row, "id = ? AND player_id = ?", id, playerID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &row, nil
}

func (r *ShareRepository) Revoke(ctx context.Context, id, playerID uuid.UUID) error {
	now := time.Now().UTC()
	result := r.db.WithContext(ctx).Model(&ConversationShare{}).
		Where("id = ? AND player_id = ? AND status = 'active'", id, playerID).
		Updates(map[string]any{"status": "revoked", "revoked_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		var count int64
		if err := r.db.WithContext(ctx).Model(&ConversationShare{}).Where("id = ? AND player_id = ?", id, playerID).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return ErrNotFound
		}
	}
	return nil
}

func (r *ShareRepository) RevokeConversation(ctx context.Context, conversationID, playerID uuid.UUID) error {
	now := time.Now().UTC()
	return r.db.WithContext(ctx).Model(&ConversationShare{}).
		Where("conversation_id = ? AND player_id = ? AND status = 'active'", conversationID, playerID).
		Updates(map[string]any{"status": "revoked", "revoked_at": now}).Error
}

func (r *ShareRepository) GetPublic(ctx context.Context, id uuid.UUID) (*ConversationShare, error) {
	var row ConversationShare
	err := r.db.WithContext(ctx).First(&row,
		"id = ? AND status = 'active' AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &row, nil
}

func (r *ShareRepository) CreateReport(ctx context.Context, report *ConversationShareReport) error {
	return r.db.WithContext(ctx).Create(report).Error
}

func (r *ShareRepository) ListAdmin(ctx context.Context, status string, limit int) ([]ConversationShare, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	query := r.db.WithContext(ctx).Select("id", "conversation_id", "branch_epoch", "scope", "source_start_message_id", "source_end_message_id", "snapshot_version", "snapshot_hash", "title", "public_attribution", "message_count", "status", "expires_at", "created_at", "revoked_at", "moderated_at")
	if status != "" {
		query = query.Where("status = ?", status)
	}
	var rows []ConversationShare
	err := query.Order("created_at DESC, id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r *ShareRepository) Moderate(ctx context.Context, id, adminID uuid.UUID, reason string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		result := tx.Model(&ConversationShare{}).Where("id = ?", id).
			Updates(map[string]any{"status": "moderated", "moderated_at": now, "moderation_reason": reason})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrNotFound
		}
		return tx.Create(&ConversationShareAuditEvent{
			ID: uuid.New(), ShareID: &id, AdminID: &adminID, EventType: "share_moderated",
			Detail: JSONMap{"reason": reason}, CreatedAt: now,
		}).Error
	})
}

func (r *ShareRepository) ListReports(ctx context.Context, status string, limit int) ([]ConversationShareReport, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	query := r.db.WithContext(ctx)
	if status != "" {
		query = query.Where("status = ?", status)
	}
	var rows []ConversationShareReport
	err := query.Order("created_at DESC, id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r *ShareRepository) ResolveReport(ctx context.Context, id, adminID uuid.UUID, status string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var report ConversationShareReport
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&report, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		now := time.Now().UTC()
		if err := tx.Model(&ConversationShareReport{}).Where("id = ?", id).
			Updates(map[string]any{"status": status, "resolved_at": now, "resolved_by": adminID}).Error; err != nil {
			return err
		}
		eventType := "report_resolved"
		if status == "dismissed" {
			eventType = "report_dismissed"
		}
		return tx.Create(&ConversationShareAuditEvent{
			ID: uuid.New(), ShareID: &report.ShareID, ReportID: &id, AdminID: &adminID,
			EventType: eventType, Detail: JSONMap{"status": status}, CreatedAt: now,
		}).Error
	})
}
