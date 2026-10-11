package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/ialtone/fatecraft/internal/chat"
	"github.com/ialtone/fatecraft/internal/contextbuilder"
	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/llm"
	"github.com/ialtone/fatecraft/internal/operation"
	"github.com/ialtone/fatecraft/internal/state"
	"github.com/ialtone/fatecraft/internal/storage"
)

// Exercise the real tool transactions and context builder against the full SQL
// schema. A provider may return state + guidance together even with parallel
// tools disabled; their replay must still be one declaration and two results.
func TestPostgresAgentBatchPersistenceAndReplay(t *testing.T) {
	for _, protocol := range []llm.Protocol{llm.ProtocolChatCompletions, llm.ProtocolResponses} {
		t.Run(string(protocol), func(t *testing.T) {
			db := openAgentTransactionTestDB(t)
			ctx, ids, g, conv, step, snapshot, contract := seedAgentBatch(t, db)
			calls := []llm.ToolCall{
				{ID: "state-call", Name: "fc_state_set_text", Arguments: `{"updates":[{"path":"narrative.name","value":"沈锋"}]}`},
				{ID: "guidance-call", Name: "fc_present_guidance", Arguments: `{"title":"何处求法？","options":[{"label":"药庐","message":"去药庐求法"},{"label":"剑崖","message":"去剑崖求法"}]}`},
			}
			blocks := []llm.ContentBlock{{Kind: llm.ContentThinking, Text: "先初始化人物，再给出方向。"}}
			if protocol == llm.ProtocolResponses {
				blocks[0].Replay = json.RawMessage(`{"type":"reasoning","id":"reason-batch","summary":[{"type":"summary_text","text":"先初始化人物，再给出方向。"}],"encrypted_content":"test-sealed"}`)
				calls[0].ItemID, calls[1].ItemID = "function-item-1", "function-item-2"
			}
			for i := range calls {
				blocks = append(blocks, llm.ContentBlock{Kind: llm.ContentToolCall, ToolCall: &calls[i]})
			}
			for i, call := range calls {
				var declaration storage.JSONDoc
				if i == 0 {
					declaration = agentContentBlocksDoc(blocks)
				}
				out, err := g.processAgentToolCall(ctx, conv, contract, nil, step, snapshot, call, true, declaration, nil, nil)
				if err != nil || out.Status != storage.ToolInvocationCommitted {
					t.Fatalf("call %d: status=%s code=%s err=%v", i+1, out.Status, out.Code, err)
				}
				// An HTTP/tool retry must not append messages or apply the state again.
				if replay, err := g.processAgentToolCall(ctx, conv, contract, nil, step, snapshot, call, true, declaration, nil, nil); err != nil || replay.Status != out.Status {
					t.Fatalf("replay call %d: %+v %v", i+1, replay, err)
				}
			}
			var revisions int64
			if err := db.Model(&storage.StateRevision{}).Where("conversation_id = ?", conv.ID).Count(&revisions).Error; err != nil || revisions != 1 {
				t.Fatalf("state revisions=%d err=%v", revisions, err)
			}
			history, err := storage.NewMessageRepo(db).ListChat(ctx, conv.ID, true)
			if err != nil || len(history) != 3 || history[0].Kind != "tool_call" || history[1].Kind != "tool_result" || history[2].Kind != "tool_result" {
				t.Fatalf("history=%+v err=%v", history, err)
			}
			built, err := contextbuilder.Build(contextbuilder.Input{History: history, CurrentTurnID: &ids.turnID, MaxContextTokens: 100000, ReservedResponseTokens: 1000}, batchCounter{})
			if err != nil {
				t.Fatal(err)
			}
			replay, err := llm.PrepareToolReplay(built.Messages)
			if err != nil || len(replay.Repairs) != 0 {
				t.Fatalf("valid committed batch required replay repair: %+v err=%v", replay.Repairs, err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					return
				}
				wire := string(body["messages"])
				if protocol == llm.ProtocolResponses {
					wire = string(body["input"])
				}
				for _, id := range []string{"state-call", "guidance-call"} {
					if strings.Count(wire, id) != 2 { // exactly one call and its result
						t.Errorf("wire must contain one call/result pair for %s: %s", id, wire)
					}
				}
				if strings.Contains(wire, "tool_replay_error") || strings.Count(wire, "先初始化人物") != 1 {
					t.Errorf("lost/duplicated reasoning or fabricated failure: %s", wire)
				}
				if protocol == llm.ProtocolResponses {
					fmt.Fprint(w, `{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"沈锋背剑走入山城。"}]}]}`)
				} else {
					fmt.Fprint(w, `{"choices":[{"message":{"content":"沈锋背剑走入山城。"},"finish_reason":"stop"}]}`)
				}
			}))
			defer server.Close()
			result, err := llm.NewClient().AgentStep(ctx, llm.Params{BaseURL: server.URL, APIKey: "test", Model: "test", Protocol: protocol}, built.Messages, nil, false, nil)
			if err != nil || result.TextProjection() != "沈锋背剑走入山城。" {
				t.Fatalf("narrative continuation: %+v %v", result, err)
			}
			if _, _, err := chat.NewService(storage.NewMessageRepo(db)).AppendAssistantWithIDAndBlocks(ctx, conv.ID, uuid.New(), result.TextProjection(), result.TextProjection(), nil, nil, agentContentBlocksDoc(result.CanonicalBlocks())); err != nil {
				t.Fatal(err)
			}
		})
	}
}

type batchCounter struct{}

func (batchCounter) Count(s string) int { return len([]rune(s)) }

func TestPostgresAgentBatchFailureBoundaries(t *testing.T) {
	for _, scenario := range []string{"two_writes", "rejected", "transaction_failure", "interrupted", "mismatched_arguments"} {
		t.Run(scenario, func(t *testing.T) {
			db := openAgentTransactionTestDB(t)
			ctx, ids, g, conv, step, snapshot, contract := seedAgentBatch(t, db)
			calls := []llm.ToolCall{
				{ID: "first", Name: "fc_state_set_text", Arguments: `{"updates":[{"path":"narrative.name","value":"first"}]}`},
				{ID: "second", Name: "fc_state_set_text", Arguments: `{"updates":[{"path":"narrative.name","value":"second"}]}`},
			}
			if scenario == "rejected" {
				calls[1].Arguments = `{"updates":[{"path":"unowned.secret","value":"forbidden"}]}`
			}
			blocks := agentContentBlocksDoc([]llm.ContentBlock{
				{Kind: llm.ContentToolCall, ToolCall: &calls[0]}, {Kind: llm.ContentToolCall, ToolCall: &calls[1]},
			})
			out, err := g.processAgentToolCall(ctx, conv, contract, nil, step, snapshot, calls[0], true, blocks, nil, nil)
			if err != nil || out.Status != storage.ToolInvocationCommitted {
				t.Fatalf("first call: %+v %v", out, err)
			}
			if scenario == "transaction_failure" {
				// Fail only the successful second output. Its state write must roll
				// back, while the separately committed failure result remains replayable.
				if err := db.Exec(`CREATE FUNCTION reject_second_output() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
				IF NEW.kind = 'tool_result' AND NEW.tool_call_id = 'second' AND NEW.content::jsonb->>'ok' = 'true' THEN
				RAISE EXCEPTION 'injected second output failure'; END IF; RETURN NEW; END $$;
				CREATE TRIGGER reject_second_output BEFORE INSERT ON chat_messages FOR EACH ROW EXECUTE FUNCTION reject_second_output();`).Error; err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "mismatched_arguments" {
				calls[1].Arguments = `{"updates":[{"path":"narrative.name","value":"tampered"}]}`
			}
			if scenario != "interrupted" {
				out, err = g.processAgentToolCall(ctx, conv, contract, nil, step, snapshot, calls[1], true, nil, nil, nil)
				want := map[string]string{"two_writes": storage.ToolInvocationCommitted, "rejected": storage.ToolInvocationRejected, "transaction_failure": storage.ToolInvocationFailed}
				if scenario == "mismatched_arguments" {
					if agentErrorCode(err) != "tool_persistence_failed" {
						t.Fatalf("expected persistence failure, got %+v %v", out, err)
					}
				} else if err != nil || out.Status != want[scenario] {
					t.Fatalf("second call: %+v %v", out, err)
				}
			}
			current, err := storage.NewStateRepo(db).Get(ctx, conv.ID)
			if err != nil {
				t.Fatal(err)
			}
			name := current.State["narrative"].(map[string]any)["name"]
			wantName, wantCount := "first", 1
			if scenario == "two_writes" {
				wantName, wantCount = "second", 2
			}
			var attempt storage.GenerationAttempt
			if err := db.First(&attempt, "id = ?", ids.attemptID).Error; err != nil || name != wantName || attempt.CommittedMutatingToolCount != wantCount {
				t.Fatalf("state=%v committed=%d err=%v", name, attempt.CommittedMutatingToolCount, err)
			}
			history, err := storage.NewMessageRepo(db).ListChat(ctx, conv.ID, true)
			if err != nil {
				t.Fatal(err)
			}
			built, err := contextbuilder.Build(contextbuilder.Input{History: history, CurrentTurnID: &ids.turnID, MaxContextTokens: 100000, ReservedResponseTokens: 1000}, batchCounter{})
			if err != nil {
				t.Fatal(err)
			}
			// Continuation supplies a new intent; missing outputs must be closed
			// before it, without executing or claiming success for the second call.
			replay, err := llm.PrepareToolReplay(append(built.Messages, llm.Message{Role: "user", Content: "继续叙事"}))
			wantRepairs := 0
			if scenario == "interrupted" || scenario == "mismatched_arguments" {
				wantRepairs = 1
			}
			if err != nil || len(replay.Repairs) != wantRepairs {
				t.Fatalf("replay repairs=%+v err=%v", replay.Repairs, err)
			}
			if wantRepairs == 1 && (replay.Repairs[0].CallID != "second" || replay.Repairs[0].Kind != llm.ReplayRepairMissingOutput) {
				t.Fatalf("repaired the already committed action: %+v", replay.Repairs)
			}
		})
	}
}

func seedAgentBatch(t *testing.T, db *gorm.DB) (context.Context, agentTransactionIDs, *Generator, *storage.Conversation, *storage.AgentStep, agentToolSnapshot, *gameplay.RuntimeContract) {
	t.Helper()
	_, ids := seedAgentTransactionTest(t, db)
	runID := uuid.New()
	if err := db.Exec(`INSERT INTO conversation_generation_runs (id, conversation_id, request_id, operation_type, payload_hash, conversation_revision, status, lease_expires_at) VALUES (?, ?, ?, 'user_input', ?, 0, 'running', ?)`, runID, ids.conversationID, uuid.New(), strings.Repeat("0", 64), time.Now().Add(time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	ctx := operation.WithMetadata(operation.WithGeneration(context.Background(), ids.conversationID, runID), operation.Metadata{ConversationID: ids.conversationID, TurnID: ids.turnID, AttemptID: ids.attemptID})
	contract := &gameplay.RuntimeContract{Mode: gameplay.RuntimeModeChallenge, StateSchema: gameplay.StateSchema{SchemaVersion: 1, Fields: []gameplay.StateField{{Path: "narrative.name", Type: "string", Default: "", Visibility: gameplay.VisibilityPlayer, UpdatePolicy: gameplay.UpdateModel}}}, Actions: gameplay.ActionConfig{Model: []string{"update_state"}}}
	snapshot, err := buildAgentToolSnapshot(contract, nil, nil, false, false)
	if err != nil {
		t.Fatal(err)
	}
	g := &Generator{agents: storage.NewAgentRepo(db), toolTx: NewAgentToolTransaction(db), stateSvc: state.NewService(storage.NewStateRepo(db))}
	return ctx, ids, g, &storage.Conversation{ID: ids.conversationID}, &storage.AgentStep{ID: ids.stepID, StepNo: 1}, snapshot, contract
}
