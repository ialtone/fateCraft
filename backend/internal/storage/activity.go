package storage

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/platformcapability"
	"gorm.io/gorm"
)

// ActivitySnapshot is the shared, public REST/WS projection. Phase is transient;
// every other field is read together under one PostgreSQL statement snapshot.
type ActivitySnapshot struct {
	ActivityVersion        int64              `json:"activity_version"`
	ConversationID         uuid.UUID          `json:"conversation_id"`
	BranchEpoch            int64              `json:"branch_epoch"`
	ConversationStatus     string             `json:"conversation_status"`
	IsProcessing           bool               `json:"is_processing"`
	TurnID                 *uuid.UUID         `json:"turn_id"`
	AttemptID              *uuid.UUID         `json:"attempt_id"`
	TurnStatus             string             `json:"turn_status,omitempty"`
	Narrative              string             `json:"narrative"`
	Memory                 string             `json:"memory"`
	MemoryJob              *ActivityMemoryJob `json:"memory_job,omitempty" gorm:"-"`
	CanContinue            bool               `json:"can_continue"`
	CanRegenerate          bool               `json:"can_regenerate"`
	RegenerateCostQuantity int64              `json:"regenerate_cost_quantity"`
	StepNo                 int                `json:"step_no,omitempty"`
	ToolCategory           string             `json:"tool_category,omitempty"`
}

type ActivityMemoryJob struct {
	ID              uuid.UUID  `json:"id"`
	Status          string     `json:"status"`
	TargetUntilTurn int64      `json:"target_until_turn"`
	ExecutionCount  int        `json:"execution_count"`
	MaxExecutions   int        `json:"max_executions"`
	CreatedAt       time.Time  `json:"created_at"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
}

type ActivityRepo struct{ db *gorm.DB }

func NewActivityRepo(db *gorm.DB) *ActivityRepo { return &ActivityRepo{db: db} }

// Snapshot takes an optional owner filter; only internal broadcasting omits it.
// EXISTS avoids counting every message and lateral joins read the current rows.
func (r *ActivityRepo) Snapshot(ctx context.Context, id uuid.UUID, owner string) (*ActivitySnapshot, error) {
	var row struct {
		ActivitySnapshot `gorm:"embedded"`
		JobJSON          string
		LLMRoute         string
	}
	result := r.db.WithContext(ctx).Raw(`
 SELECT c.id AS conversation_id, COALESCE(v.version, 0) AS activity_version,
 c.branch_epoch, c.status AS conversation_status, c.is_processing, c.llm_route,
 t.id AS turn_id, a.id AS attempt_id, COALESCE(t.status, '') AS turn_status,
 CASE WHEN c.is_processing THEN 'preparing' ELSE 'idle' END AS narrative,
 CASE j.status WHEN 'pending' THEN 'queued' WHEN 'running' THEN 'running' ELSE 'idle' END AS memory,
 CASE WHEN j.id IS NULL THEN '' ELSE jsonb_build_object(
 'id',j.id,'status',j.status,'target_until_turn',j.target_until_turn_seq,
 'execution_count',j.execution_count,'max_executions',j.max_executions,
 'created_at',j.created_at,'started_at',j.started_at,'finished_at',j.finished_at)::text END AS job_json,
 COALESCE((t.status = 'completed' AND EXISTS(SELECT 1 FROM chat_messages m
 WHERE m.conversation_id=c.id AND m.turn_id=t.id AND m.role='assistant' AND m.invalidated_at IS NULL))
 OR (t.status='partial' AND a.committed_mutating_tool_count > 0), false) AS can_continue,
 COALESCE(t.status IN ('completed','partial') OR (t.status IN ('failed','cancelled') AND EXISTS(
 SELECT 1 FROM chat_messages m WHERE m.conversation_id=c.id AND m.turn_id=t.id
 AND m.role='user' AND m.invalidated_at IS NULL)), false) AS can_regenerate
 FROM conversations c
 LEFT JOIN conversation_activity_versions v ON v.conversation_id=c.id
 LEFT JOIN LATERAL (SELECT id,status FROM conversation_turns WHERE conversation_id=c.id
 AND invalidated_at IS NULL ORDER BY turn_seq DESC LIMIT 1) t ON true
 LEFT JOIN LATERAL (SELECT id,committed_mutating_tool_count FROM generation_attempts
 WHERE turn_id=t.id ORDER BY attempt_no DESC LIMIT 1) a ON true
 LEFT JOIN LATERAL (SELECT * FROM memory_jobs WHERE conversation_id=c.id
 ORDER BY created_at DESC,id DESC LIMIT 1) j ON true
 WHERE c.id=? AND c.status <> 'deleted' AND (? = '' OR c.player_id::text = ?)`, id, owner, owner).Scan(&row)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	snapshot := row.ActivitySnapshot
	policy := platformcapability.MessageRewritePolicy()
	if policy.Enabled && row.LLMRoute != LLMRouteClient && (snapshot.TurnStatus == TurnStatusCompleted || snapshot.TurnStatus == TurnStatusPartial) {
		snapshot.RegenerateCostQuantity = policy.CostQuantity
	}
	if row.JobJSON != "" {
		if err := json.Unmarshal([]byte(row.JobJSON), &snapshot.MemoryJob); err != nil {
			return nil, err
		}
	}
	if snapshot.IsProcessing {
		phase, err := NewAgentRepo(r.db).CurrentActivity(ctx, id)
		if err != nil {
			return nil, err
		}
		// A phase read may race a new attempt; never attach it to the previous one.
		if phase != nil && snapshot.AttemptID != nil && phase.AttemptID == *snapshot.AttemptID {
			snapshot.Narrative, snapshot.StepNo = phase.Phase, phase.StepNo
			snapshot.ToolCategory = ActivityToolCategory(phase.ToolName)
		}
	}
	return &snapshot, nil
}

func ActivityToolCategory(name string) string {
	switch {
	case strings.HasPrefix(name, "fc_state_"):
		return "state"
	case name == "fc_request_roll":
		return "roll"
	case name == "fc_complete_challenge":
		return "settlement"
	case strings.HasPrefix(name, "fc_action_"):
		return "action"
	default:
		return ""
	}
}

func (r *ActivityRepo) Channel(ctx context.Context) (string, error) {
	var channel string
	err := r.db.WithContext(ctx).Raw("SELECT 'fatecraft_activity_' || substr(md5(current_schema()), 1, 16)").Scan(&channel).Error
	return channel, err
}
