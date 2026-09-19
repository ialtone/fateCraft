package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type InquiryRepo struct {
	db           *gorm.DB
	ruleProvider InquiryRuleProvider
}

type InquiryRuleProvider interface {
	InquiryRules(context.Context, uuid.UUID) ([]InquiryRuleSource, error)
}

func NewInquiryRepo(db *gorm.DB, providers ...InquiryRuleProvider) *InquiryRepo {
	var provider InquiryRuleProvider
	if len(providers) > 0 {
		provider = providers[0]
	}
	return &InquiryRepo{db: db, ruleProvider: provider}
}

type InquirySnapshotData struct {
	Conversation    Conversation
	State           JSONMap
	Rules           []InquiryRuleSource
	Messages        []ChatMessage
	Events          []CampaignEvent
	Revision        int64
	StateRevisionID *uuid.UUID
}

type InquiryRuleSource struct {
	Title   string
	Content string
}

type InquiryVersion struct {
	BranchEpoch     int64
	Revision        int64
	StateRevisionID *uuid.UUID
}

func (r *InquiryRepo) ReadSnapshot(ctx context.Context, conversationID, playerID uuid.UUID) (*InquirySnapshotData, error) {
	var out InquirySnapshotData
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&out.Conversation, "id = ? AND player_id = ? AND status <> ?", conversationID, playerID, StatusDeleted).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		var current ConversationState
		if err := tx.First(&current, "conversation_id = ?", conversationID).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		out.State = current.State
		var revision StateRevision
		err := tx.Table("state_revisions AS r").Select("r.*").
			Joins("LEFT JOIN conversation_turns t ON t.id = r.turn_id").
			Where(`r.conversation_id = ? AND r.invalidated_at IS NULL AND
				(r.turn_id IS NULL OR (t.status = ? AND t.invalidated_at IS NULL))`, conversationID, TurnStatusCompleted).
			Order("r.created_at DESC, r.id DESC").First(&revision).Error
		if err == nil {
			out.State = revision.AfterState
			out.StateRevisionID = &revision.ID
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		} else {
			// A state revision may be committed just before its turn is sealed. In
			// that short window its before_state is the last public snapshot.
			var pending StateRevision
			if pendingErr := tx.Table("state_revisions AS r").Select("r.*").
				Joins("JOIN conversation_turns t ON t.id = r.turn_id").
				Where("r.conversation_id = ? AND r.invalidated_at IS NULL AND t.status = ? AND t.invalidated_at IS NULL", conversationID, TurnStatusPending).
				Order("r.created_at ASC").First(&pending).Error; pendingErr == nil {
				out.State = pending.BeforeState
			} else if !errors.Is(pendingErr, gorm.ErrRecordNotFound) {
				return pendingErr
			}
		}
		if err := tx.Raw(`
			SELECT m.*, t.turn_seq
			FROM chat_messages m
			JOIN conversation_turns t ON t.id = m.turn_id
			WHERE m.conversation_id = ? AND m.invalidated_at IS NULL AND m.hidden = FALSE
			  AND t.status = 'completed' AND t.invalidated_at IS NULL
			ORDER BY m.message_seq DESC
			LIMIT 16`, conversationID).Scan(&out.Messages).Error; err != nil {
			return err
		}
		for left, right := 0, len(out.Messages)-1; left < right; left, right = left+1, right-1 {
			out.Messages[left], out.Messages[right] = out.Messages[right], out.Messages[left]
		}
		if err := tx.Model(&ConversationTurn{}).
			Where("conversation_id = ? AND status = ? AND invalidated_at IS NULL", conversationID, TurnStatusCompleted).
			Select("COALESCE(MAX(turn_seq), 0)").Scan(&out.Revision).Error; err != nil {
			return err
		}
		if out.Conversation.CampaignID != nil {
			if err := tx.Where("campaign_id = ? AND visibility = ?", *out.Conversation.CampaignID, CampaignVisibilityPlayer).
				Order("sequence DESC").Limit(16).Find(&out.Events).Error; err != nil {
				return err
			}
			for left, right := 0, len(out.Events)-1; left < right; left, right = left+1, right-1 {
				out.Events[left], out.Events[right] = out.Events[right], out.Events[left]
			}
			if err := tx.Model(&Campaign{}).Where("id = ?", *out.Conversation.CampaignID).
				Select("revision").Scan(&out.Revision).Error; err != nil {
				return err
			}
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return &out, err
	}
	if r.ruleProvider != nil {
		out.Rules, err = r.ruleProvider.InquiryRules(ctx, out.Conversation.ChallengeVersionID)
	} else {
		out.Rules, err = readInquiryRules(r.db.WithContext(ctx), out.Conversation.ChallengeVersionID)
	}
	return &out, err
}

func readInquiryRules(tx *gorm.DB, versionID uuid.UUID) ([]InquiryRuleSource, error) {
	var version ChallengeVersion
	if err := tx.Select("manifest").First(&version, "id = ?", versionID).Error; err != nil {
		return nil, err
	}
	var manifest struct {
		DefaultConfig struct {
			RequiredWorldBooks []string `json:"required_worldbooks"`
		} `json:"default_config"`
	}
	if err := json.Unmarshal(version.Manifest, &manifest); err != nil {
		return nil, err
	}
	if len(manifest.DefaultConfig.RequiredWorldBooks) == 0 {
		return nil, nil
	}
	var assets []ChallengeAsset
	if err := tx.Where(
		"challenge_version_id = ? AND kind = ? AND slug IN ?",
		versionID, ChallengeAssetWorldBook, manifest.DefaultConfig.RequiredWorldBooks,
	).Find(&assets).Error; err != nil {
		return nil, err
	}
	booksBySlug := make(map[string]WorldBook, len(assets))
	for _, asset := range assets {
		var book WorldBook
		if err := json.Unmarshal(asset.Content, &book); err != nil {
			return nil, err
		}
		booksBySlug[asset.Slug] = book
	}
	books := make([]WorldBook, 0, len(manifest.DefaultConfig.RequiredWorldBooks))
	for _, slug := range manifest.DefaultConfig.RequiredWorldBooks {
		if book, ok := booksBySlug[slug]; ok {
			books = append(books, book)
		}
	}
	return VisibleInquiryRules(books), nil
}

// VisibleInquiryRules derives the explicitly player-visible inquiry sources
// from immutable worldbooks. Cached and uncached paths share this function.
func VisibleInquiryRules(books []WorldBook) []InquiryRuleSource {
	type candidate struct {
		bookOrder int
		entry     WorldBookEntry
	}
	candidates := make([]candidate, 0)
	for bookOrder, book := range books {
		for _, entry := range book.Entries {
			visible, _ := entry.Metadata["inquiry_visible"].(bool)
			if entry.Enabled && visible {
				candidates = append(candidates, candidate{bookOrder: bookOrder, entry: entry})
			}
		}
	}
	sort.SliceStable(candidates, func(left, right int) bool {
		if candidates[left].bookOrder != candidates[right].bookOrder {
			return candidates[left].bookOrder < candidates[right].bookOrder
		}
		if candidates[left].entry.Priority != candidates[right].entry.Priority {
			return candidates[left].entry.Priority < candidates[right].entry.Priority
		}
		return candidates[left].entry.Title < candidates[right].entry.Title
	})
	out := make([]InquiryRuleSource, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, InquiryRuleSource{Title: candidate.entry.Title, Content: candidate.entry.Content})
	}
	return out
}

func (r *InquiryRepo) CurrentVersion(ctx context.Context, conversationID uuid.UUID) (InquiryVersion, error) {
	var conv Conversation
	if err := r.db.WithContext(ctx).Select("id", "campaign_id", "branch_epoch").First(&conv, "id = ?", conversationID).Error; err != nil {
		return InquiryVersion{}, err
	}
	version := InquiryVersion{BranchEpoch: conv.BranchEpoch}
	if conv.CampaignID != nil {
		if err := r.db.WithContext(ctx).Model(&Campaign{}).Where("id = ?", *conv.CampaignID).Select("revision").Scan(&version.Revision).Error; err != nil {
			return version, err
		}
	} else {
		if err := r.db.WithContext(ctx).Model(&ConversationTurn{}).
			Where("conversation_id = ? AND status = ? AND invalidated_at IS NULL", conversationID, TurnStatusCompleted).
			Select("COALESCE(MAX(turn_seq), 0)").Scan(&version.Revision).Error; err != nil {
			return version, err
		}
	}
	var revision StateRevision
	err := r.db.WithContext(ctx).Table("state_revisions AS r").Select("r.id").
		Joins("LEFT JOIN conversation_turns t ON t.id = r.turn_id").
		Where(`r.conversation_id = ? AND r.invalidated_at IS NULL AND
			(r.turn_id IS NULL OR (t.status = ? AND t.invalidated_at IS NULL))`, conversationID, TurnStatusCompleted).
		Order("r.created_at DESC, r.id DESC").First(&revision).Error
	if err == nil {
		version.StateRevisionID = &revision.ID
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return version, err
	}
	return version, nil
}

func (r *InquiryRepo) Thread(ctx context.Context, conversationID, playerID uuid.UUID) (*CharacterInquiryThread, error) {
	now := time.Now()
	candidate := CharacterInquiryThread{ID: uuid.New(), ConversationID: conversationID, PlayerID: playerID, CreatedAt: now, UpdatedAt: now}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "conversation_id"}, {Name: "player_id"}}, DoNothing: true,
	}).Create(&candidate).Error; err != nil {
		return nil, err
	}
	// Do not reuse candidate here. When the insert conflicts, its generated ID
	// is not the stored row ID, and GORM would add that non-zero primary key to
	// the explicit conversation/player predicates.
	var thread CharacterInquiryThread
	if err := r.db.WithContext(ctx).First(&thread, "conversation_id = ? AND player_id = ?", conversationID, playerID).Error; err != nil {
		return nil, err
	}
	return &thread, nil
}

func (r *InquiryRepo) Append(ctx context.Context, message *CharacterInquiryMessage) error {
	if message.ID == uuid.Nil {
		message.ID = uuid.New()
	}
	if message.CreatedAt.IsZero() {
		message.CreatedAt = time.Now()
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(message).Error; err != nil {
			return err
		}
		return tx.Model(&CharacterInquiryThread{}).
			Where("id = ? AND conversation_id = ? AND player_id = ?", message.ThreadID, message.ConversationID, message.PlayerID).
			Update("updated_at", message.CreatedAt).Error
	})
}

func (r *InquiryRepo) List(ctx context.Context, threadID, conversationID, playerID uuid.UUID, limit int) ([]CharacterInquiryMessage, error) {
	if limit <= 0 || limit > 100 {
		limit = 40
	}
	var rows []CharacterInquiryMessage
	err := r.db.WithContext(ctx).Where("thread_id = ? AND conversation_id = ? AND player_id = ?", threadID, conversationID, playerID).
		Order("created_at DESC, id DESC").Limit(limit).Find(&rows).Error
	for left, right := 0, len(rows)-1; left < right; left, right = left+1, right-1 {
		rows[left], rows[right] = rows[right], rows[left]
	}
	return rows, err
}

func (r *InquiryRepo) Clear(ctx context.Context, threadID, conversationID, playerID uuid.UUID) error {
	return r.db.WithContext(ctx).Where("thread_id = ? AND conversation_id = ? AND player_id = ?", threadID, conversationID, playerID).
		Delete(&CharacterInquiryMessage{}).Error
}

func (r *InquiryRepo) ConsumeRateLimit(ctx context.Context, playerID uuid.UUID, now time.Time, window time.Duration, maximum int) (bool, error) {
	allowed := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row := CharacterInquiryRateLimit{PlayerID: playerID, WindowStartedAt: now, RequestCount: 0, UpdatedAt: now}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "player_id"}}, DoNothing: true}).Create(&row).Error; err != nil {
			return err
		}
		cutoff := now.Add(-window)
		result := tx.Model(&CharacterInquiryRateLimit{}).
			Where("player_id = ? AND (window_started_at <= ? OR request_count < ?)", playerID, cutoff, maximum).
			Updates(map[string]any{
				"window_started_at": gorm.Expr("CASE WHEN window_started_at <= ? THEN ? ELSE window_started_at END", cutoff, now),
				"request_count":     gorm.Expr("CASE WHEN window_started_at <= ? THEN 1 ELSE request_count + 1 END", cutoff),
				"updated_at":        now,
			})
		if result.Error != nil {
			return result.Error
		}
		allowed = result.RowsAffected == 1
		return nil
	})
	return allowed, err
}
