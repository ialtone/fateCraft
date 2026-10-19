package realtime

import "github.com/ialtone/fatecraft/internal/storage"

// WebSocket 消息类型（doc 10）。
const (
	// 客户端 -> 服务端
	TypeUserInput        = "user_input"
	TypeCancelGeneration = "cancel_generation"
	TypeSteerGeneration  = "steer_generation"
	TypeFollowUp         = "follow_up"
	TypeClearQueue       = "clear_generation_queue"

	// 服务端 -> 客户端
	TypeStateUpdate           = "state_update"
	TypeMessageCreated        = "message_created"
	TypeMessageUpdated        = "message_updated"
	TypeGuidanceUpdated       = "guidance.updated"
	TypeBranchReplaced        = "branch.replaced"
	TypeGenerationReplayed    = "generation.replayed"
	TypeContextBuilt          = "context_built"
	TypeConversationEnded     = "conversation_ended"
	TypeChallengeCompleted    = "challenge_completed"
	TypeEndingProposed        = "ending.proposed"
	TypeCampaignUpdated       = "campaign_updated"
	TypeModuleActionCommitted = "module.action_committed"
	TypeActivityUpdated       = "activity_updated"
	TypeSubscriptionReady     = "subscription.ready"
	// TypeError 只用于终止当前生成的故障;前端收到后复位生成状态。
	TypeError = "error"
	// TypeNotice 是非终止性提示(请求被拒绝、对话忙、已取消等),
	// 不代表正在进行的生成已经结束,前端只展示横幅、不复位流式状态。
	TypeNotice = "notice"

	TypeAgentTurnStarted    = "agent.turn.started"
	TypeAgentStepStarted    = "agent.step.started"
	TypeAgentToolStarted    = "agent.tool.started"
	TypeAgentToolFinished   = "agent.tool.finished"
	TypeAgentReasoningDelta = "agent.reasoning.delta"
	TypeAgentTextDelta      = "agent.text.delta"
	// TypeAgentTextReset retracts uncommitted text that was optimistically
	// previewed before the model revealed a tool call in the same step.
	TypeAgentTextReset     = "agent.text.reset"
	TypeAgentTurnCompleted = "agent.turn.completed"
	TypeAgentTurnPartial   = "agent.turn.partial"
	TypeAgentTurnFailed    = "agent.turn.failed"
	TypeAgentTurnCancelled = "agent.turn.cancelled"
	TypeAgentQueueUpdated  = "agent.queue.updated"
)

// EmitFunc 是把生成事件推送给客户端的回调。
type EmitFunc func(Event)

// ClientMessage 是客户端发来的消息。
type ClientMessage struct {
	Type      string `json:"type"`
	Content   string `json:"content"`
	RequestID string `json:"request_id"`
}

// Event 是服务端推送给客户端的事件。字段按需填充。
type Event struct {
	Type            string                    `json:"type"`
	EventID         string                    `json:"event_id,omitempty"`
	ConversationID  string                    `json:"conversation_id,omitempty"`
	TurnID          string                    `json:"turn_id,omitempty"`
	AttemptID       string                    `json:"attempt_id,omitempty"`
	OperationID     string                    `json:"operation_id,omitempty"`
	StepNo          int                       `json:"step_no,omitempty"`
	Sequence        int64                     `json:"sequence,omitempty"`
	OccurredAt      string                    `json:"occurred_at,omitempty"`
	Payload         any                       `json:"payload,omitempty"`
	MessageID       string                    `json:"message_id,omitempty"`
	Delta           string                    `json:"delta,omitempty"`
	ContextBuildID  string                    `json:"context_build_id,omitempty"`
	Message         any                       `json:"message,omitempty"`
	State           any                       `json:"state,omitempty"`
	Error           string                    `json:"error,omitempty"`
	Completion      *storage.CompletionView   `json:"completion,omitempty"`
	RunID           string                    `json:"run_id,omitempty"`
	Outcome         any                       `json:"outcome,omitempty"`
	RewardGrants    any                       `json:"reward_grants,omitempty"`
	Metrics         any                       `json:"metrics,omitempty"`
	NextActions     []string                  `json:"next_actions,omitempty"`
	Campaign        any                       `json:"campaign,omitempty"`
	DomainEvents    any                       `json:"domain_events,omitempty"`
	Activity        string                    `json:"activity,omitempty"`
	Status          string                    `json:"status,omitempty"`
	ActivityVersion int64                     `json:"activity_version,omitempty"`
	BranchEpoch     int64                     `json:"branch_epoch,omitempty"`
	Snapshot        *storage.ActivitySnapshot `json:"snapshot,omitempty"`
	RefreshRequired bool                      `json:"refresh_required,omitempty"`
	Reason          string                    `json:"reason,omitempty"`
}
