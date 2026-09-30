package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/logging"
	"github.com/ialtone/fatecraft/internal/realtime"
)

func TestAgentFailureDoesNotExposeInternalError(t *testing.T) {
	ctx := logging.NewTrace(context.Background())
	conversation := uuid.New()
	var events []realtime.Event
	emit := func(e realtime.Event) { events = append(events, e) }
	g := &Generator{}
	g.finishAgentFailure(ctx, newRuntimeEmitter(ctx, conversation, emit), conversation, 1, false, errors.New("synthetic-secret database connection details"), emit)
	encoded, _ := json.Marshal(events)
	if strings.Contains(string(encoded), "synthetic-secret") || !strings.Contains(string(encoded), logging.TraceID(ctx)) {
		t.Fatalf("unsafe/missing trace in events: %s", encoded)
	}
}
