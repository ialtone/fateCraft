package realtime

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

func TestSubscriptionReadyFollowsRegistration(t *testing.T) {
	hub := NewHub(nil, nil)
	id := uuid.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _ = hub.Serve(w, r, id) }))
	defer server.Close()
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	_ = ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	var event Event
	if err := ws.ReadJSON(&event); err != nil {
		t.Fatal(err)
	}
	if event.Type != TypeSubscriptionReady || !hub.HasSubscribers(id) {
		t.Fatalf("ready without subscription: %+v", event)
	}
	hub.Broadcast(id, Event{Type: TypeActivityUpdated, ActivityVersion: 2})
	if err := ws.ReadJSON(&event); err != nil || event.ActivityVersion != 2 {
		t.Fatalf("subscribed event: %+v %v", event, err)
	}
	hub.SetActivityAvailable(false)
	if err := ws.ReadJSON(&event); err == nil {
		t.Fatal("notification gap did not close connection")
	}
}

