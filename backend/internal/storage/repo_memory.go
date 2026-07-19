package storage

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// MemoryRepo 负责摘要记忆，严格按 conversation_id 隔离。
type MemoryRepo struct{ db *gorm.DB }

func NewMemoryRepo(db *gorm.DB) *MemoryRepo { return &MemoryRepo{db: db} }

func (r *MemoryRepo) Create(ctx context.Context, s *MemorySummary) error {
	return r.db.WithContext(ctx).Create(s).Error
}

// Latest 返回当前对话最新版本的摘要，不存在时返回 ErrNotFound。
func (r *MemoryRepo) Latest(ctx context.Context, convID uuid.UUID) (*MemorySummary, error) {
	var s MemorySummary
	err := r.db.WithContext(ctx).
		Where("conversation_id = ?", convID).
		Order("version DESC").First(&s).Error
	if err == gorm.ErrRecordNotFound {
		return nil, ErrNotFound
	}
	return &s, err
}

// MaxVersion 返回当前对话已有的最大摘要版本号，无摘要时为 0。
func (r *MemoryRepo) MaxVersion(ctx context.Context, convID uuid.UUID) (int, error) {
	var result struct{ Max *int }
	err := r.db.WithContext(ctx).Model(&MemorySummary{}).
		Where("conversation_id = ?", convID).
		Select("MAX(version) AS max").Scan(&result).Error
	if err != nil || result.Max == nil {
		return 0, err
	}
	return *result.Max, nil
}

// List 按版本倒序返回当前对话的所有摘要版本。
func (r *MemoryRepo) List(ctx context.Context, convID uuid.UUID) ([]MemorySummary, error) {
	var out []MemorySummary
	err := r.db.WithContext(ctx).
		Where("conversation_id = ?", convID).
		Order("version DESC").Find(&out).Error
	return out, err
}

func (r *MemoryRepo) Get(ctx context.Context, convID, id uuid.UUID) (*MemorySummary, error) {
	var s MemorySummary
	err := r.db.WithContext(ctx).First(&s, "id = ? AND conversation_id = ?", id, convID).Error
	if err == gorm.ErrRecordNotFound {
		return nil, ErrNotFound
	}
	return &s, err
}

// UpdateContent 编辑某个摘要版本的正文。
func (r *MemoryRepo) UpdateContent(ctx context.Context, convID, id uuid.UUID, content string, tokenCount int) error {
	return r.db.WithContext(ctx).Model(&MemorySummary{}).
		Where("id = ? AND conversation_id = ?", id, convID).
		Updates(map[string]any{"content": content, "token_count": tokenCount}).Error
}
