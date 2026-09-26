package share

import (
	"time"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/storage"
)

const (
	SnapshotVersion      = 1
	MaxMessages          = 500
	MaxSnapshotBytes     = 2 << 20
	MaxActiveShares      = 100
	DefaultExpiryDays    = 30
	AnonymousAttribution = "匿名旅人"
)

type ChallengeSnapshot struct {
	Slug    string `json:"slug"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

type MessageSnapshot struct {
	Position  int             `json:"position"`
	Type      string          `json:"type"`
	Markdown  string          `json:"markdown"`
	Metadata  storage.JSONMap `json:"metadata,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

type Snapshot struct {
	SchemaVersion int               `json:"schema_version"`
	Title         string            `json:"title"`
	Attribution   string            `json:"attribution"`
	ShowUsername  bool              `json:"show_username"`
	AvatarURL     string            `json:"avatar_url,omitempty"`
	Challenge     ChallengeSnapshot `json:"challenge"`
	// LLMRoute 为来源对话固化的请求路由；client 表示内容未经服务端校验。
	LLMRoute  string            `json:"llm_route"`
	CreatedAt time.Time         `json:"created_at"`
	Messages  []MessageSnapshot `json:"messages"`
}

type CreateInput struct {
	Scope             string     `json:"scope"`
	StartMessageID    *uuid.UUID `json:"start_message_id"`
	EndMessageID      *uuid.UUID `json:"end_message_id"`
	Title             string     `json:"title"`
	ShowUsername      bool       `json:"show_username"`
	PublicAttribution string     `json:"-"`
	PublicAvatarURL   string     `json:"-"`
	ExpiresInDays     *int       `json:"expires_in_days"`
	RequestID         uuid.UUID  `json:"request_id"`
}

type View struct {
	ID                   uuid.UUID  `json:"id"`
	ConversationID       uuid.UUID  `json:"conversation_id"`
	BranchEpoch          int64      `json:"branch_epoch"`
	Scope                string     `json:"scope"`
	SourceStartMessageID uuid.UUID  `json:"source_start_message_id"`
	SourceEndMessageID   uuid.UUID  `json:"source_end_message_id"`
	Title                string     `json:"title"`
	ShowUsername         bool       `json:"show_username"`
	PublicAttribution    string     `json:"public_attribution"`
	MessageCount         int        `json:"message_count"`
	Status               string     `json:"status"`
	ExpiresAt            *time.Time `json:"expires_at,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	RevokedAt            *time.Time `json:"revoked_at,omitempty"`
	ModeratedAt          *time.Time `json:"moderated_at,omitempty"`
	PublicURL            string     `json:"public_url"`
	Replayed             bool       `json:"replayed,omitempty"`
	ConversationTitle    string     `json:"conversation_title,omitempty"`
}

type OwnedPage struct {
	Shares     []View `json:"shares"`
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
	PageSize   int    `json:"page_size"`
}

type PublicView struct {
	ID           uuid.UUID `json:"id"`
	SnapshotHash string    `json:"snapshot_hash"`
	Snapshot     Snapshot  `json:"snapshot"`
}

type ReportInput struct {
	Reason  string `json:"reason"`
	Details string `json:"details"`
}

type AdminModerateInput struct {
	Reason string `json:"reason"`
}

type AdminResolveReportInput struct {
	Status string `json:"status"`
}
