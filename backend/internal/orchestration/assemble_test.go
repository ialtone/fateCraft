package orchestration

import (
	"testing"

	"github.com/ialtone/fatecraft/internal/storage"
)

func TestLatestUserContentSelectsCurrentUserInput(t *testing.T) {
	history := []storage.ChatMessage{
		{Role: storage.RoleUser, Content: "旧问题"},
		{Role: storage.RoleAssistant, Content: "旧回答"},
		{Role: storage.RoleUser, Content: "顾药师现在在哪"},
	}
	if got := latestUserContent(history); got != "顾药师现在在哪" {
		t.Fatalf("latest user content = %q", got)
	}
}
