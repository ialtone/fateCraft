package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ialtone/fatecraft/internal/contextbuilder"
	"github.com/ialtone/fatecraft/internal/llm"
	"github.com/ialtone/fatecraft/internal/storage"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEndingPreviewUsesProposalToolWithoutPlayerExecutionInBothProtocols(t *testing.T) {
	snapshot := agentToolSnapshot{Bindings: map[string]agentToolBinding{}}
	projection := map[string]any{"ending_capabilities": []any{map[string]any{"key": "trial.abandon", "description": "提前中止，不奖励积分", "available": true, "revision": int64(5), "argument_schema": agentClosedObject(map[string]any{}), "metadata": map[string]any{"label": "提前中止"}}}}
	if err := addEndingTool(&snapshot, projection); err != nil {
		t.Fatal(err)
	}
	binding := snapshot.Bindings["fc_propose_ending"]
	if binding.Mutating || binding.Kind != agentToolEnding || len(snapshot.Bindings) != 1 {
		t.Fatal("proposal grants execution authority")
	}
	notice := endingNotice(snapshot)
	built, err := contextbuilder.Build(contextbuilder.Input{MaxContextTokens: 20000, ReservedResponseTokens: 256, RuntimeDirective: notice, History: []storage.ChatMessage{{Role: "user", Content: "我确定，结束吧"}}}, batchCounter{})
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
				field := "messages"
				if protocol == llm.ProtocolResponses {
					field = "input"
				}
				wire := string(body[field])
				tools := string(body["tools"])
				if !strings.Contains(wire, "ending_options") || !strings.Contains(wire, "不授予模型执行权") || !strings.Contains(tools, "fc_propose_ending") {
					t.Errorf("missing proposal contract: %s %s", wire, tools)
				}
				if strings.Contains(tools, "fc_domain_trial_abandon") {
					t.Error("player executor exposed")
				}
				if strings.Index(wire, "我确定") > strings.Index(wire, "ending_options") {
					t.Error("ending notice precedes history")
				}
				if protocol == llm.ProtocolResponses {
					fmt.Fprint(w, `{"status":"completed","output":[{"type":"function_call","call_id":"proposal","name":"fc_propose_ending","arguments":"{\"action_key\":\"trial.abandon\",\"arguments_json\":\"{}\"}"}]}`)
				} else {
					fmt.Fprint(w, `{"choices":[{"message":{"tool_calls":[{"id":"proposal","type":"function","function":{"name":"fc_propose_ending","arguments":"{\"action_key\":\"trial.abandon\",\"arguments_json\":\"{}\"}"}}]},"finish_reason":"tool_calls"}]}`)
				}
			}))
			defer server.Close()
			result, err := llm.NewClient().AgentStep(context.Background(), llm.Params{BaseURL: server.URL, APIKey: "test", Model: "test", Protocol: protocol}, built.Messages, []llm.ToolDefinition{binding.Definition}, false, nil)
			if err != nil || requests != 1 || len(result.ToolCalls) != 1 || result.ToolCalls[0].Name != "fc_propose_ending" {
				t.Fatalf("proposal not decoded: %+v %v", result, err)
			}
		})
	}
}
func TestBoundedResultKeepsCompletionReceipt(t *testing.T) {
	view := storage.NewCompletion(storage.Conversation{}, 1, "stable", storage.ChallengeOutcomeRule{OutcomeKey: "abandoned", Category: "neutral"})
	text, result := boundedToolResult(storage.JSONMap{"ok": true, "code": "module_settled", "data": map[string]any{"completion": view, "events": strings.Repeat("x", maxPublicToolResultBytes)}})
	if len(text) > maxPublicToolResultBytes || !strings.Contains(text, "stable/") {
		t.Fatal("completion discarded")
	}
	raw, _ := json.Marshal(result["data"])
	var data storage.JSONMap
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	if storage.CompletionFromResult(data) == nil {
		t.Fatal("receipt cannot be recovered")
	}
}
