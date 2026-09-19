package orchestration

import (
	"testing"

	"github.com/ialtone/fatecraft/internal/llm"
	"github.com/ialtone/fatecraft/internal/storage"
)

func TestResolveAgentLane(t *testing.T) {
	tests := []struct {
		name     string
		result   llm.AgentStepResult
		wantLane string
		wantErr  string
	}{
		{
			name:     "undecided with text resolves to text",
			result:   llm.AgentStepResult{Text: "叙事文本"},
			wantLane: storage.AgentLaneText,
		},
		{
			name:     "undecided with refusal resolves to text",
			result:   llm.AgentStepResult{Refusal: "拒绝内容"},
			wantLane: storage.AgentLaneText,
		},
		{
			name:     "undecided with tool calls resolves to tool",
			result:   llm.AgentStepResult{ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "fc_request_roll"}}},
			wantLane: storage.AgentLaneTool,
		},
		{
			name:     "undecided with nothing stays undecided",
			result:   llm.AgentStepResult{},
			wantLane: storage.AgentLaneUndecided,
		},
		{
			name: "text lane with trailing tool calls resolves to tool",
			result: llm.AgentStepResult{
				Text:      "让我为你掷骰……",
				ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "fc_request_roll"}},
			},
			wantLane: storage.AgentLaneTool,
		},
		{
			name: "undecided mixed output resolves to tool",
			result: llm.AgentStepResult{
				Text:      "前导文本",
				ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "fc_state_set_text"}},
			},
			wantLane: storage.AgentLaneTool,
		},
		{
			name: "tool lane with preamble text stays tool without error",
			result: llm.AgentStepResult{
				Text:      "调用工具前的碎碎念",
				ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "fc_state_set_text"}},
			},
			wantLane: storage.AgentLaneTool,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lane := resolveAgentLane(tt.result)
			if lane != tt.wantLane {
				t.Fatalf("lane = %q, want %q", lane, tt.wantLane)
			}
		})
	}
}
