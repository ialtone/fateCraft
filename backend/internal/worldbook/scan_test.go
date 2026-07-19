package worldbook

import (
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/storage"
)

type runeCounter struct{}

func (runeCounter) Count(s string) int { return utf8.RuneCountInString(s) }

func entry(title string, kws []string, constant bool, priority int) storage.WorldBookEntry {
	return storage.WorldBookEntry{
		ID: uuid.New().String(), Title: title, Content: "c", Keywords: kws,
		Constant: constant, Enabled: true, Priority: priority,
	}
}

// 常驻条目始终激活；关键词条目命中才激活；常驻优先、其次按 priority 升序。
func TestScan_ActivationAndOrder(t *testing.T) {
	c := entry("常驻", nil, true, 100)
	k1 := entry("命中", []string{"dragon"}, false, 50)
	k2 := entry("未命中", []string{"nothere"}, false, 10)

	activated, _ := Scan([]storage.WorldBookEntry{k2, k1, c}, "I saw a DRAGON", 10000, runeCounter{})

	if len(activated) != 2 {
		t.Fatalf("期望激活 2 条，得到 %d", len(activated))
	}
	if !activated[0].Entry.Constant {
		t.Errorf("第一条应为常驻条目，得到 %q", activated[0].Entry.Title)
	}
	if activated[1].Entry.Title != "命中" {
		t.Errorf("第二条应为命中关键词条目，得到 %q", activated[1].Entry.Title)
	}
}

// 预算不足时按优先级（常驻优先）取舍，其余跳过。
func TestScan_BudgetSkip(t *testing.T) {
	c := entry("常驻", nil, true, 100) // tokens = len("常驻\nc")
	k1 := entry("命中", []string{"dragon"}, false, 50)

	// 预算仅够一条。
	budget := runeCounter{}.Count(c.Title + "\n" + c.Content)
	activated, skipped := Scan([]storage.WorldBookEntry{c, k1}, "dragon", budget, runeCounter{})

	if len(activated) != 1 || !activated[0].Entry.Constant {
		t.Fatalf("预算紧张时应优先保留常驻条目，activated=%v", activated)
	}
	if len(skipped) != 1 {
		t.Fatalf("期望跳过 1 条，得到 %d", len(skipped))
	}
}
