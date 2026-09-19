package share

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/storage"
)

func TestProjectMessagesDoesNotExposePrivateFields(t *testing.T) {
	messages := []storage.DisplayMessage{
		{
			ID: uuid.New(), ConversationID: uuid.New(), Type: "roll_result", Markdown: "result", Reasoning: "private",
			CreatedAt: time.Now(), Metadata: storage.JSONMap{
				"details_version": 1, "secret": "do-not-share", "tool_call_id": "private",
				"roll_key": "trial", "roll_title": "Trial", "roll_category": "story",
				"roll_outcome": map[string]any{
					"rolled": 50, "sides": 100, "success_rate": 60, "success": true, "level": "success",
					"breakdown":     map[string]any{"base": 1, "attributes": 2, "traits": 3, "context": 4, "reward_cap": 5, "history": 6, "difficulty_mode": 7, "final": 8},
					"success_range": map[string]any{"range_start": 1, "range_end": 60},
				},
				"conditions": []any{
					map[string]any{"label": "Visible", "met": true, "public": true, "internal": "no"},
					map[string]any{"label": "Hidden", "met": true, "public": false},
				},
			},
		},
	}
	projected, err := projectMessages(messages)
	if err != nil {
		t.Fatal(err)
	}
	metadata := projected[0].Metadata
	if _, exists := metadata["secret"]; exists {
		t.Fatal("secret metadata was exposed")
	}
	if _, exists := metadata["tool_call_id"]; exists {
		t.Fatal("tool call id was exposed")
	}
	conditions, ok := metadata["conditions"].([]any)
	if !ok || len(conditions) != 1 {
		t.Fatalf("conditions = %#v", metadata["conditions"])
	}
}

func BenchmarkProjectSnapshot(b *testing.B) {
	for _, count := range []int{50, 200, 500} {
		b.Run(fmt.Sprintf("messages_%d", count), func(b *testing.B) {
			messages := make([]storage.DisplayMessage, count)
			for i := range messages {
				messages[i] = storage.DisplayMessage{
					Type: "assistant", Markdown: strings.Repeat("一段公开故事内容。", 24), CreatedAt: time.Unix(int64(i), 0),
				}
			}
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				projected, err := projectMessages(messages)
				if err != nil {
					b.Fatal(err)
				}
				if _, err := json.Marshal(Snapshot{SchemaVersion: SnapshotVersion, Messages: projected}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestSnapshotSerializationContainsNoInternalIDsOrReasoning(t *testing.T) {
	snapshot := Snapshot{
		SchemaVersion: SnapshotVersion,
		Messages:      []MessageSnapshot{{Position: 1, Type: "assistant", Markdown: "visible", CreatedAt: time.Now()}},
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, forbidden := range []string{"conversation_id", "chat_message_id", "attempt_id", "reasoning", "tool_call"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("snapshot contains %q: %s", forbidden, text)
		}
	}
}

func TestProjectMessagesRejectsUnknownTypes(t *testing.T) {
	_, err := projectMessages([]storage.DisplayMessage{{Type: "future_internal"}})
	if err == nil {
		t.Fatal("unknown type was accepted")
	}
}

func TestNormalizeCreateInputRequiresContinuousRangeEndpoints(t *testing.T) {
	start := uuid.New()
	valid := []CreateInput{
		{Scope: "all", RequestID: uuid.New()},
		{Scope: "range", StartMessageID: &start, EndMessageID: &start, RequestID: uuid.New()},
	}
	for _, input := range valid {
		if _, _, err := normalizeCreateInput(input, "Test User", ""); err != nil {
			t.Fatalf("valid input rejected: %v", err)
		}
	}
	invalid := []CreateInput{
		{Scope: "all", StartMessageID: &start, RequestID: uuid.New()},
		{Scope: "range", StartMessageID: &start, RequestID: uuid.New()},
		{Scope: "list", RequestID: uuid.New()},
	}
	for _, input := range invalid {
		if _, _, err := normalizeCreateInput(input, "Test User", ""); err == nil {
			t.Fatalf("invalid input accepted: %#v", input)
		}
	}
}

func TestNormalizeCreateInputControlsAttributionFromAuthenticatedUser(t *testing.T) {
	anonymous, _, err := normalizeCreateInput(CreateInput{Scope: "all", RequestID: uuid.New()}, "Owner", "https://cdn.ldstatic.com/avatar.png")
	if err != nil || anonymous.PublicAttribution != AnonymousAttribution {
		t.Fatalf("anonymous attribution = %q, err = %v", anonymous.PublicAttribution, err)
	}
	if anonymous.PublicAvatarURL != "" {
		t.Fatalf("anonymous avatar URL = %q", anonymous.PublicAvatarURL)
	}
	public, _, err := normalizeCreateInput(CreateInput{Scope: "all", ShowUsername: true, RequestID: uuid.New()}, " Owner ", "https://cdn.ldstatic.com/avatar.png")
	if err != nil || public.PublicAttribution != "Owner" {
		t.Fatalf("public attribution = %q, err = %v", public.PublicAttribution, err)
	}
	if public.PublicAvatarURL != "https://cdn.ldstatic.com/avatar.png" {
		t.Fatalf("public avatar URL = %q", public.PublicAvatarURL)
	}
}

func TestNormalizeAvatarURLAllowsOnlyTrustedLinuxDOURLs(t *testing.T) {
	for _, value := range []string{"https://cdn.ldstatic.com/avatar.png", "https://linux.do/avatar.png", "https://cdn.linux.do/avatar.png"} {
		if got := normalizeAvatarURL(value); got != value {
			t.Fatalf("normalizeAvatarURL(%q) = %q", value, got)
		}
	}
	for _, value := range []string{"http://cdn.ldstatic.com/avatar.png", "https://example.com/avatar.png", "https://linux.do.evil.example/avatar.png", "javascript:alert(1)", "data:image/svg+xml,bad", "/avatar.png"} {
		if got := normalizeAvatarURL(value); got != "" {
			t.Fatalf("normalizeAvatarURL(%q) = %q, want empty", value, got)
		}
	}
}

func TestReplaceRequestHashBindsOldShare(t *testing.T) {
	conversationID := uuid.New()
	input := CreateInput{Scope: "all", RequestID: uuid.New()}
	createHash := hashRequest(conversationID, input)
	first := hashReplaceRequest(uuid.New(), conversationID, input)
	second := hashReplaceRequest(uuid.New(), conversationID, input)
	if createHash == first {
		t.Fatal("replace request collided with create request")
	}
	if first == second {
		t.Fatal("replace request hash did not bind the old share")
	}
}
