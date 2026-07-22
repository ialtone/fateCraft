package contextbuilder

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/llm"
	"github.com/ialtone/fatecraft/internal/storage"
	"github.com/ialtone/fatecraft/internal/tokenizer"
	"github.com/ialtone/fatecraft/internal/worldbook"
)

// ErrRequiredOverflow 表示必选段超出预算，不应发起 LLM 请求（doc 08 §Required 处理）。
var ErrRequiredOverflow = errors.New("必选上下文超出 token 预算，请降低 Prompt/状态大小或调整预留")

// 各段在最终消息中的组装顺序（值越小越靠前）。
const (
	orderSystemMaster = iota
	orderOutputSchema
	orderCharacter
	orderPersona
	orderScenario
	orderPresetSystem
	orderState
	orderRuntimeDirective
	orderSummary
	orderWorldBook
	orderStyle
)

// Input 是构造上下文所需的全部输入（必须只属于一个 conversation）。
type Input struct {
	MaxContextTokens       int
	ReservedResponseTokens int
	Model                  string

	PresetSegments []storage.PromptSegment // 仅启用的分段
	Character      *storage.Character
	Persona        *storage.Persona
	Scenario       *storage.Scenario
	State          map[string]any
	Summary        *storage.MemorySummary
	WorldActivated []worldbook.Activated
	// RuntimeDirective 是服务端在单次重试中注入的必选约束，不写入聊天历史。
	RuntimeDirective string

	// History 为时间正序、未隐藏、且已剔除被摘要覆盖部分之外的内部消息。
	History []storage.ChatMessage
}

// SegmentInfo 记录单个段的明细，用于上下文检查器与构建日志。
type SegmentInfo struct {
	Key        string `json:"key"`
	Role       string `json:"role"`
	Tokens     int    `json:"tokens"`
	Required   bool   `json:"required"`
	Included   bool   `json:"included"`
	BudgetKey  string `json:"budget_key"`
	SkipReason string `json:"skip_reason,omitempty"`
	order      int
	content    string
}

// Result 是一次上下文构造的产物。
type Result struct {
	Messages           []llm.Message
	Segments           []SegmentInfo
	PromptBudget       int
	UsedPromptTokens   int
	IncludedMessageIDs []uuid.UUID
	ActivatedEntryIDs  []string // 世界书条目 id（内容存于挑战版本 JSON，id 为字符串）
}

// Build 在 token 预算内构造 Chat Completion 消息与构建日志。
func Build(in Input, counter tokenizer.Counter) (*Result, error) {
	promptBudget := PromptBudget(in.MaxContextTokens, in.ReservedResponseTokens)
	if promptBudget <= 0 {
		return nil, fmt.Errorf("%w: prompt 预算为 %d", ErrRequiredOverflow, promptBudget)
	}

	var segs []*SegmentInfo
	add := func(key, role, content string, required bool, order int, budgetKey string) *SegmentInfo {
		s := &SegmentInfo{
			Key: key, Role: role, content: content,
			Tokens: counter.Count(content), Required: required,
			order: order, BudgetKey: budgetKey,
		}
		segs = append(segs, s)
		return s
	}

	// --- 构造候选段 ---

	// Prompt 预设分段（含必选的 system_master/output_schema 与可选 style）。
	for _, ps := range in.PresetSegments {
		if !ps.Enabled {
			continue
		}
		order := orderPresetSystem
		switch ps.Key {
		case "system_master":
			order = orderSystemMaster
		case "output_schema":
			order = orderOutputSchema
		case "style_note":
			order = orderStyle
		}
		add(ps.Key, ps.Role, ps.Content, ps.Required, order, "prompt")
	}

	// 角色卡 / Persona / 场景描述。
	if c := in.Character; c != nil {
		if content := renderCharacter(c); content != "" {
			add("character", storage.RoleSystem, content, false, orderCharacter, "persona")
		}
	}
	if p := in.Persona; p != nil {
		if content := renderPersona(p); content != "" {
			add("persona", storage.RoleSystem, content, false, orderPersona, "persona")
		}
	}
	if sc := in.Scenario; sc != nil {
		if content := renderScenario(sc); content != "" {
			add("scenario", storage.RoleSystem, content, false, orderScenario, "persona")
		}
	}

	// 当前结构化状态（必选，注入要短）。
	add("current_state", storage.RoleSystem, renderState(in.State), true, orderState, "state")
	if strings.TrimSpace(in.RuntimeDirective) != "" {
		add("runtime_directive", storage.RoleSystem,
			"<runtime_directive>\n"+in.RuntimeDirective+"\n</runtime_directive>",
			true, orderRuntimeDirective, "runtime")
	}

	// 摘要记忆（可选）。
	if in.Summary != nil && in.Summary.Content != "" {
		add("memory_summary", storage.RoleSystem,
			"<memory_summary>\n"+in.Summary.Content+"\n</memory_summary>", false, orderSummary, "summary")
	}

	// 世界书激活条目（可选），合并为一个 system 段。
	// token 记账直接按拼接后的完整文本计算，包裹标签与标题一并计入。
	if len(in.WorldActivated) > 0 {
		var b strings.Builder
		b.WriteString("<world_info>\n")
		for _, a := range in.WorldActivated {
			b.WriteString("# " + a.Entry.Title + "\n" + a.Entry.Content + "\n\n")
		}
		b.WriteString("</world_info>")
		add("world_info", storage.RoleSystem, b.String(), false, orderWorldBook, "worldbook")
	}

	// --- 预算消费 ---
	// 1) 必选段优先；合计超预算时报告总量，任何一段都无法省略。
	used := 0
	requiredTotal := 0
	for _, s := range segs {
		if s.Required {
			requiredTotal += s.Tokens
		}
	}
	if requiredTotal > promptBudget {
		return nil, fmt.Errorf("%w: 必选段合计需要 %d token，超出预算 %d", ErrRequiredOverflow, requiredTotal, promptBudget)
	}
	for _, s := range segs {
		if !s.Required {
			continue
		}
		s.Included = true
		used += s.Tokens
	}

	// 2) 可选段按消费优先级依次尝试加入。
	for _, s := range optionalByPriority(segs) {
		if used+s.Tokens <= promptBudget {
			s.Included = true
			used += s.Tokens
		} else {
			s.SkipReason = "budget"
		}
	}

	// 3) 倒序填充最近历史（doc 08 §历史填充规则）。
	var includedIDs []uuid.UUID
	var historyMsgs []llm.Message
	visibleHistory := 0
	for i := len(in.History) - 1; i >= 0; i-- {
		m := in.History[i]
		if m.Hidden {
			continue
		}
		visibleHistory++
		t := m.Content
		cost := counter.Count(t)
		if used+cost > promptBudget {
			break // 超预算停止
		}
		used += cost
		historyMsgs = append(historyMsgs, llm.Message{Role: m.Role, Content: m.Content})
		includedIDs = append(includedIDs, m.ID)
	}
	// 有可见历史但一条都放不进时报错，避免在无历史情况下调用模型。
	if visibleHistory > 0 && len(includedIDs) == 0 {
		return nil, fmt.Errorf("%w: 剩余预算无法容纳任何一条历史消息", ErrRequiredOverflow)
	}
	// 恢复时间正序。
	reverseMessages(historyMsgs)
	reverseUUIDs(includedIDs)

	// --- 组装最终消息 ---
	included := make([]*SegmentInfo, 0, len(segs))
	for _, s := range segs {
		if s.Included {
			included = append(included, s)
		}
	}
	sort.SliceStable(included, func(i, j int) bool { return included[i].order < included[j].order })

	messages := make([]llm.Message, 0, len(included)+len(historyMsgs))
	for _, s := range included {
		messages = append(messages, llm.Message{Role: s.Role, Content: s.content})
	}
	messages = append(messages, historyMsgs...)

	// 激活的世界书条目 id。
	var entryIDs []string
	for _, a := range in.WorldActivated {
		entryIDs = append(entryIDs, a.Entry.ID)
	}

	// 输出段明细（不含正文）。
	infos := make([]SegmentInfo, len(segs))
	for i, s := range segs {
		infos[i] = *s
	}

	return &Result{
		Messages:           messages,
		Segments:           infos,
		PromptBudget:       promptBudget,
		UsedPromptTokens:   used,
		IncludedMessageIDs: includedIDs,
		ActivatedEntryIDs:  entryIDs,
	}, nil
}

// optionalByPriority 返回可选段，按消费优先级排序：
// 角色/Persona/场景 > 摘要 > 世界书 > 其它预设可选段（style 等）。
func optionalByPriority(segs []*SegmentInfo) []*SegmentInfo {
	rank := func(s *SegmentInfo) int {
		switch s.BudgetKey {
		case "persona":
			return 0
		case "summary":
			return 1
		case "worldbook":
			return 2
		default:
			return 3
		}
	}
	var out []*SegmentInfo
	for _, s := range segs {
		if !s.Required {
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i]) < rank(out[j]) })
	return out
}

func renderState(state map[string]any) string {
	if state == nil {
		state = map[string]any{}
	}
	b, err := json.Marshal(state)
	if err != nil {
		b = []byte("{}")
	}
	return "<current_state>\n" + string(b) + "\n</current_state>"
}

func renderCharacter(c *storage.Character) string {
	var b strings.Builder
	b.WriteString("<character>\n")
	b.WriteString("名称: " + c.Name + "\n")
	if c.Description != "" {
		b.WriteString("描述: " + c.Description + "\n")
	}
	if c.Personality != "" {
		b.WriteString("性格: " + c.Personality + "\n")
	}
	if c.Scenario != "" {
		b.WriteString("场景: " + c.Scenario + "\n")
	}
	if c.ExampleDialogues != "" {
		b.WriteString("示例对话:\n" + c.ExampleDialogues + "\n")
	}
	if c.SystemPromptOverride != "" {
		b.WriteString(c.SystemPromptOverride + "\n")
	}
	b.WriteString("</character>")
	return b.String()
}

func renderPersona(p *storage.Persona) string {
	var b strings.Builder
	b.WriteString("<persona>\n")
	b.WriteString("名称: " + p.Name + "\n")
	if p.Description != "" {
		b.WriteString("描述: " + p.Description + "\n")
	}
	if p.SpeakingStyle != "" {
		b.WriteString("说话风格: " + p.SpeakingStyle + "\n")
	}
	if p.Constraints != "" {
		b.WriteString("限制: " + p.Constraints + "\n")
	}
	b.WriteString("</persona>")
	return b.String()
}

func renderScenario(s *storage.Scenario) string {
	if s.Description == "" && s.Opening == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("<scenario>\n")
	if s.Description != "" {
		b.WriteString(s.Description + "\n")
	}
	if s.Opening != "" {
		b.WriteString(s.Opening + "\n")
	}
	b.WriteString("</scenario>")
	return b.String()
}

func reverseMessages(s []llm.Message) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

func reverseUUIDs(s []uuid.UUID) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}
