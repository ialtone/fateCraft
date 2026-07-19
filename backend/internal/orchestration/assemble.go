package orchestration

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/contextbuilder"
	"github.com/ialtone/fatecraft/internal/storage"
)

// 当 Prompt 预设缺少核心分段时使用的内置兜底内容，保证 envelope 指令始终存在。
const (
	defaultSystemMaster = "你是一个沉浸式文字冒险/角色扮演叙事引擎。根据玩家输入推进剧情，保持人物性格一致。"
	defaultOutputSchema = `你必须只输出一个 JSON 对象，不要包含任何额外文字或代码块标记。格式：{"narrative":"剧情文本(Markdown)","state_update":{},"notes":"可选"}。state_update 键使用点号路径，数组追加用键名加 + 后缀；仅允许更新 profile.* / attributes.* / traits / inventory.* / relationships.* / location.* / quests / risks / world_flags.*。`
)

// assembleInput 收集构造上下文所需的全部数据（仅限当前 conversation）。
func (g *Generator) assembleInput(ctx context.Context, conv *storage.Conversation, settings *storage.LLMSettings) (contextbuilder.Input, error) {
	in := contextbuilder.Input{
		MaxContextTokens:       settings.MaxContextTokens,
		ReservedResponseTokens: settings.MaxResponseTokens,
		Model:                  settings.Model,
	}

	// Prompt 预设分段（按 position 排序，保证核心分段存在）。
	var segs []storage.PromptSegment
	if conv.PromptPresetID != nil {
		if preset, err := g.promptStore.Get(*conv.PromptPresetID); err == nil {
			segs = append([]storage.PromptSegment(nil), preset.Segments...)
			sort.SliceStable(segs, func(i, j int) bool { return segs[i].Position < segs[j].Position })
		}
	}
	in.PresetSegments = ensureCoreSegments(segs)

	if conv.CharacterID != nil {
		if c, err := g.charStore.Get(*conv.CharacterID); err == nil {
			in.Character = &c
		}
	}
	if conv.PersonaID != nil {
		if p, err := g.personaStore.Get(*conv.PersonaID); err == nil {
			in.Persona = &p
		}
	}
	if conv.ScenarioID != nil {
		if s, err := g.scenStore.Get(*conv.ScenarioID); err == nil {
			in.Scenario = &s
		}
	}

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
	bookIDs, _ := g.convSvc.WorldBookIDs(ctx, conv.ID)
	if len(bookIDs) > 0 {
		scanText := buildScanText(history, st, summary)
		worldBudget := contextbuilder.WorldBudget(contextbuilder.PromptBudget(settings.MaxContextTokens, settings.MaxResponseTokens))
		activated, _ := g.worldSvc.Activate(bookIDs, scanText, worldBudget)
		in.WorldActivated = activated
	}

	return in, nil
}

// ensureCoreSegments 在缺失时补齐 system_master 与 output_schema 兜底分段。
func ensureCoreSegments(segs []storage.PromptSegment) []storage.PromptSegment {
	hasMaster, hasSchema := false, false
	for _, s := range segs {
		if s.Key == "system_master" {
			hasMaster = true
		}
		if s.Key == "output_schema" {
			hasSchema = true
		}
	}
	var prepend []storage.PromptSegment
	if !hasMaster {
		prepend = append(prepend, storage.PromptSegment{
			Key: "system_master", Role: storage.RoleSystem, Content: defaultSystemMaster,
			Enabled: true, Required: true, Priority: 10,
		})
	}
	if !hasSchema {
		prepend = append(prepend, storage.PromptSegment{
			Key: "output_schema", Role: storage.RoleSystem, Content: defaultOutputSchema,
			Enabled: true, Required: true, Priority: 20,
		})
	}
	return append(prepend, segs...)
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
