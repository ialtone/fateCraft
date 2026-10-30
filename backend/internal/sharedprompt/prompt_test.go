package sharedprompt

import (
	"strings"
	"testing"

	"github.com/ialtone/fatecraft/internal/storage"
)

func TestCommonPromptForbidsCornerQuotes(t *testing.T) {
	segments, err := Segments()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, segment := range segments {
		if segment.Key != "shared_prompt" {
			continue
		}
		found = true
		if !strings.Contains(segment.Content, "「」") || !strings.Contains(segment.Content, "**") {
			t.Fatalf("shared prompt should ban corner quotes and require markdown bold: %q", segment.Content)
		}
	}
	if !found {
		t.Fatal("missing shared_prompt segment")
	}
}

func TestEnsureIdempotent(t *testing.T) {
	base := []storage.PromptSegment{{
		Key: "system_master", Role: storage.RoleSystem, Content: "x",
		Enabled: true, Required: true,
	}}
	once, err := Ensure(base)
	if err != nil {
		t.Fatal(err)
	}
	twice, err := Ensure(once)
	if err != nil {
		t.Fatal(err)
	}
	if len(once) != len(twice) {
		t.Fatalf("idempotent length %d vs %d", len(once), len(twice))
	}
	count := 0
	for _, segment := range twice {
		if segment.Key == "shared_prompt" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("shared_prompt count = %d", count)
	}
}
