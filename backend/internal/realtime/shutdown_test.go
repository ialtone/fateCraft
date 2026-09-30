package realtime

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/logging"
)

func TestQueuedGenerationUsesCommandTrace(t *testing.T) {
	h := NewHub(nil, nil)
	conversation := uuid.New()
	first, second := logging.NewTrace(context.Background()), logging.NewTrace(context.Background())
	started, release, trace := make(chan struct{}), make(chan struct{}), make(chan string, 1)
	if !h.RunDetached(first, conversation, func(context.Context, EmitFunc) { close(started); <-release }) {
		t.Fatal("start failed")
	}
	<-started
	if err := h.QueueDetached(second, conversation, func(ctx context.Context, _ EmitFunc) { trace <- logging.TraceID(ctx) }); err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case value := <-trace:
		if value != logging.TraceID(second) {
			t.Fatal("queued command reused prior trace")
		}
	case <-time.After(time.Second):
		t.Fatal("queue did not run")
	}
	if err := h.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestHubShutdownStopsProducers(t *testing.T) {
	h := NewHub(nil, nil)
	started, finished := make(chan struct{}), make(chan struct{})
	h.RunDetached(context.Background(), uuid.New(), func(ctx context.Context, _ EmitFunc) { close(started); <-ctx.Done(); close(finished) })
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := h.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	default:
		t.Fatal("shutdown returned before producer stopped")
	}
	if h.RunDetached(ctx, uuid.New(), func(context.Context, EmitFunc) {}) {
		t.Fatal("accepted producer after shutdown")
	}
	if err := h.QueueDetached(ctx, uuid.New(), func(context.Context, EmitFunc) {}); err != errHubClosed {
		t.Fatalf("queue after shutdown=%v", err)
	}
}
