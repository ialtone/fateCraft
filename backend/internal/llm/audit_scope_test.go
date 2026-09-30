package llm

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/storage"
)

func TestScopedCaptureKeepsSummariesAndBusinessOutput(t *testing.T) {
	user, conversation, other := uuid.New(), uuid.New(), uuid.New()
	for _, relay := range []bool{false, true} {
		for _, tt := range []struct {
			name, scope, player, conv string
			target                    *uuid.UUID
			capture                   bool
		}{
			{"all", "all", "", "", nil, true},
			{"user match", "user", user.String(), conversation.String(), &user, true},
			{"user miss", "user", other.String(), conversation.String(), &user, false},
			{"missing user", "user", "", conversation.String(), &user, false},
			{"conversation match", "conversation", user.String(), conversation.String(), &conversation, true},
			{"conversation miss", "conversation", user.String(), other.String(), &conversation, false},
			{"no conversation", "conversation", user.String(), "", &conversation, false},
		} {
			name := "server/" + tt.name
			if relay {
				name = "relay/" + tt.name
			}
			t.Run(name, func(t *testing.T) {
				settings := storage.DefaultSystemSettings()
				settings.DetailedLLMLoggingEnabled, settings.DetailedLLMLoggingScope, settings.DetailedLLMLoggingTargetID = true, tt.scope, tt.target
				client := NewClient()
				capture := &requestAuditCapture{}
				decisions := 0
				client.SetRequestAudit(capture.write, func(_ context.Context, p Params) bool {
					decisions++
					return settings.CaptureDetailedLLMLog(p.PlayerID, p.ConversationID)
				})
				respond := func(body []byte) *http.Response {
					if !strings.Contains(string(body), "player content") {
						t.Error("business prompt was lost")
					}
					if strings.Contains(string(body), user.String()) || strings.Contains(string(body), "PlayerID") {
						t.Error("capture attribution sent upstream")
					}
					// A settings edit during the stream affects the next logical call only.
					settings.DetailedLLMLoggingEnabled = false
					return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))}
				}
				p := Params{Protocol: ProtocolChatCompletions, BaseURL: "https://example.test/v1", APIKey: "test-key", Model: "m", PlayerID: tt.player, ConversationID: tt.conv}
				if relay {
					p.Relay = &scriptedRelayFunc{do: func(_ context.Context, req RelayRequest) (*http.Response, error) { return respond(req.Body), nil }}
				} else {
					client.http.Transport = auditRoundTripper(func(r *http.Request) (*http.Response, error) {
						body, err := io.ReadAll(r.Body)
						if err != nil {
							t.Fatal(err)
						}
						return respond(body), nil
					})
				}
				result, err := client.Stream(context.Background(), p, []Message{{Role: storage.RoleUser, Content: "player content"}}, nil)
				if err != nil || result != "ok" || len(capture.items) != 1 || decisions != 1 {
					t.Fatalf("result=%q error=%v summaries=%d decisions=%d", result, err, len(capture.items), decisions)
				}
				a := capture.items[0]
				if !a.Success || a.ConversationID != tt.conv {
					t.Fatal("summary lost")
				}
				if tt.capture {
					if (a.BodyStatus != "captured" && a.BodyStatus != "truncated") || a.RawResponse == "" || a.ResponseText != "ok" || !strings.Contains(string(a.RequestBody), "player content") {
						t.Fatalf("matching body not captured: %+v", a)
					}
				} else if a.BodyStatus != "disabled" || a.RawResponse != "" || a.ResponseText != "" || string(a.RequestBody) != "{}" {
					t.Fatalf("out-of-scope body captured: %+v", a)
				}
			})
		}
	}
}
