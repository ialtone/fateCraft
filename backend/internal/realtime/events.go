package realtime

// WebSocket 消息类型（doc 10）。
const (
	// 客户端 -> 服务端
	TypeUserInput        = "user_input"
	TypeCancelGeneration = "cancel_generation"

	// 服务端 -> 客户端
	TypeGenerationStarted   = "generation_started"
	TypeGenerationDelta     = "generation_delta"
	TypeGenerationFinished  = "generation_finished"
	TypeGenerationDiscarded = "generation_discarded"
	TypeStateUpdate         = "state_update"
	TypeMessageCreated      = "message_created"
	TypeMessageUpdated      = "message_updated"
	TypeContextBuilt        = "context_built"
	TypeConversationEnded   = "conversation_ended"
	TypeEndingTriggered     = "ending_triggered"
	TypeError               = "error"
)

// EmitFunc 是把生成事件推送给客户端的回调。
type EmitFunc func(Event)

// ClientMessage 是客户端发来的消息。
type ClientMessage struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

// Event 是服务端推送给客户端的事件。字段按需填充。
type Event struct {
	Type           string `json:"type"`
	ConversationID string `json:"conversation_id,omitempty"`
	MessageID      string `json:"message_id,omitempty"`
	Delta          string `json:"delta,omitempty"`
	ContextBuildID string `json:"context_build_id,omitempty"`
	Message        any    `json:"message,omitempty"`
	State          any    `json:"state,omitempty"`
	Error          string `json:"error,omitempty"`
	// 结局相关字段
	EndingType    string `json:"ending_type,omitempty"`
	EndingTitle   string `json:"ending_title,omitempty"`
	EndingSummary string `json:"ending_summary,omitempty"`
	SpiritStones  int64  `json:"spirit_stones,omitempty"`  // 本次带出的灵石数量
	SurvivalTurns int    `json:"survival_turns,omitempty"` // 本次存活回合数
}
