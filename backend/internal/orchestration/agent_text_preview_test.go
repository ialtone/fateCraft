package orchestration

import (
	"reflect"
	"testing"
)

func TestAgentTextPreviewStreamsUntilToolAndResetsOnce(t *testing.T) {
	preview := newAgentTextPreview()
	var deltas []string
	resetCount := 0
	preview.append("先", true, func(delta string) { deltas = append(deltas, delta) })
	preview.append("行", true, func(delta string) { deltas = append(deltas, delta) })
	preview.markTool(func() { resetCount++ })
	preview.markTool(func() { resetCount++ })
	preview.append("中间", true, func(delta string) { deltas = append(deltas, delta) })

	if got, want := deltas, []string{"先", "行"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("deltas = %#v, want %#v", got, want)
	}
	if resetCount != 1 {
		t.Fatalf("reset count = %d, want 1", resetCount)
	}
	if got, want := preview.text(), "先行"; got != want {
		t.Fatalf("preview text = %q, want %q", got, want)
	}
}

func TestAgentTextPreviewDoesNotEmitWhenStreamingDisabled(t *testing.T) {
	preview := newAgentTextPreview()
	emitted := false
	preview.append("完整正文", false, func(string) { emitted = true })
	if emitted || preview.sent() {
		t.Fatalf("non-streaming preview emitted=%v sent=%v", emitted, preview.sent())
	}
	if preview.text() != "完整正文" {
		t.Fatalf("preview text = %q", preview.text())
	}
}
