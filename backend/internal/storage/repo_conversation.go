package storage

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ErrNotFound 表示记录不存在。
var ErrNotFound = errors.New("记录不存在")

// ConversationRepo 负责对话及其世界书关联的持久化。
type ConversationRepo struct{ db *gorm.DB }

func NewConversationRepo(db *gorm.DB) *ConversationRepo { return &ConversationRepo{db: db} }

func (r *ConversationRepo) Create(ctx context.Context, c *Conversation) error {
	return r.db.WithContext(ctx).Create(c).Error
}

// Get 按 id 读取对话（包含已软删除的，便于恢复场景）。
func (r *ConversationRepo) Get(ctx context.Context, id uuid.UUID) (*Conversation, error) {
	var c Conversation
	err := r.db.WithContext(ctx).First(&c, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &c, err
}

// List 按玩家、状态过滤并支持标题/内容搜索，按 updated_at 倒序。
func (r *ConversationRepo) List(ctx context.Context, playerID string, statuses []string, search string) ([]Conversation, error) {
	q := r.db.WithContext(ctx).Where("player_id = ?", playerID)
	if len(statuses) > 0 {
		q = q.Where("status IN ?", statuses)
	} else {
		// 默认隐藏已删除对话。
		q = q.Where("status <> ?", StatusDeleted)
	}
	if search != "" {
		like := "%" + search + "%"
		q = q.Where("title ILIKE ?", like)
	}
	var out []Conversation
	err := q.Order("updated_at DESC").Find(&out).Error
	return out, err
}

// Save 全量保存对话。
func (r *ConversationRepo) Save(ctx context.Context, c *Conversation) error {
	return r.db.WithContext(ctx).Save(c).Error
}

// UpdateFields 局部更新字段。
func (r *ConversationRepo) UpdateFields(ctx context.Context, id uuid.UUID, fields map[string]any) error {
	return r.db.WithContext(ctx).Model(&Conversation{}).Where("id = ?", id).Updates(fields).Error
}

// TryLock 以原子方式将 is_processing 由 false 置为 true，返回是否抢到生成锁。
func (r *ConversationRepo) TryLock(ctx context.Context, id uuid.UUID) (bool, error) {
	res := r.db.WithContext(ctx).Model(&Conversation{}).
		Where("id = ? AND is_processing = ?", id, false).
		Update("is_processing", true)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// Unlock 释放生成锁。
func (r *ConversationRepo) Unlock(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Model(&Conversation{}).
		Where("id = ?", id).Update("is_processing", false).Error
}

// ClearProcessingLocks 清除所有残留的生成锁（进程崩溃/被 kill 后 is_processing
// 可能残留为 true，导致对话永远提示"正在生成中"）。应在服务启动时调用一次。
func (r *ConversationRepo) ClearProcessingLocks(ctx context.Context) (int64, error) {
	res := r.db.WithContext(ctx).Model(&Conversation{}).
		Where("is_processing = ?", true).Update("is_processing", false)
	return res.RowsAffected, res.Error
}

// IncrementRewindsUsed 将对话本局已用回溯次数 +1（天道回溯成功时调用）。
func (r *ConversationRepo) IncrementRewindsUsed(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Model(&Conversation{}).
		Where("id = ?", id).
		Update("rewinds_used_this_trial", gorm.Expr("rewinds_used_this_trial + 1")).Error
}

// SetWorldBooks 覆盖对话选择的世界书模板集合（id 为模板文件 slug）。
func (r *ConversationRepo) SetWorldBooks(ctx context.Context, convID uuid.UUID, bookIDs []string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("conversation_id = ?", convID).Delete(&ConversationWorldBook{}).Error; err != nil {
			return err
		}
		for _, bid := range bookIDs {
			if err := tx.Create(&ConversationWorldBook{ConversationID: convID, WorldBookID: bid}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *ConversationRepo) AddWorldBook(ctx context.Context, convID uuid.UUID, bookID string) error {
	link := ConversationWorldBook{ConversationID: convID, WorldBookID: bookID}
	return r.db.WithContext(ctx).
		Where("conversation_id = ? AND world_book_id = ?", convID, bookID).
		FirstOrCreate(&link).Error
}

func (r *ConversationRepo) RemoveWorldBook(ctx context.Context, convID uuid.UUID, bookID string) error {
	return r.db.WithContext(ctx).
		Where("conversation_id = ? AND world_book_id = ?", convID, bookID).
		Delete(&ConversationWorldBook{}).Error
}

// WorldBookIDs 返回对话绑定的世界书 id 列表。
func (r *ConversationRepo) WorldBookIDs(ctx context.Context, convID uuid.UUID) ([]string, error) {
	var ids []string
	err := r.db.WithContext(ctx).Model(&ConversationWorldBook{}).
		Where("conversation_id = ?", convID).
		Pluck("world_book_id", &ids).Error
	return ids, err
}
