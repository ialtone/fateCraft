package orchestration

import (
	"strings"
	"testing"

	"github.com/ialtone/fatecraft/internal/gameplay"
	"github.com/ialtone/fatecraft/internal/llm"
)

func TestValidateAgentToolCallBatch(t *testing.T) {
	tests := []struct {
		name    string
		calls   []llm.ToolCall
		wantErr string
	}{
		{
			name: "accepts distinct calls in provider order",
			calls: []llm.ToolCall{
				{ID: "call-1", Name: "fc_state_set_text", Arguments: `{}`},
				{ID: "call-2", Name: "fc_state_set_number", Arguments: `{}`},
			},
		},
		{
			name: "rejects duplicate call ids",
			calls: []llm.ToolCall{
				{ID: "call-1", Name: "fc_state_set_text", Arguments: `{}`},
				{ID: "call-1", Name: "fc_state_set_number", Arguments: `{}`},
			},
			wantErr: "重复 call_id",
		},
		{
			name:    "rejects malformed call id",
			calls:   []llm.ToolCall{{ID: " call-1", Name: "fc_state_set_text", Arguments: `{}`}},
			wantErr: "稳定 call_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAgentToolCallBatch(tt.calls)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestFinalizeAgentToolSnapshotRecordsSerialExecutionMode(t *testing.T) {
	snapshot, err := finalizeAgentToolSnapshot(agentToolSnapshot{
		Bindings: map[string]agentToolBinding{
			"fc_state_set_text": {
				Definition: llm.ToolDefinition{
					Name: "fc_state_set_text", Parameters: map[string]any{"type": "object"},
				},
				Kind: agentToolStateText, Mutating: true,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Hash == "" || len(snapshot.JSON) == 0 {
		t.Fatal("snapshot was not finalized")
	}
	if !strings.Contains(string(snapshot.JSON), `"execution_mode":"serial"`) {
		t.Fatalf("snapshot = %s", snapshot.JSON)
	}
}

func TestCampaignToolSnapshotBindsOwnershipAndOmitsDynamicStateTool(t *testing.T) {
	contract := &gameplay.RuntimeContract{
		Engine: gameplay.EngineRef{Key: "test.campaign", Version: 1}, Mode: gameplay.RuntimeModeCampaign,
		Campaign: &gameplay.CampaignConfig{
			LifecycleHandler: "test.lifecycle", ProjectionHandler: "test.projection", ContextPolicy: "test.context",
			ActionAllowlist: []string{"test.inspect"}, InitialLocationKey: "world.start",
			FieldOwnership: []gameplay.FieldOwnership{{Path: "campaign", Owner: gameplay.OwnerCampaign, ValueType: "object", Visibility: gameplay.VisibilityPlayer, StorageProjection: "campaigns"}},
			WorldClock:     gameplay.WorldClockConfig{CalendarKey: "world.default", MaxDueEventsPerTurn: 1},
		},
		StateSchema: gameplay.StateSchema{SchemaVersion: 1, Fields: []gameplay.StateField{{
			Path: "narrative.current_focus", Type: "string", Default: "", Visibility: gameplay.VisibilityPlayer, UpdatePolicy: gameplay.UpdateModel,
		}}},
		Actions: gameplay.ActionConfig{Model: []string{"update_state"}},
	}
	snapshot, err := buildAgentToolSnapshot(contract, nil, nil, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.OwnershipHash == "" || !strings.Contains(string(snapshot.JSON), snapshot.OwnershipHash) {
		t.Fatalf("ownership hash missing from tool snapshot: %#v", snapshot)
	}
	if strings.Contains(string(snapshot.JSON), "fc_state_set_dynamic_json") {
		t.Fatalf("Campaign snapshot exposed dynamic state tool: %s", snapshot.JSON)
	}
}
