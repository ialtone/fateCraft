package llm

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestAsyncRequestAuditWriterQueuesAndCloses(t *testing.T) {
	var mu sync.Mutex
	var got []RequestAudit
	writer := NewAsyncRequestAuditWriter(func(_ context.Context, audit RequestAudit) error {
		mu.Lock()
		got = append(got, audit)
		mu.Unlock()
		return nil
	}, 2)
	input := []byte(`{"messages":[{"role":"user","content":"hello"}]}`)
	if err := writer.Write(context.Background(), RequestAudit{RequestBody: input, OutputTypes: []string{"message"}}); err != nil {
		t.Fatal(err)
	}
	input[0] = 'X'
	if err := writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || string(got[0].RequestBody) != `{"messages":[{"role":"user","content":"hello"}]}` {
		t.Fatalf("audit = %#v", got)
	}
	if len(got[0].OutputTypes) != 1 || got[0].OutputTypes[0] != "message" {
		t.Fatalf("output types = %#v", got[0].OutputTypes)
	}
}

func TestAsyncRequestAuditWriterDoesNotAcceptAfterClose(t *testing.T) {
	writer := NewAsyncRequestAuditWriter(func(context.Context, RequestAudit) error { return nil }, 1)
	if err := writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := writer.Write(context.Background(), RequestAudit{}); err == nil {
		t.Fatal("closed writer accepted an audit")
	}
	select {
	case <-writer.done:
	case <-time.After(time.Second):
		t.Fatal("audit worker did not stop")
	}
}
