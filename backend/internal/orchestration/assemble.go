package orchestration

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/contextbuilder"
	"github.com/ialtone/fatecraft/internal/storage"
	"github.com/ialtone/fatecraft/internal/worldbook"
)

// assembleInput 收集构造上下文所需的全部数据（仅限当前 conversation）。
func (g *Generator) assembleInput(ctx context.Context, conv *storage.Conversation, settings *storage.LLMSettings, runtimeDirective string) (contextbuilder.Input, error) {
	in := contextbuilder.Input{
		MaxContextTokens:       settings.MaxContextTokens,
		ReservedResponseTokens: settings.MaxResponseTokens,
		Model:                  settings.Model,
		RuntimeDirective:       runtimeDirective,
	}

	bundle, err := g.challenge.Bundle(ctx, conv)
	if err != nil {
		return in, err
	}

	// Prompt、角色、Persona、场景和世界书都来自对话固定的挑战版本。
	segs := append([]storage.PromptSegment(nil), bundle.Prompt.Segments...)
	sort.SliceStable(segs, func(i, j int) bool { return segs[i].Position < segs[j].Position })
	in.PresetSegments = segs
	in.Character = bundle.Character
	in.Persona = bundle.Persona
	in.Scenario = bundle.Scenario

	st, err := g.stateSvc.Get(ctx, conv.ID)
	if err != nil {
		return in, err
	}
	in.State = st

	summary, _ := g.memSvc.Latest(ctx, conv.ID)
	in.Summary = summary

	history, err := g.chatSvc.History(ctx, conv.ID, false)
	if err != nil {
		return in, err
	}
	history = filterCovered(history, summary)
	in.History = history

	// 世界书激活。
	if len(bundle.WorldBooks) > 0 {
		scanText := buildScanText(history, st, summary)
		worldBudget := contextbuilder.WorldBudget(contextbuilder.PromptBudget(settings.MaxContextTokens, settings.MaxResponseTokens))
		var entries []storage.WorldBookEntry
		for _, book := range bundle.WorldBooks {
			entries = append(entries, book.Entries...)
		}
		activated, _ := worldbook.Scan(entries, scanText, worldBudget, g.counter)
		in.WorldActivated = activated
	}

	return in, nil
}

// filterCovered 剔除被最新摘要覆盖（含）之前的历史消息。
func filterCovered(history []storage.ChatMessage, summary *storage.MemorySummary) []storage.ChatMessage {
	if summary == nil || summary.CoveredUntilMessageID == nil {
		return history
	}
	for i, m := range history {
		if m.ID == *summary.CoveredUntilMessageID {
			return history[i+1:]
		}
	}
	return history
}

// buildScanText 构造世界书扫描文本：最近若干历史 + 状态 + 摘要。
func buildScanText(history []storage.ChatMessage, st map[string]any, summary *storage.MemorySummary) string {
	var b strings.Builder
	const recent = 6
	from := len(history) - recent
	if from < 0 {
		from = 0
	}
	for _, m := range history[from:] {
		b.WriteString(m.Content)
		b.WriteByte('\n')
	}
	for _, v := range st {
		b.WriteString(flatten(v))
		b.WriteByte('\n')
	}
	if summary != nil {
		b.WriteString(summary.Content)
	}
	return b.String()
}

// flatten 把任意状态值粗略转成可供关键词扫描的文本。
func flatten(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case map[string]any:
		var b strings.Builder
		for _, vv := range t {
			b.WriteString(flatten(vv))
			b.WriteByte(' ')
		}
		return b.String()
	case []any:
		var b strings.Builder
		for _, vv := range t {
			b.WriteString(flatten(vv))
			b.WriteByte(' ')
		}
		return b.String()
	default:
		return ""
	}
}

func uuidsToStrings(ids []uuid.UUID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}

// toJSONDoc 将段明细序列化为 jsonb 可存储的 JSONDoc。
func toJSONDoc(v any) storage.JSONDoc {
	b, err := json.Marshal(v)
	if err != nil {
		return storage.JSONDoc("[]")
	}
	return storage.JSONDoc(b)
}
