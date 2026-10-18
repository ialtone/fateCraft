package orchestration

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/storage"
)

const maxNarrativeRunes = 20000

type narrativeViolation struct {
	Code     string `json:"code"`
	Path     string `json:"path,omitempty"`
	Claim    string `json:"claim,omitempty"`
	Expected string `json:"expected,omitempty"`
	Actual   string `json:"actual,omitempty"`
}

// ValidateNarrative is the deterministic L0/L1 gate. It only rejects claims
// that can be mapped to a committed change or a current fact; literary
// passages that cannot be mapped are left alone.
func ValidateNarrative(text string, narrative *gameplay.TurnNarrativeContext) (string, storage.JSONMap) {
	violations := make([]narrativeViolation, 0, 4)
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		violations = append(violations, narrativeViolation{Code: "empty_text"})
	}
	if !utf8.ValidString(text) {
		violations = append(violations, narrativeViolation{Code: "invalid_utf8"})
	}
	if utf8.RuneCountInString(text) > maxNarrativeRunes {
		violations = append(violations, narrativeViolation{Code: "text_too_long"})
	}
	for _, marker := range []string{"<campaign_projection>", "</campaign_projection>", "state_hash", "field_ownership", "narrative_context"} {
		if strings.Contains(text, marker) {
			violations = append(violations, narrativeViolation{Code: "internal_marker", Claim: marker})
		}
	}
	if narrative != nil {
		violations = append(violations, validateNarrativeFacts(text, *narrative)...)
	}
	status := "passed"
	if len(violations) > 0 {
		status = "failed"
	}
	detail := storage.JSONMap{
		"layer": "L0/L1", "violations": violations,
	}
	if narrative != nil {
		detail["revision"] = narrative.RevisionTo
		detail["state_hash"] = narrative.StateHash
	}
	return status, detail
}

func validateNarrativeFacts(text string, narrative gameplay.TurnNarrativeContext) []narrativeViolation {
	changes := map[string]gameplay.FactChange{}
	facts := map[string]any{}
	preserved := map[string]bool{}
	failedBreakthrough := false
	hasInventoryChange := false
	for _, action := range narrative.Actions {
		if action.GoalStatus == "failed" && strings.Contains(action.ActionKey, "breakthrough") {
			failedBreakthrough = true
		}
		for _, change := range action.Changes {
			changes[change.Path] = change
			if strings.Contains(change.Path, "inventory") ||
				strings.Contains(change.Path, "item") ||
				strings.Contains(change.Path, "currency") {
				hasInventoryChange = true
			}
		}
		for key, value := range action.CurrentFacts {
			facts[key] = value
		}
		for _, fact := range action.Preserved {
			preserved[fact.Path] = true
		}
	}
	violations := make([]narrativeViolation, 0)
	add := func(code, path, claim, expected, actual string) {
		violations = append(violations, narrativeViolation{
			Code: code, Path: path, Claim: claim, Expected: expected, Actual: actual,
		})
	}

	if failedBreakthrough {
		pattern := regexp.MustCompile(`突破[^。！？\n]{0,12}成功|成功[^。！？\n]{0,12}突破|晋升成功|破境成功`)
		if match := pattern.FindString(text); match != "" && !negated(text, pattern) {
			add("failed_goal_success", "", match, "行动失败", "正文宣称成功")
		}
	}

	if preserved["character.realm_name"] || preserved["character.stage_name"] {
		pattern := regexp.MustCompile(`突破[^。！？\n]{0,12}成功|成功[^。！？\n]{0,12}突破|晋升成功|破境成功|境界(?:提升|突破|跃升)`)
		if match := pattern.FindString(text); match != "" && !negated(text, pattern) {
			add("realm_changed", "character.realm_name", match, "境界未变化", "正文宣称境界提升")
		}
	}

	if nonEmptyFact(facts["character.injuries"]) {
		pattern := regexp.MustCompile(`伤势?[^。！？\n]{0,8}(痊愈|愈合|全消|尽复|无碍|已经好了)|伤愈`)
		if match := pattern.FindString(text); match != "" && !negated(text, pattern) {
			add("injuries_cleared", "character.injuries", match, "伤势仍存在", "正文宣称伤势痊愈")
		}
	}

	health, hasHealth := factInt(facts["character.health"])
	maxHealth, hasMax := factInt(facts["character.max_health"])
	if hasHealth && hasMax && health < maxHealth {
		pattern := regexp.MustCompile(`气血[^。！？\n]{0,8}(回满|尽复|全满|充盈|恢复如初)|恢复如初`)
		if match := pattern.FindString(text); match != "" && !negated(text, pattern) {
			add("health_not_full", "character.health", match, strconv.FormatInt(health, 10)+"/"+strconv.FormatInt(maxHealth, 10), "正文宣称气血回满")
		}
	}

	if preserved["character.lifespan_days"] {
		pattern := regexp.MustCompile(`寿元[^。！？\n]{0,8}(增加|提升|延长|暴涨|大增)`)
		if match := pattern.FindString(text); match != "" && !negated(text, pattern) {
			add("lifespan_changed", "character.lifespan_days", match, "寿元未变化", "正文宣称寿元增加")
		}
	}

	checkRatioClaim(text, "气血", "character.health", "character.max_health", facts, &violations)
	checkRatioClaim(text, "法力", "character.mana", "character.max_mana", facts, &violations)
	checkSingleClaim(text, "神识", "character.spiritual_sense", "", facts, &violations)
	checkSingleClaim(text, "寿元", "character.lifespan_days", "天", facts, &violations)

	if !hasInventoryChange {
		pattern := regexp.MustCompile(`(?:获得|得到|拾取|收获)[^。！？\n]{0,8}(\d+)\s*(块|枚|株|份|件|瓶|张)`)
		if match := pattern.FindString(text); match != "" && !negated(text, pattern) {
			add("inventory_granted", "inventory", match, "库存未变化", "正文宣称获得物品")
		}
	}
	return violations
}

func checkRatioClaim(text, label, currentPath, maxPath string, facts map[string]any, violations *[]narrativeViolation) {
	current, okCurrent := factInt(facts[currentPath])
	maximum, okMax := factInt(facts[maxPath])
	if !okCurrent || !okMax {
		return
	}
	pattern := regexp.MustCompile(label + `[^\d。！？\n]{0,6}(\d+)\s*/\s*(\d+)`)
	match := pattern.FindStringSubmatch(text)
	if len(match) != 3 || negated(text, pattern) {
		return
	}
	claimedCurrent, errCurrent := strconv.ParseInt(match[1], 10, 64)
	claimedMax, errMax := strconv.ParseInt(match[2], 10, 64)
	if errCurrent != nil || errMax != nil {
		return
	}
	if claimedCurrent != current || claimedMax != maximum {
		*violations = append(*violations, narrativeViolation{
			Code: "numeric_mismatch", Path: currentPath, Claim: match[0],
			Expected: strconv.FormatInt(current, 10) + "/" + strconv.FormatInt(maximum, 10),
			Actual:   match[1] + "/" + match[2],
		})
	}
}

func checkSingleClaim(text, label, path, unit string, facts map[string]any, violations *[]narrativeViolation) {
	current, ok := factInt(facts[path])
	if !ok {
		return
	}
	pattern := regexp.MustCompile(label + `[^\d。！？\n]{0,6}(\d+)\s*` + unit)
	match := pattern.FindStringSubmatch(text)
	if len(match) != 2 || negated(text, pattern) {
		return
	}
	claimed, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		return
	}
	if claimed != current {
		*violations = append(*violations, narrativeViolation{
			Code: "numeric_mismatch", Path: path, Claim: match[0],
			Expected: strconv.FormatInt(current, 10), Actual: match[1],
		})
	}
}

func negated(text string, pattern *regexp.Regexp) bool {
	indexes := pattern.FindAllStringIndex(text, -1)
	for _, index := range indexes {
		start := index[0] - 8
		if start < 0 {
			start = 0
		}
		prefix := text[start:index[0]]
		for _, marker := range []string{"没有", "未", "并非", "不曾", "无法", "没有能", "没能"} {
			if strings.Contains(prefix, marker) {
				return true
			}
		}
	}
	return false
}

func factInt(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int32:
		return int64(typed), true
	case int64:
		return typed, true
	case float64:
		return int64(typed), true
	case json.Number:
		parsed, err := typed.Int64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func nonEmptyFact(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(typed) != ""
	case []any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	default:
		return true
	}
}

func renderNarrativeFallback(narrative *gameplay.TurnNarrativeContext) string {
	if narrative == nil || len(narrative.Actions) == 0 {
		return "本回合没有可确认的状态变化。"
	}
	lines := make([]string, 0, len(narrative.Actions)+1)
	for _, action := range narrative.Actions {
		prefix := "本次行动已结算。"
		switch action.GoalStatus {
		case "failed":
			prefix = "本次行动未能达成目标。"
		case "completed":
			prefix = "本次行动已完成。"
		case "unchanged":
			prefix = "本次行动没有改变关键状态。"
		}
		parts := []string{prefix}
		if action.Summary != "" {
			parts = append(parts, action.Summary)
		}
		for index, change := range action.Changes {
			if index >= 3 {
				break
			}
			label := change.Label
			if label == "" {
				label = gameplay.FactLabel(change.Path)
			}
			parts = append(parts, label+"："+gameplay.FactValueText(change.Before)+" → "+gameplay.FactValueText(change.After))
		}
		lines = append(lines, strings.Join(parts, "；"))
	}
	return strings.Join(lines, "\n")
}

// RepairNarrative removes sentences that directly contain a detected
// contradiction. It is a bounded, deterministic first repair; if nothing safe
// remains the caller falls back to the template.
func RepairNarrative(text string, detail storage.JSONMap) string {
	raw, ok := detail["violations"].([]narrativeViolation)
	if !ok || len(raw) == 0 {
		return ""
	}
	claims := make([]string, 0, len(raw))
	for _, violation := range raw {
		if claim := strings.TrimSpace(violation.Claim); claim != "" {
			claims = append(claims, claim)
		}
	}
	if len(claims) == 0 {
		return ""
	}
	sentences := regexp.MustCompile(`[^。！？\n]+[。！？]?`).FindAllString(text, -1)
	kept := make([]string, 0, len(sentences))
	for _, sentence := range sentences {
		remove := false
		for _, claim := range claims {
			if strings.Contains(sentence, claim) {
				remove = true
				break
			}
		}
		if !remove {
			kept = append(kept, sentence)
		}
	}
	repaired := strings.TrimSpace(strings.Join(kept, ""))
	if repaired == "" {
		return ""
	}
	return repaired
}

func narrativeBindingFromContext(narrative gameplay.TurnNarrativeContext, status string, detail storage.JSONMap) *storage.NarrativeBinding {
	campaignID, err := uuid.Parse(narrative.CampaignID)
	if err != nil {
		return nil
	}
	binding := &storage.NarrativeBinding{
		CampaignID: campaignID, Revision: narrative.RevisionTo,
		StateHash: narrative.StateHash, ValidationStatus: status,
		ValidationDetail: detail,
	}
	for _, action := range narrative.Actions {
		if strings.TrimSpace(action.ActionID) == "" {
			continue
		}
		binding.Actions = append(binding.Actions, storage.NarrativeActionBinding{
			ID: action.ActionID, Revision: action.Revision,
		})
	}
	return binding
}
