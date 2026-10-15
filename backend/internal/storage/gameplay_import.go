package storage

import (
	"context"
	"github.com/google/uuid"
	"gorm.io/gorm/clause"
	"time"
)

type GameplayImport struct {
	ID              uuid.UUID  `gorm:"primaryKey;type:uuid" json:"id"`
	ActorID         uuid.UUID  `gorm:"type:uuid" json:"actor_id"`
	Source          string     `json:"source"`
	FileName        string     `json:"file_name"`
	FileHash        string     `json:"file_hash"`
	ContentHash     string     `json:"content_hash"`
	TargetSlug      string     `json:"target_slug"`
	Strategy        string     `json:"strategy"`
	PackRevision    int64      `json:"pack_revision"`
	DraftRevision   int64      `json:"draft_revision"`
	Status          string     `json:"status"`
	Payload         JSONDoc    `gorm:"type:jsonb" json:"-"`
	Preview         JSONMap    `gorm:"type:jsonb" json:"preview"`
	RequestID       *uuid.UUID `gorm:"type:uuid" json:"request_id,omitempty"`
	ResultVersionID *uuid.UUID `gorm:"type:uuid" json:"result_version_id,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	ExpiresAt       time.Time  `json:"expires_at"`
}

func (GameplayImport) TableName() string { return "gameplay_imports" }
func (r *ChallengeRepository) CleanupImports(ctx context.Context) error {
	return r.db.WithContext(ctx).Model(&GameplayImport{}).Where("status='preview' AND expires_at < ?", time.Now()).Updates(map[string]any{"status": "expired", "payload": nil}).Error
}
func (r *ChallengeRepository) CreateImport(ctx context.Context, row *GameplayImport) error {
	var count int64
	if err := r.db.WithContext(ctx).Model(&GameplayImport{}).Where("status='preview'").Count(&count).Error; err != nil {
		return err
	}
	if count >= 100 {
		return ErrRevisionConflict
	}
	return r.db.WithContext(ctx).Create(row).Error
}
func (r *ChallengeRepository) ImportRecord(ctx context.Context, id, actor uuid.UUID, lock bool) (*GameplayImport, error) {
	var row GameplayImport
	q := r.db.WithContext(ctx)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := q.First(&row, "id=? AND actor_id=?", id, actor).Error
	return &row, asNotFound(err)
}
func (r *ChallengeRepository) SaveImport(ctx context.Context, row *GameplayImport) error {
	return r.db.WithContext(ctx).Save(row).Error
}
func (r *ChallengeRepository) ImportRequest(ctx context.Context, id, actor uuid.UUID) (*GameplayImport, error) {
	var row GameplayImport
	err := r.db.WithContext(ctx).First(&row, "request_id=? AND actor_id=?", id, actor).Error
	return &row, asNotFound(err)
}

type ImportCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

func (r *ChallengeRepository) ListImports(ctx context.Context, actor uuid.UUID, before *ImportCursor) ([]GameplayImport, error) {
	var rows []GameplayImport
	q := r.db.WithContext(ctx).Omit("payload").Where("actor_id=?", actor)
	if before != nil {
		q = q.Where("(created_at,id) < (?,?)", before.CreatedAt, before.ID)
	}
	err := q.Order("created_at DESC,id DESC").Limit(51).Find(&rows).Error
	return rows, err
}
