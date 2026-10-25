package orchestration

import (
	"encoding/json"
	"regexp"
	"sort"
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

// ClaimDomain is the narrowest set of committed facts that can back one class
// of narrative claim. A committed action only vouches for the domains it
// actually wrote; "travel" cannot prove an item was gained or a relationship
// improved.
type ClaimDomain string

const (
	ClaimRealm         ClaimDomain = "realm"
	ClaimHealth        ClaimDomain = "health"
	ClaimInventory     ClaimDomain = "inventory"
	ClaimRelationship  ClaimDomain = "relationship"
	ClaimQuestAccept   ClaimDomain = "quest_accept"
	ClaimQuestComplete ClaimDomain = "quest_complete"
	ClaimFaction       ClaimDomain = "faction"
)

// TurnEvidence is the trusted, per-turn set of committed facts. It is built
// from ActionResult changes and committed state-tool paths, never from model
// prose or display projections.
type TurnEvidence struct {
	Domains map[ClaimDomain]bool
	Paths   map[string]gameplay.FactChange
	// Entities maps a domain to the entity names seen in committed change
	// paths (for example the NPC name inside relationships["老邢"].trust).
	Entities   map[ClaimDomain]map[string]bool
	Actions    []gameplay.ActionResult
	StatePaths []string
	// UnknownMutation marks a turn that committed a tool without exposing
	// inspectable facts (for example a roll). Such a turn neither grants nor
	// denies domains.
	UnknownMutation bool
}

func (e TurnEvidence) enforce() bool {
	return len(e.Actions) > 0 || len(e.Paths) > 0 || len(e.StatePaths) > 0 || !e.UnknownMutation
}

func (e TurnEvidence) hasDomain(domains []ClaimDomain) bool {
	for _, domain := range domains {
		if e.Domains[domain] {
			return true
		}
	}
	return false
}

// entityMatches reports whether the claim names one of the entities that the
// committed changes touched. When no entity name was observed the check is
// skipped rather than guessed.
func (e TurnEvidence) entityMatches(domains []ClaimDomain, claim string) bool {
	checked := false
	for _, domain := range domains {
		names := e.Entities[domain]
		if len(names) == 0 {
			continue
		}
		checked = true
		for name := range names {
			if name != "" && strings.Contains(claim, name) {
				return true
			}
		}
	}
	return !checked
}

// BuildTurnEvidence collects domains, changed paths and entity names from the
// actions that were committed during this turn. statePaths are the explicit
// field paths written by challenge state tools.
func BuildTurnEvidence(narrative *gameplay.TurnNarrativeContext, extra []gameplay.ActionResult, statePaths []string, unknownMutation bool) TurnEvidence {
	evidence := TurnEvidence{
		Domains: map[ClaimDomain]bool{}, Paths: map[string]gameplay.FactChange{},
		Entities: map[ClaimDomain]map[string]bool{}, StatePaths: statePaths,
		UnknownMutation: unknownMutation,
	}
	seen := map[string]bool{}
	add := func(action gameplay.ActionResult) {
		if action.ActionID != "" && seen[action.ActionID] {
			return
		}
		if action.ActionID != "" {
			seen[action.ActionID] = true
		}
		evidence.Actions = append(evidence.Actions, action)
		for _, domain := range claimDomainsForKey(action.ActionKey) {
			evidence.domains(domain)
		}
		for _, change := range action.Changes {
			evidence.addPath(change)
		}
	}
	if narrative != nil {
		for _, action := range narrative.Actions {
			add(action)
		}
	}
	for _, action := range extra {
		add(action)
	}
	for _, path := range statePaths {
		evidence.addPath(gameplay.FactChange{Path: path})
	}
	return evidence
}

func (e *TurnEvidence) domains(domains ...ClaimDomain) {
	for _, domain := range domains {
		e.Domains[domain] = true
	}
}

func (e *TurnEvidence) addPath(change gameplay.FactChange) {
	path := strings.ToLower(change.Path)
	if path == "" {
		return
	}
	e.Paths[change.Path] = change
	domains := claimDomainsForPath(path, change.After)
	for _, domain := range domains {
		e.domains(domain)
	}
	for _, name := range pathEntityNames(change.Path) {
		for _, domain := range domains {
			if e.Entities[domain] == nil {
				e.Entities[domain] = map[string]bool{}
			}
			e.Entities[domain][name] = true
		}
	}
}

func claimDomainsForKey(key string) []ClaimDomain {
	switch {
	case key == "cultivation.attempt_breakthrough" || key == "cultivation.begin_ascension":
		return []ClaimDomain{ClaimRealm}
	case key == "character.recover", key == "character.apply_condition",
		key == "character.resolve_condition", key == "character.adjust_vitals":
		return []ClaimDomain{ClaimHealth}
	case key == "inventory.use_item":
		return []ClaimDomain{ClaimHealth, ClaimInventory}
	case key == "cultivation.retreat":
		return []ClaimDomain{ClaimInventory}
	case key == "quest.accept":
		return []ClaimDomain{ClaimQuestAccept}
	case key == "quest.submit":
		return []ClaimDomain{ClaimQuestComplete}
	}
	switch {
	case strings.HasPrefix(key, "inventory."), strings.HasPrefix(key, "craft."),
		key == "trade.buy", key == "trade.sell", key == "world.gather":
		return []ClaimDomain{ClaimInventory}
	case strings.HasPrefix(key, "relationship."):
		return []ClaimDomain{ClaimRelationship}
	case strings.HasPrefix(key, "faction."):
		return []ClaimDomain{ClaimFaction}
	}
	return nil
}

func claimDomainsForPath(path string, after any) []ClaimDomain {
	out := make([]ClaimDomain, 0, 2)
	if strings.Contains(path, "realm") {
		out = append(out, ClaimRealm)
	}
	if strings.Contains(path, "health") || strings.Contains(path, "injur") {
		out = append(out, ClaimHealth)
	}
	if strings.Contains(path, "inventory") || strings.Contains(path, "currency") {
		out = append(out, ClaimInventory)
	}
	if strings.Contains(path, "relationship") {
		out = append(out, ClaimRelationship)
	}
	if strings.Contains(path, "quest") || strings.Contains(path, "obligation") {
		status, _ := after.(string)
		if status == "completed" || status == "resolved" {
			out = append(out, ClaimQuestComplete)
		} else {
			out = append(out, ClaimQuestAccept)
		}
	}
	if strings.Contains(path, "faction") {
		out = append(out, ClaimFaction)
	}
	return out
}

var pathEntityPattern = regexp.MustCompile(`\["([^"]+)"\]`)

func pathEntityNames(path string) []string {
	matches := pathEntityPattern.FindAllStringSubmatch(path, -1)
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) == 2 && match[1] != "" {
			out = append(out, match[1])
		}
	}
	return out
}

// ValidateTurnNarrative extends ValidateNarrative with the "unbacked claim"
// gate: a narrative may only claim a change in a domain that this turn's
// committed facts actually touched. Claims without matching evidence are
// reported so the RepairNarrative / fallback flow applies.
// 挑战模式额外启用“时间推进主张”闸门：没有可信动作的回合，正文不得让日历
// 前进（数字日/年、季节转换、岁月套语等）。guards 为玩法作者追加的受控主张。
func ValidateTurnNarrative(text string, narrative *gameplay.TurnNarrativeContext, evidence TurnEvidence, guards []gameplay.NarrativeGuard) (string, storage.JSONMap) {
	status, detail := ValidateNarrative(text, narrative)
	extra := guardClaims(text, guards)
	if evidence.enforce() {
		extra = append(extra, unbackedStateClaims(text, evidence)...)
		if narrative != nil && narrative.ChallengeMode && len(narrative.Actions) == 0 && !evidence.UnknownMutation {
			extra = append(extra, unbackedTimeClaims(text)...)
		}
	}
	if len(extra) == 0 {
		return status, detail
	}
	violations, _ := detail["violations"].([]narrativeViolation)
	detail["violations"] = append(violations, extra...)
	detail["evidence_domains"] = evidence.domainNames()
	detail["missing_domains"] = missingDomains(extra)
	return "failed", detail
}

func (e TurnEvidence) domainNames() []string {
	out := make([]string, 0, len(e.Domains))
	for domain := range e.Domains {
		out = append(out, string(domain))
	}
	sort.Strings(out)
	return out
}

func missingDomains(violations []narrativeViolation) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(violations))
	for _, violation := range violations {
		for _, rule := range unbackedClaimPatterns {
			if rule.code != violation.Code {
				continue
			}
			for _, domain := range rule.domains {
				key := string(domain)
				if !seen[key] {
					seen[key] = true
					out = append(out, key)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

var unbackedClaimPatterns = []struct {
	code     string
	path     string
	domains  []ClaimDomain
	entities bool
	pattern  *regexp.Regexp
}{
	{"unbacked_realm_change", "character.realm_name", []ClaimDomain{ClaimRealm}, false, regexp.MustCompile(`突破[^。！？\n]{0,12}成功|成功[^。！？\n]{0,12}突破|晋升成功|破境成功|境界(?:提升|突破|跃升)|一举突破`)},
	{"unbacked_healing", "character.health", []ClaimDomain{ClaimHealth}, false, regexp.MustCompile(`伤势?[^。！？\n]{0,8}(痊愈|愈合|全消|尽复)|伤愈|气血[^。！？\n]{0,8}(回满|尽复|全满)|恢复如初`)},
	{"unbacked_item_gain", "inventory", []ClaimDomain{ClaimInventory}, false, regexp.MustCompile(`(?:获得|得到|拾取|收获|到手|赚得|赚到|赚了)[^。！？\n]{0,8}?(?:\d+|[一二两三四五六七八九十百千万几数]+)\s*(?:块|枚|株|份|件|瓶|张|颗|粒|两|贯|文|个|把|本)`)},
	{"unbacked_currency_gain", "currency", []ClaimDomain{ClaimInventory}, false, regexp.MustCompile(`(?:灵石|银两|银子|金币|铜钱|钱财)[^。！？\n]{0,4}(?:增加|大增|入账|翻倍)`)},
	{"unbacked_item_loss", "inventory", []ClaimDomain{ClaimInventory}, false, regexp.MustCompile(`(?:用掉|消耗|花掉|失去|交出|耗尽)[^。！？\n]{0,8}(?:\d+|[一二两三四五六七八九十百千万几数]+)\s*(?:块|枚|株|份|件|瓶|张|颗|粒|两|贯|文|个|把|本)`)},
	{"unbacked_relationship_gain", "relationships", []ClaimDomain{ClaimRelationship}, true, regexp.MustCompile(`(?:好感|信任)[^。！？\n]{0,6}(?:提升|增加|上涨|加深|增强)|(?:更|更加|越发)信任`)},
	{"unbacked_quest_accept", "quests", []ClaimDomain{ClaimQuestAccept}, false, regexp.MustCompile(`(?:接下|接受|领取)[^。！？\n]{0,8}(?:任务|委托|承诺)`)},
	{"unbacked_quest_complete", "quests", []ClaimDomain{ClaimQuestComplete}, false, regexp.MustCompile(`(?:任务|委托|承诺)[^。！？\n]{0,4}(?:完成|结束|交付)|(?:完成|交付|提交)[^。！？\n]{0,4}(?:任务|委托|承诺)`)},
	{"unbacked_faction_change", "factions", []ClaimDomain{ClaimFaction}, false, regexp.MustCompile(`(?:声望|名望)[^。！？\n]{0,4}(?:提升|增加|上涨)|(?:成为|晋升为)[^。！？\n]{0,8}(?:弟子|执事|长老|成员)`)},
}

func unbackedStateClaims(text string, evidence TurnEvidence) []narrativeViolation {
	out := make([]narrativeViolation, 0)
	for _, rule := range unbackedClaimPatterns {
		match := firstAffirmedClaim(text, rule.pattern)
		if match == "" {
			continue
		}
		if evidence.hasDomain(rule.domains) &&
			(!rule.entities || evidence.entityMatches(rule.domains, text)) {
			continue
		}
		out = append(out, narrativeViolation{
			Code: rule.code, Path: rule.path, Claim: match,
			Expected: "本回合该领域无已提交变化", Actual: "正文宣称状态变化",
		})
	}
	return out
}

// unbackedTimePatterns 匹配“日历前进”类主张，首发保守：数字天数/年数、
// “数日/数月”类量词、“X 个月后/年后”与季节、岁月套语。模式表刻意不覆盖
// 无后缀的“N 年”（避免误伤“他还有五十九年可走”这类寿元陈述）。
var unbackedTimePatterns = []*regexp.Regexp{
	regexp.MustCompile(`\d+\s*[日天]`),
	regexp.MustCompile(`(?:第|初|过)?[一二两三四五六七八九十百千]+\s*[日天]`),
	regexp.MustCompile(`数\s*[日天月年]`),
	regexp.MustCompile(`[0-9一二两三四五六七八九十百千]+\s*个?\s*[月年]\s*(?:之?后)`),
	regexp.MustCompile(`入冬|开春|入春|入夏|入秋|翌年|次年|转年|春去秋来|寒来暑往|光阴似箭|日月如梭|岁月如流`),
}

// timeClaimExempt 允许正文复述面板上的剩余寿元/已过天数（“剩余寿元 21535 天”
// 是现状陈述而非时间推进）。
func timeClaimExempt(text string, index []int) bool {
	prefix := claimPrefix(text, index[0], 8)
	for _, marker := range []string{"剩余", "寿元", "尚有", "还有", "只余", "仅剩", "已过天数",
		"如果", "假如", "倘若", "计划", "打算", "准备", "需要", "还需", "曾经", "此前", "从前", "回想", "回忆", "每隔"} {
		if strings.Contains(prefix, marker) {
			return true
		}
	}
	// A request to return later describes a plan, not an elapsed interval.
	return plannedTimeSuffix.MatchString(text[index[1]:])
}

var plannedTimeSuffix = regexp.MustCompile(`^\s*(?:之?后|以后)[^，。！？\n]{0,8}(?:再|请|才会|才能|可以)`)

func unbackedTimeClaims(text string) []narrativeViolation {
	out := make([]narrativeViolation, 0)
	seen := map[string]bool{}
	for _, pattern := range unbackedTimePatterns {
		for _, index := range pattern.FindAllStringIndex(text, -1) {
			match := text[index[0]:index[1]]
			if seen[match] || negatedAt(text, index[0]) || timeClaimExempt(text, index) {
				continue
			}
			seen[match] = true
			out = append(out, narrativeViolation{
				Code: "unbacked_time_passage", Claim: match,
				Expected: "本回合无已提交动作，游戏时间未推进", Actual: "正文叙述时间流逝",
			})
		}
	}
	return out
}

// guardClaims 应用玩法作者追加的受控主张守则。
func guardClaims(text string, guards []gameplay.NarrativeGuard) []narrativeViolation {
	out := make([]narrativeViolation, 0)
	for _, guard := range guards {
		pattern, err := regexp.Compile(guard.Pattern)
		if err != nil {
			continue // ValidateContract 已拒绝非法 pattern；此处防御性跳过
		}
		for _, index := range pattern.FindAllStringIndex(text, -1) {
			if negatedAt(text, index[0]) {
				continue
			}
			out = append(out, narrativeViolation{
				Code: "narrative_guard", Claim: text[index[0]:index[1]],
				Expected: guard.Message, Actual: "正文命中玩法守则",
			})
			break
		}
	}
	return out
}

func validateNarrativeFacts(text string, narrative gameplay.TurnNarrativeContext) []narrativeViolation {
	changes := map[string]gameplay.FactChange{}
	facts := map[string]any{}
	preserved := map[string]bool{}
	failedBreakthrough := false
	hasInventoryChange := false
	for _, action := range narrative.Actions {
		if strings.Contains(action.ActionKey, "breakthrough") {
			failedBreakthrough = action.GoalStatus == "failed"
		}
		for _, change := range action.Changes {
			changes[change.Path] = change
			delete(preserved, change.Path)
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
			if _, changed := changes[fact.Path]; !changed {
				preserved[fact.Path] = true
			}
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
	index := pattern.FindStringIndex(text)
	return index != nil && negatedAt(text, index[0])
}

func firstAffirmedClaim(text string, pattern *regexp.Regexp) string {
	for _, index := range pattern.FindAllStringIndex(text, -1) {
		if negatedAt(text, index[0]) || speculativeAt(text, index[0]) {
			continue
		}
		return text[index[0]:index[1]]
	}
	return ""
}

// speculativeAt skips rumour, comparison and hypothetical clauses: missing a
// claim is preferable to rejecting legitimate prose.
func speculativeAt(text string, matchStart int) bool {
	prefix := claimPrefix(text, matchStart, 8)
	for _, marker := range []string{"据说", "传闻", "听说", "仿佛", "似乎", "像是", "好像", "也许", "可能", "如果", "假如"} {
		if strings.Contains(prefix, marker) {
			return true
		}
	}
	return false
}

func negatedAt(text string, matchStart int) bool {
	prefix := claimPrefix(text, matchStart, 8)
	for _, marker := range []string{"没有", "未", "并非", "不曾", "无法", "没有能", "没能"} {
		if strings.Contains(prefix, marker) {
			return true
		}
	}
	return false
}

// Limit exemptions to the matching clause and count Chinese characters, not bytes.
func claimPrefix(text string, matchStart, maxRunes int) string {
	prefix := text[:matchStart]
	if boundary := strings.LastIndexAny(prefix, "。！？!?；;，,\n"); boundary >= 0 {
		_, size := utf8.DecodeRuneInString(prefix[boundary:])
		prefix = prefix[boundary+size:]
	}
	runes := []rune(prefix)
	if len(runes) > maxRunes {
		runes = runes[len(runes)-maxRunes:]
	}
	return string(runes)
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
