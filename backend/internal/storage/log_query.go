package storage

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var ErrLogQuery = errors.New("诊断筛选或游标无效")

type LogQuery struct {
	Limit          int        `json:"-"`
	Cursor         string     `json:"-"`
	From           *time.Time `json:"from,omitempty"`
	Until          *time.Time `json:"until,omitempty"`
	ConversationID *uuid.UUID `json:"conversation_id,omitempty"`
	AttemptID      *uuid.UUID `json:"attempt_id,omitempty"`
	TraceID        *uuid.UUID `json:"trace_id,omitempty"`
	ActorID        *uuid.UUID `json:"actor_id,omitempty"`
	Purpose        string     `json:"purpose,omitempty"`
	Model          string     `json:"model,omitempty"`
	Action         string     `json:"action,omitempty"`
	Success        *bool      `json:"success,omitempty"`
}

type logCursor struct {
	Time   time.Time `json:"time"`
	ID     uuid.UUID `json:"id"`
	Filter string    `json:"filter"`
	Kind   string    `json:"kind"`
}

func (q LogQuery) fingerprint() string {
	data, _ := json.Marshal(q)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (q LogQuery) query(ctx context.Context, db *gorm.DB, kind string) (*gorm.DB, error) {
	if q.Limit < 1 || q.Limit > 200 || len(q.Cursor) > 2048 || len(q.Model) > 256 || len(q.Purpose) > 64 || len(q.Action) > 64 {
		return nil, ErrLogQuery
	}
	if q.From != nil && q.Until != nil && !q.From.Before(*q.Until) {
		return nil, ErrLogQuery
	}
	query := db.WithContext(ctx)
	if q.From != nil {
		query = query.Where("created_at >= ?", *q.From)
	}
	if q.Until != nil {
		query = query.Where("created_at < ?", *q.Until)
	}
	if q.ConversationID != nil {
		query = query.Where("conversation_id = ?", *q.ConversationID)
	}
	if q.AttemptID != nil {
		query = query.Where("attempt_id = ?", *q.AttemptID)
	}
	if q.TraceID != nil {
		query = query.Where("trace_id = ?", *q.TraceID)
	}
	if q.ActorID != nil {
		query = query.Where("actor_user_id = ?", *q.ActorID)
	}
	if q.Purpose != "" {
		query = query.Where("purpose = ?", q.Purpose)
	}
	if q.Model != "" {
		query = query.Where("model = ?", q.Model)
	}
	if q.Action != "" {
		query = query.Where("action = ?", q.Action)
	}
	if q.Success != nil {
		query = query.Where("success = ?", *q.Success)
	}
	if q.Cursor != "" {
		var cursor logCursor
		data, err := base64.RawURLEncoding.DecodeString(q.Cursor)
		if err != nil || json.Unmarshal(data, &cursor) != nil || cursor.ID == uuid.Nil || cursor.Time.IsZero() || cursor.Kind != kind || cursor.Filter != q.fingerprint() {
			return nil, ErrLogQuery
		}
		query = query.Where("(created_at, id) < (?, ?)", cursor.Time, cursor.ID)
	}
	return query.Order("created_at DESC, id DESC").Limit(q.Limit + 1), nil
}

func (q LogQuery) next(kind string, created time.Time, id uuid.UUID) string {
	data, _ := json.Marshal(logCursor{Time: created, ID: id, Kind: kind, Filter: q.fingerprint()})
	return base64.RawURLEncoding.EncodeToString(data)
}
