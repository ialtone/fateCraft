package storage

import "testing"

func TestReasoningTextPrefersChatContent(t *testing.T) {
	got := ReasoningText("先检查状态", JSONDoc(`[{"type":"reasoning","content":[{"type":"reasoning_text","text":"不应重复"}]}]`))
	if got != "先检查状态" {
		t.Fatalf("reasoning text = %q", got)
	}
}

func TestReasoningTextExtractsReadableResponsesParts(t *testing.T) {
	items := JSONDoc(`[
        {"type":"reasoning","encrypted_content":"opaque","content":[{"type":"reasoning_text","text":"先检查状态"}]},
        {"type":"reasoning","summary":[{"type":"summary_text","text":"再生成叙事"}]}
    ]`)
	got := ReasoningText("", items)
	if got != "先检查状态\n再生成叙事" {
		t.Fatalf("reasoning text = %q", got)
	}
}

func TestReasoningTextDoesNotExposeEncryptedOnlyItems(t *testing.T) {
	got := ReasoningText("", JSONDoc(`[{"type":"reasoning","encrypted_content":"opaque"}]`))
	if got != "" {
		t.Fatalf("reasoning text = %q", got)
	}
}

func TestReasoningTextBlocksProjectsReadableReplay(t *testing.T) {
	blocks := JSONDoc(`[{"kind":"thinking","replay":{"type":"reasoning","content":[{"type":"reasoning_text","text":"先观察"}]}},{"kind":"text","text":"叙事"}]`)
	if got := ReasoningTextBlocks(blocks); got != "先观察" {
		t.Fatalf("ReasoningTextBlocks() = %q, want readable replay text", got)
	}
}
