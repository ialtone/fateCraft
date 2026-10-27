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
	UserID         *uuid.UUID `json:"user_id,omitempty"`   // 按用户筛选：模型调用按发起人，上下文按对话所属玩家，审计按操作者/目标
	UserRole       string     `json:"user_role,omitempty"` // 仅审计：actor / target，留空表示两者之一
	Result         string     `json:"result,omitempty"`    // 仅审计：success / rejected / failed
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
	if q.Limit < 1 || q.Limit > 200 || len(q.Cursor) > 2048 || len(q.Model) > 256 || len(q.Purpose) > 64 || len(q.Action) > 64 || len(q.Result) > 16 || (q.UserRole != "" && q.UserRole != "actor" && q.UserRole != "target") {
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
	if q.UserID != nil {
		switch kind {
		case "llm":
			query = query.Where("user_id = ?", *q.UserID)
		case "context":
			query = query.Where("conversation_id IN (SELECT id FROM conversations WHERE player_id = ?)", *q.UserID)
		case "audit":
			switch q.UserRole {
			case "actor":
				query = query.Where("actor_user_id = ?", *q.UserID)
			case "target":
				query = query.Where("target_user_id = ?", *q.UserID)
			default:
				query = query.Where("(actor_user_id = ? OR target_user_id = ?)", *q.UserID, *q.UserID)
			}
		}
	}
	if q.Result != "" {
		query = query.Where("result = ?", q.Result)
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
