package realtime

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func testDeltaEvent(kind, messageID, delta string, sequence int64) Event {
	return Event{
		Type: kind, EventID: uuid.NewString(), ConversationID: "conversation",
		TurnID: "turn", AttemptID: "attempt", StepNo: 1, Sequence: sequence,
		Payload: map[string]any{"message_id": messageID, "delta": delta, "preview": true},
	}
}

func readEvent(t *testing.T, c *conn) Event {
	t.Helper()
	select {
	case data := <-c.send:
		var ev Event
		if err := json.Unmarshal(data, &ev); err != nil {
			t.Fatalf("decode event: %v", err)
		}
		return ev
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
		return Event{}
	}
}

func TestConnCoalescesAdjacentDeltasAndPreservesBoundary(t *testing.T) {
	c := newConn(nil, uuid.New())
	first := testDeltaEvent(TypeAgentTextDelta, "message", "前", 1)
	c.enqueue(first)
	c.enqueue(testDeltaEvent(TypeAgentTextDelta, "message", "半", 2))
	c.enqueue(Event{Type: TypeAgentToolStarted, Sequence: 3, Payload: map[string]any{"name": "roll"}})

	merged := readEvent(t, c)
	if merged.Type != TypeAgentTextDelta || merged.Sequence != 2 {
		t.Fatalf("merged event = %#v", merged)
	}
	payload, ok := merged.Payload.(map[string]any)
	if !ok || payload["delta"] != "前半" || payload["message_id"] != "message" {
		t.Fatalf("merged payload = %#v", merged.Payload)
	}
	if payload["sequence_start"] != float64(1) {
		t.Fatalf("merged sequence_start = %#v", payload["sequence_start"])
	}
	if merged.EventID == "" || merged.EventID == first.EventID {
		t.Fatalf("merged event must have a fresh event id: %#v", merged)
	}
	control := readEvent(t, c)
	if control.Type != TypeAgentToolStarted {
		t.Fatalf("control event = %#v", control)
	}
	c.close()
}

func TestConnDoesNotMergeAcrossDeltaBoundaries(t *testing.T) {
	c := newConn(nil, uuid.New())
	c.enqueue(testDeltaEvent(TypeAgentReasoningDelta, "message", "思考", 1))
	c.enqueue(testDeltaEvent(TypeAgentTextDelta, "message", "正文", 2))
	c.enqueue(testDeltaEvent(TypeAgentTextDelta, "other-message", "另一条", 3))
	c.enqueue(Event{Type: TypeAgentTextReset, Sequence: 4})

	for i, want := range []struct {
		typ  string
		text string
	}{
		{TypeAgentReasoningDelta, "思考"},
		{TypeAgentTextDelta, "正文"},
		{TypeAgentTextDelta, "另一条"},
	} {
		ev := readEvent(t, c)
		if ev.Type != want.typ {
			t.Fatalf("event %d type = %q, want %q", i, ev.Type, want.typ)
		}
		payload, ok := ev.Payload.(map[string]any)
		if !ok || payload["delta"] != want.text {
			t.Fatalf("event %d payload = %#v, want delta %q", i, ev.Payload, want.text)
		}
	}
	if got := readEvent(t, c).Type; got != TypeAgentTextReset {
		t.Fatalf("reset type = %q", got)
	}
	c.close()
}

func TestConnFlushesDeltaAfterInterval(t *testing.T) {
	originalInterval := wsDeltaFlushInterval
	wsDeltaFlushInterval = 5 * time.Millisecond
	defer func() { wsDeltaFlushInterval = originalInterval }()

	c := newConn(nil, uuid.New())
	c.enqueue(testDeltaEvent(TypeAgentReasoningDelta, "message", "定时", 1))
	ev := readEvent(t, c)
	if ev.Type != TypeAgentReasoningDelta {
		t.Fatalf("event type = %q", ev.Type)
	}
	payload, ok := ev.Payload.(map[string]any)
	if !ok || payload["delta"] != "定时" {
		t.Fatalf("payload = %#v", ev.Payload)
	}
	c.close()
}

func TestConnReliableControlEventClosesOnBackpressure(t *testing.T) {
	c := newConn(nil, uuid.New())
	for i := 0; i < wsSendQueueCapacity; i++ {
		c.send <- []byte("occupied")
	}
	c.enqueue(Event{Type: TypeAgentTurnCompleted, Sequence: 1})

	select {
	case <-c.done:
	case <-time.After(time.Second):
		t.Fatal("slow connection was not closed after reliable enqueue timeout")
	}
	if !c.closed {
		t.Fatal("connection should be marked closed")
	}
}

func TestQueueGenerationStartsFreshTaskForFinishedGeneration(t *testing.T) {
	h := NewHub(nil, nil)
	convID := uuid.New()
	oldDone := make(chan struct{})
	close(oldDone)
	old := &genTask{cancel: func() {}, done: oldDone, finished: true}
	h.mu.Lock()
	h.cancels[convID] = old
	h.mu.Unlock()

	started := make(chan struct{})
	h.queueGeneration(context.Background(), convID, func(context.Context, EmitFunc) {
		close(started)
	})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("fresh generation did not start")
	}
	if err := h.WaitGeneration(context.Background(), convID); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.queued[convID]) != 0 {
		t.Fatalf("finished generation left queued work: %#v", h.queued[convID])
	}
}

func TestQueueGenerationHasBoundedLength(t *testing.T) {
	h := NewHub(nil, nil)
	convID := uuid.New()
	task := &genTask{cancel: func() {}, done: make(chan struct{})}
	h.mu.Lock()
	h.cancels[convID] = task
	h.mu.Unlock()

	for i := 0; i < maxGenerationQueueLength+1; i++ {
		h.queueGeneration(context.Background(), convID, func(context.Context, EmitFunc) {})
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if got := len(h.queued[convID]); got != maxGenerationQueueLength {
		t.Fatalf("queue length = %d, want %d", got, maxGenerationQueueLength)
	}
	task.finished = true
	close(task.done)
}

func TestConcurrentGenerationIsRejectedWithoutCancellingActiveTask(t *testing.T) {
	h := NewHub(nil, nil)
	convID := uuid.New()
	oldStarted := make(chan struct{})
	if !h.startGeneration(context.Background(), convID, func(ctx context.Context, _ EmitFunc) {
		close(oldStarted)
		<-ctx.Done()
	}) {
		t.Fatal("first generation was rejected")
	}
	<-oldStarted

	newRan := make(chan struct{})
	if h.startGeneration(context.Background(), convID, func(context.Context, EmitFunc) {
		close(newRan)
	}) {
		t.Fatal("concurrent generation was accepted")
	}
	select {
	case <-newRan:
		t.Fatal("rejected generation ran")
	case <-time.After(25 * time.Millisecond):
	}
	h.mu.Lock()
	active := h.cancels[convID]
	h.mu.Unlock()
	if active == nil {
		t.Fatal("active generation was cancelled")
	}
	active.cancel()
	if err := h.WaitGeneration(context.Background(), convID); err != nil {
		t.Fatal(err)
	}
}

func TestParseRequestIDRejectsMalformedClientIDs(t *testing.T) {
	if id, ok := parseRequestID(""); !ok || id == uuid.Nil {
		t.Fatalf("empty request id should receive a generated id: %v %v", id, ok)
	}
	want := uuid.New()
	if got, ok := parseRequestID(want.String()); !ok || got != want {
		t.Fatalf("valid request id was not preserved: %v %v", got, ok)
	}
	if got, ok := parseRequestID("not-a-uuid"); ok || got != uuid.Nil {
		t.Fatalf("malformed request id was accepted: %v %v", got, ok)
	}
}
