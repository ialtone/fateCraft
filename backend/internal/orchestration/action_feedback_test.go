package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/contextbuilder"
	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/llm"
	"github.com/ialtone/fatecraft/internal/storage"
)

func blockedNarrativeSnapshot(t *testing.T) agentToolSnapshot {
	t.Helper()
	contract := &gameplay.RuntimeContract{Mode: gameplay.RuntimeModeChallenge, Actions: gameplay.ActionConfig{Model: []string{"domain_action"}, Domain: []string{"trial.breakthrough"}}}
	snapshot, err := buildAgentToolSnapshot(contract, map[string]any{"revision": 5, "action_capabilities": []campaign.ActionCapability{{Key: "trial.breakthrough", Revision: 5, Available: false, Violations: []campaign.ActionViolation{{Code: "action.unavailable", Message: "修为 52/100，未达到圆满"}}}}}, nil, false, false)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestUnavailableToolDiagnosisDoesNotAuthorizeOrReuseStalePreview(t *testing.T) {
	snapshot := blockedNarrativeSnapshot(t)
	name := campaignToolName("trial.breakthrough")
	if _, exists := snapshot.Bindings[name]; exists {
		t.Fatal("unavailable action became executable")
	}
	if out := unavailableToolOutcome(snapshot, name); out.Code != "action_unavailable" || !strings.Contains(out.Result["message"].(string), "52/100") || out.Mutating {
		t.Fatalf("bad rejection: %+v", out)
	}
	revision := int64(6)
	snapshot.ObservedRevision = &revision
	if out := unavailableToolOutcome(snapshot, name); out.Code != "action_snapshot_stale" || strings.Contains(out.Result["message"].(string), "52/100") {
		t.Fatalf("stale reason treated as current: %+v", out)
	}
	if out := unavailableToolOutcome(snapshot, name+"_typo"); out.Code != "unknown_tool" || out.Mutating {
		t.Fatalf("unknown tool corrected/authorized: %+v", out)
	}
}

func TestSnapshotDistinguishesCatalogLimitsFromRuleRestrictions(t *testing.T) {
	contract := &gameplay.RuntimeContract{Mode: gameplay.RuntimeModeChallenge, Actions: gameplay.ActionConfig{Model: []string{"domain_action"}}}
	capabilities := []campaign.ActionCapability{}
	for index := 0; index <= maxDomainToolsPerStep; index++ {
		key := fmt.Sprintf("test.action_%02d", index)
		contract.Actions.Domain = append(contract.Actions.Domain, key)
		capabilities = append(capabilities, campaign.ActionCapability{Key: key, Revision: 3, Available: true, ArgumentSchema: storage.JSONMap{"type": "object", "properties": map[string]any{}, "additionalProperties": false}})
	}
	capabilities = append(capabilities, campaign.ActionCapability{Key: "hidden.undeclared", Available: false, Violations: []campaign.ActionViolation{{Message: "private-rule"}}})
	snapshot, err := buildAgentToolSnapshot(contract, map[string]any{"revision": 3, "action_capabilities": capabilities}, nil, false, false)
	if err != nil {
		t.Fatal(err)
	}
	last := campaignToolName(contract.Actions.Domain[maxDomainToolsPerStep])
	if out := unavailableToolOutcome(snapshot, last); out.Code != "action_not_provided" || strings.Contains(out.Result["message"].(string), "前置") {
		t.Fatalf("catalog limit misdiagnosed: %+v", out)
	}
	if _, exists := snapshot.Restrictions[campaignToolName("hidden.undeclared")]; exists {
		t.Fatal("undeclared action leaked")
	}
}

func TestRestrictionNoticeIsBoundedAndRecentFailureGetsPriority(t *testing.T) {
	snapshot := blockedNarrativeSnapshot(t)
	for index := 0; index < 64; index++ {
		name := fmt.Sprintf("fc_action_test_%02d", index)
		snapshot.Restrictions[name] = agentActionRestriction{ToolName: name, ActionKey: name, Message: strings.Repeat("限制说明", 100)}
	}
	name := campaignToolName("trial.breakthrough")
	notice := actionRestrictionNotice(snapshot, []storage.ChatMessage{{ToolName: name, Role: "tool"}})
	if len(notice) > 4200 || !strings.Contains(notice, "52/100") || !strings.Contains(notice, "omitted_count") || strings.Contains(notice, "argument_schema") {
		t.Fatalf("invalid notice: %s", notice)
	}
}

func TestToolResultTruncationPreservesCommittedFailureFacts(t *testing.T) {
	feedback, err := gameplay.NarrativeFeedback("trial.breakthrough", 4, 5, map[string]any{"realm": "凡人", "progress": 100}, map[string]any{"realm": "凡人", "progress": 80}, []string{"realm", "progress"}, map[string]any{"success": false}, false)
	if err != nil {
		t.Fatal(err)
	}
	text, _ := boundedToolResult(storage.JSONMap{"ok": true, "code": "module_committed", "data": map[string]any{"revision": 5, "narrative_feedback": feedback, "events": strings.Repeat("x", maxPublicToolResultBytes)}})
	if len(text) > maxPublicToolResultBytes || !strings.Contains(text, `"goal_succeeded":false`) || !strings.Contains(text, `"realm":"凡人"`) || !strings.Contains(text, `"revision":5`) || !strings.Contains(text, `"truncated":true`) {
		t.Fatalf("essential facts truncated: %s", text)
	}
}

func TestNarrativeRequestIncludesFactsAndRestrictionInBothProtocols(t *testing.T) {
	snapshot := blockedNarrativeSnapshot(t)
	projection := narrativeProjection(map[string]any{"revision": 5, "status": "active", "narrative_facts": map[string]any{"realm": "凡人", "progress": 52}, "action_capabilities": []any{map[string]any{"untrusted": "not an execution directory"}}})
	if _, exists := projection["action_capabilities"]; exists {
		t.Fatal("internal action directory leaked")
	}
	out := unavailableToolOutcome(snapshot, campaignToolName("trial.breakthrough"))
	resultJSON, _ := boundedToolResult(out.Result)
	call := llm.ToolCall{ID: "blocked-call", Name: campaignToolName("trial.breakthrough"), Arguments: `{}`}
	built, err := contextbuilder.Build(contextbuilder.Input{
		MaxContextTokens: 20000, ReservedResponseTokens: 256, CampaignProjection: projection,
		RuntimeDirective: actionRestrictionNotice(snapshot, nil), OutputContract: outputContract,
		History: []storage.ChatMessage{
			{Role: "assistant", Kind: "tool_call", ToolCallID: call.ID, ToolName: call.Name, ContentBlocks: agentContentBlocksDoc([]llm.ContentBlock{{Kind: llm.ContentToolCall, ToolCall: &call}})},
			{Role: "tool", Kind: "tool_result", ToolCallID: call.ID, ToolName: call.Name, Content: resultJSON},
		},
	}, batchCounter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, protocol := range []llm.Protocol{llm.ProtocolChatCompletions, llm.ProtocolResponses} {
		t.Run(string(protocol), func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				var body map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					return
				}
				wire := string(body["messages"])
				if protocol == llm.ProtocolResponses {
					wire = string(body["input"])
				}
				for _, required := range []string{"凡人", "52/100", "narrative_facts", "action_restrictions", "本调用没有提交", "优先于历史正文"} {
					if !strings.Contains(wire, required) {
						t.Errorf("request lost %s: %s", required, wire)
					}
				}
				if strings.Count(wire, call.ID) != 2 {
					t.Errorf("broken call/result pairing: %s", wire)
				}
				if protocol == llm.ProtocolResponses {
					fmt.Fprint(w, `{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"当前仍是凡人，尚未满足突破条件。"}]}]}`)
				} else {
					fmt.Fprint(w, `{"choices":[{"message":{"content":"当前仍是凡人，尚未满足突破条件。"},"finish_reason":"stop"}]}`)
				}
			}))
			defer server.Close()
			_, err := llm.NewClient().AgentStep(context.Background(), llm.Params{BaseURL: server.URL, APIKey: "test", Model: "test", Protocol: protocol}, built.Messages, nil, false, nil)
			if err != nil || requests != 1 {
				t.Fatalf("request count=%d err=%v", requests, err)
			}
		})
	}
}
