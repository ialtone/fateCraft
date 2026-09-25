package orchestration

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ialtone/fatecraft/internal/guidance"
	"github.com/ialtone/fatecraft/internal/storage"
)

func TestGuidanceToolStagesOnlyValidatedIntent(t *testing.T) {
	g := &Generator{}
	binding := agentToolBinding{Kind: agentToolGuidance}
	for _, tc := range []struct {
		raw    string
		status string
	}{
		{`{"title":"Next","options":[{"label":"Ask","message":"Ask for directions"},{"label":"Wait","message":"Wait here"}]}`, storage.ToolInvocationCommitted},
		{`{"title":"Next","options":[{"label":"Ask","message":"Ask","action_key":"gain_item"},{"label":"Wait","message":"Wait"}]}`, storage.ToolInvocationRejected},
	} {
		var args map[string]any
		if err := json.Unmarshal([]byte(tc.raw), &args); err != nil {
			t.Fatal(err)
		}
		out := g.executeAgentToolInTx(context.Background(), nil, nil, nil, binding, args, "call", nil)
		if out.Status != tc.status || out.Mutating || len(out.Events) != 0 {
			t.Fatalf("unexpected tool outcome %#v", out)
		}
		if out.Status == storage.ToolInvocationCommitted {
			raw, _ := json.Marshal(out.Result["data"])
			if _, err := guidance.Decode(raw); err != nil {
				t.Fatal(err)
			}
		}
	}
}
