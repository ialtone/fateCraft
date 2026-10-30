package romance

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/ialtone/fatecraft/internal/gameplay/contract"
)

type RivalState struct {
	Key      string `json:"key"`
	Progress int64  `json:"progress"`
	Since    int64  `json:"since"`
	Out      bool   `json:"out"`
}

type Message struct {
	ID   int64  `json:"id"`
	Kind string `json:"kind"`
	Day  int64  `json:"day"`
	Slot int64  `json:"slot"`
}

type Entry struct {
	Text string `json:"text"`
	Day  int64  `json:"day"`
}

type OpenScene struct {
	Key     string   `json:"key"`
	Options []string `json:"options"`
}

type State struct {
	Lead     string             `json:"lead"`
	Day      int64              `json:"day"`
	Slot     int64              `json:"slot"`
	Half     int64              `json:"half"`
	Stage    int64              `json:"stage"`
	StageDay int64              `json:"stage_day"`
	Stats    map[string]int64   `json:"stats"`
	Practice map[string]int64   `json:"practice"`
	Likes    []string           `json:"likes"`
	Dislikes []string           `json:"dislikes"`
	Rivals   []RivalState       `json:"rivals"`
	Messages []Message          `json:"messages"`
	Lists    map[string][]Entry `json:"lists"`
	Flags    map[string]int64   `json:"flags"`
	Scene    *OpenScene         `json:"scene"`
	Log      []string           `json:"log"`
	NextID   int64              `json:"next_id"`
}

var stateKeys = []string{"lead", "day", "slot", "half", "stage", "stage_day", "stats", "practice", "likes", "dislikes", "rivals", "messages", "lists", "flags", "scene", "log", "next_id"}

// game interprets the pack's rules for one transition. Random failures are
// kept in err and checked once by Execute.
type game struct {
	c        *Config
	l        Lead
	s        *State
	run      *contract.RunState
	random   contract.RandomSource
	err      error
	notes    []string
	rolls    int
	depth    int
	rival    *RivalState
	args     map[string]any
	act      Activity
	choice   string
	result   int64
	final    int64
	bonus    int64
	lastRoll string
}

func clamp(value, low, high int64) int64 { return min(high, max(low, value)) }

func b2i(ok bool) int64 {
	if ok {
		return 1
	}
	return 0
}

func (g *game) active() bool { return g.run.Status == contract.RunActive }

func (g *game) roll(upper int64) int64 {
	if g.err != nil || g.random == nil || upper < 1 {
		return 0
	}
	value, err := g.random.Intn(fmt.Sprintf("romance.r%d", g.rolls), upper)
	g.rolls++
	if err != nil {
		g.err = err
	}
	return value
}

func (g *game) note(text string) {
	if text != "" {
		g.notes = append(g.notes, text)
	}
}

func weekend(day int64) bool { return (day-1)%7 >= 5 }

func (g *game) gameTime() int64 {
	slot := g.c.Slots[min(g.s.Slot, int64(len(g.c.Slots)-1))]
	return (g.s.Day-1)*1440 + slot.Start + g.s.Half*slot.Half
}

func (g *game) stage() Stage { return g.c.Stages[clamp(g.s.Stage, 0, int64(len(g.c.Stages)-1))] }

func (g *game) stageDays() int64 { return g.s.Day - g.s.StageDay + 1 }

func (g *game) stat(key string) int64 {
	def, ok := g.c.stat(key)
	if !ok {
		return 0
	}
	if len(def.Sum) > 0 {
		return g.terms(def.Sum)
	}
	return g.s.Stats[key]
}

func (g *game) leave() int64 {
	d := g.c.Deadline
	if d.Flag == "" || g.s.Flags[d.Flag] == 0 || (d.Cancel != "" && g.s.Flags[d.Cancel] == 1) {
		return 0
	}
	return g.s.Flags[d.Flag] + d.LeaveAfter + g.s.Flags[d.Delay]
}

func (g *game) activeRivals() []*RivalState {
	result := []*RivalState{}
	for index := range g.s.Rivals {
		if !g.s.Rivals[index].Out {
			result = append(result, &g.s.Rivals[index])
		}
	}
	return result
}

func (g *game) ref(name string) int64 {
	name = g.fill(name)
	s := g.s
	head, rest, _ := strings.Cut(name, ".")
	switch head {
	case "":
		return 0
	case "stat":
		return g.stat(rest)
	case "flag":
		return s.Flags[rest]
	case "active":
		return b2i(s.Flags[rest] >= s.Day)
	case "lead":
		return g.l.Params[rest]
	case "cond":
		return b2i(g.cond(rest))
	case "list":
		return int64(len(s.Lists[rest]))
	case "arg":
		switch value := g.args[rest].(type) {
		case bool:
			return b2i(value)
		case float64:
			return int64(value)
		case int64:
			return value
		case int:
			return int64(value)
		}
		return 0
	case "chose":
		return b2i(g.choice == rest)
	case "confess":
		return map[string]int64{"result": g.result, "final": g.final, "bonus": g.bonus}[rest]
	case "act":
		return g.act.Halves
	case "rival":
		switch rest {
		case "count":
			return int64(len(g.activeRivals()))
		case "max":
			top := int64(0)
			for _, r := range g.activeRivals() {
				top = max(top, r.Progress)
			}
			return top
		}
		if g.rival == nil {
			return 0
		}
		return map[string]int64{"progress": g.rival.Progress, "days": s.Day - g.rival.Since, "band": g.rival.Progress / 20 * 20, "band_top": g.rival.Progress/20*20 + 19}[rest]
	case "deadline":
		leave := g.leave()
		return map[string]int64{"leave": leave, "due": b2i(s.Day >= g.c.Deadline.Max || leave > 0 && s.Day >= leave), "warning": s.Flags[g.c.Deadline.Flag], "max": g.c.Deadline.Max}[rest]
	case "stage":
		switch rest {
		case "":
			return s.Stage
		case "min_days":
			return g.stage().MinDays
		case "max_days":
			return g.stage().MaxDays
		case "last":
			return int64(len(g.c.Stages) - 1)
		case "rise":
			return b2i(len(g.stage().Rise) > 0 && g.checks(g.stage().Rise))
		case "push":
			return b2i(len(g.stage().Push) > 0 && g.checks(g.stage().Push))
		}
	case "revealed":
		switch rest {
		case "likes":
			return int64(len(s.Likes))
		case "dislikes":
			return int64(len(s.Dislikes))
		}
		return int64(len(s.Likes) + len(s.Dislikes))
	case "day":
		return s.Day
	case "slot":
		return s.Slot
	case "half":
		return s.Half
	case "weekend":
		return b2i(weekend(s.Day))
	case "stage_day":
		return g.stageDays()
	case "scene":
		return b2i(s.Scene != nil)
	case "messages":
		return int64(len(s.Messages))
	}
	return 0
}

func (g *game) terms(terms []Term) int64 {
	total := int64(0)
	for _, t := range terms {
		value := g.ref(t.Ref)
		if t.Mul != 0 {
			value *= t.Mul
		}
		if t.Div > 0 {
			value /= t.Div
		}
		total += value
	}
	return total
}

func (g *game) check(c Check) bool {
	left := g.ref(c.Ref) + g.terms(c.Plus)
	if c.Mod > 0 {
		left %= c.Mod
	}
	right := c.Value
	if c.Than != "" {
		right += g.ref(c.Than)
	}
	if c.Dice > 0 {
		die := g.roll(c.Dice) + 1
		left += die
		g.lastRoll = fmt.Sprintf("d%d=%d 合计 %d / DC %d", c.Dice, die, left, right)
		if c.Crit && die == c.Dice {
			return true
		}
		if c.Crit && die == 1 {
			return false
		}
	}
	switch c.Op {
	case "le":
		return left <= right
	case "gt":
		return left > right
	case "lt":
		return left < right
	case "eq":
		return left == right
	case "ne":
		return left != right
	}
	return left >= right
}

func (g *game) checks(checks []Check) bool {
	for _, c := range checks {
		if !g.check(c) {
			return false
		}
	}
	return true
}

func (g *game) cond(key string) bool {
	cond, ok := g.c.Conds[key]
	if !ok || g.depth > 12 {
		return false
	}
	g.depth++
	defer func() { g.depth-- }()
	if !g.checks(cond.All) {
		return false
	}
	if len(cond.Any) == 0 {
		return true
	}
	return slices.ContainsFunc(cond.Any, g.check)
}

func (g *game) gate(gates []Gate) string {
	for _, gate := range gates {
		if len(gate.If) > 0 && g.checks(fullChecks(gate.If)) {
			return g.fill(gate.Reason)
		}
	}
	return ""
}

func (g *game) text(key string) string {
	if value, ok := g.l.Profile[key]; ok {
		return value
	}
	return g.l.Hidden[key]
}

// fill expands {placeholders}: names, stage and slot labels, the current
// rival, lead texts and any numeric ref. Unknown keys keep the brace form
// instead of silently printing 0 (validators should catch typos first).
func (g *game) fill(template string) string {
	if !strings.Contains(template, "{") {
		return template
	}
	var out strings.Builder
	for {
		open := strings.IndexByte(template, '{')
		if open < 0 {
			break
		}
		end := strings.IndexByte(template[open:], '}')
		if end < 0 {
			break
		}
		out.WriteString(template[:open])
		key := template[open+1 : open+end]
		template = template[open+end+1:]
		value, ok := g.fillValue(key)
		if !ok {
			out.WriteByte('{')
			out.WriteString(key)
			out.WriteByte('}')
			continue
		}
		out.WriteString(value)
	}
	out.WriteString(template)
	return out.String()
}

func (g *game) fillValue(key string) (string, bool) {
	switch key {
	case "name":
		return g.l.Name, true
	case "stage_name":
		return g.stage().Name, true
	case "slot_name":
		return g.c.Slots[min(g.s.Slot, int64(len(g.c.Slots)-1))].Label, true
	case "label":
		return g.act.Label, true
	case "roll":
		return g.lastRoll, true
	case "rival":
		if g.rival == nil {
			return "", true
		}
		return g.rival.Key, true
	case "rival_name":
		if g.rival == nil {
			return "", true
		}
		def, _ := g.c.rival(g.rival.Key)
		return g.fill(def.Name), true
	case "kind":
		// Usually pre-substituted before fill; keep empty if not set.
		return "", true
	}
	if strings.HasPrefix(key, "text.") {
		name := strings.TrimPrefix(key, "text.")
		if name == "" {
			return "", false
		}
		return g.text(name), true
	}
	if _, ok := resolveRef(g.c, key); !ok {
		return "", false
	}
	return strconv.FormatInt(g.ref(key), 10), true
}

// resolveRef is the single known-ref table for fill, checks, and content validation.
// The returned string is the canonical name (currently identical to the input).
func resolveRef(c *Config, name string) (string, bool) {
	if name == "" || strings.Contains(name, "{") {
		return "", false
	}
	head, rest, _ := strings.Cut(name, ".")
	switch head {
	case "stat":
		if _, ok := c.stat(rest); !ok {
			return "", false
		}
	case "cond":
		if _, ok := c.Conds[rest]; !ok {
			return "", false
		}
	case "list":
		if _, ok := c.list(rest); !ok {
			return "", false
		}
	case "flag", "active", "lead", "arg", "chose":
		if rest == "" {
			return "", false
		}
	case "confess":
		if !slices.Contains([]string{"result", "final", "bonus"}, rest) {
			return "", false
		}
	case "rival":
		if !slices.Contains([]string{"count", "max", "progress", "days", "band", "band_top"}, rest) {
			return "", false
		}
	case "deadline":
		if !slices.Contains([]string{"leave", "due", "warning", "max"}, rest) {
			return "", false
		}
	case "stage":
		if rest != "" && !slices.Contains([]string{"min_days", "max_days", "last", "rise", "push"}, rest) {
			return "", false
		}
	case "revealed":
		if rest != "" && !slices.Contains([]string{"likes", "dislikes"}, rest) {
			return "", false
		}
	case "act":
		// Runtime g.ref("act") and g.ref("act.halves") both yield action halves.
		if rest != "" && rest != "halves" {
			return "", false
		}
	case "day", "slot", "half", "weekend", "stage_day", "scene", "messages":
		// Bare names (and any ignored suffix) resolve via g.ref head switch.
	default:
		return "", false
	}
	return name, true
}

func (g *game) add(key string, delta int64) {
	def, ok := g.c.stat(key)
	if !ok || len(def.Sum) > 0 || g.s.Stage < def.MinStage {
		return
	}
	if delta > 0 && g.l.Gain[key] > 0 {
		delta = delta * g.l.Gain[key] / 100
	}
	g.s.Stats[key] = g.bound(def, g.s.Stats[key]+delta)
}

func (g *game) bound(def StatDef, value int64) int64 {
	high := def.Max
	if def.CapFlag != "" && g.s.Flags[def.CapFlag] > 0 {
		high = min(high, def.CapMax)
	}
	return clamp(value, def.Min, high)
}

func (g *game) normalize() {
	for _, def := range g.c.Stats {
		if len(def.Sum) == 0 {
			g.s.Stats[def.Key] = g.bound(def, g.s.Stats[def.Key])
		}
	}
}

func (g *game) setFlag(key string, value int64) {
	if value == 0 {
		delete(g.s.Flags, key)
		return
	}
	g.s.Flags[key] = value
}

func matchTag(list []Tag, tags []string) (Tag, bool) {
	for _, tag := range list {
		for _, want := range tags {
			if slices.Contains(tag.Tags, want) || tag.Key == want {
				return tag, true
			}
		}
	}
	return Tag{}, false
}

func (g *game) unlock(key string) {
	if slices.ContainsFunc(g.l.Likes, func(t Tag) bool { return t.Key == key }) && !slices.Contains(g.s.Likes, key) {
		g.s.Likes = append(g.s.Likes, key)
	}
	if slices.ContainsFunc(g.l.Dislikes, func(t Tag) bool { return t.Key == key }) && !slices.Contains(g.s.Dislikes, key) {
		g.s.Dislikes = append(g.s.Dislikes, key)
	}
}

func (g *game) reveal(count int64) {
	for range count {
		next := slices.IndexFunc(g.l.Likes, func(t Tag) bool { return !slices.Contains(g.s.Likes, t.Key) })
		if next >= 0 {
			g.unlock(g.l.Likes[next].Key)
			continue
		}
		next = slices.IndexFunc(g.l.Dislikes, func(t Tag) bool { return !slices.Contains(g.s.Dislikes, t.Key) })
		if next >= 0 {
			g.unlock(g.l.Dislikes[next].Key)
		}
	}
}

func sorted(m map[string]int64) []string { return slices.Sorted(maps.Keys(m)) }

func empty(e Effect) bool { return reflect.ValueOf(e).IsZero() }

// apply runs an effect. Numeric gains scale by percent; the fixed order lets
// one rule raise a stage and then settle an ending.
func (g *game) apply(e Effect, percent int64) {
	if g.err != nil || empty(e) {
		return
	}
	s := g.s
	if e.Choice != "" {
		g.choice = e.Choice
	}
	if e.Result != 0 {
		g.result = e.Result
	}
	for _, key := range sorted(e.Stats) {
		g.add(key, e.Stats[key]*percent/100)
	}
	for _, key := range sorted(e.Set) {
		if def, ok := g.c.stat(key); ok {
			s.Stats[key] = g.bound(def, e.Set[key])
		}
	}
	for _, key := range sorted(e.Flags) {
		g.setFlag(g.fill(key), e.Flags[key])
	}
	for _, key := range sorted(e.Until) {
		g.setFlag(g.fill(key), s.Day+e.Until[key])
	}
	for _, key := range sorted(e.Add) {
		k := g.fill(key)
		g.setFlag(k, s.Flags[k]+e.Add[key])
	}
	for _, rival := range g.activeRivals() {
		rival.Progress = clamp(rival.Progress+e.Rivals, 0, 100)
		if e.RivalCap > 0 {
			rival.Progress = min(rival.Progress, e.RivalCap)
		}
	}
	if g.rival != nil {
		g.rival.Progress = clamp(g.rival.Progress+e.Rival, 0, 100)
		if e.RivalOut {
			g.rival.Out = true
		}
	}
	g.reveal(e.Reveal * percent / 100)
	if e.Expire {
		for index := range s.Messages {
			s.Messages[index].Day = 0
		}
	}
	if e.Collect != "" {
		g.push(e.Collect, g.act.Label)
	}
	g.normalize()
	if e.Confess != "" {
		g.confess(e.Confess == "final")
	}
	if e.Stage != "" {
		g.enterStage(e.Stage)
	}
	g.table(e.Table)
	if e.Scene != "" {
		g.open(e.Scene, e.Force)
	}
	if e.Outcome != "" {
		g.finish(e.Outcome, g.fill(e.Note))
	} else {
		g.note(g.fill(e.Note))
	}
}

func (g *game) push(list, text string) bool {
	def, ok := g.c.list(list)
	if !ok || text == "" {
		return false
	}
	entries := append(g.s.Lists[list], Entry{Text: text, Day: g.s.Day})
	if int64(len(entries)) > def.Max {
		if !def.Roll {
			return false
		}
		entries = entries[1:]
	}
	g.s.Lists[list] = entries
	return true
}

func (g *game) table(key string) {
	table, ok := g.c.table(key)
	if !ok || g.depth > 12 {
		return
	}
	g.depth++
	defer func() { g.depth-- }()
	for _, rule := range table.Rules {
		if !g.active() && g.err == nil && key != g.c.Hooks.AfterAction {
			return
		}
		if !g.checks(fullChecks(rule.When)) {
			continue
		}
		if rule.Chance > 0 && g.roll(100) >= rule.Chance {
			continue
		}
		g.apply(rule.Do, 100)
		if !rule.Continue {
			return
		}
	}
}

func (g *game) open(key string, force bool) {
	if g.s.Scene != nil && !force || !g.active() {
		return
	}
	scene, ok := g.c.scene(key)
	if !ok {
		return
	}
	options := []string{}
	for _, option := range scene.Options {
		if g.checks(fullChecks(option.When)) {
			options = append(options, option.Key)
		}
	}
	if len(options) > 0 {
		g.s.Scene = &OpenScene{Key: key, Options: options}
	}
}

func (g *game) enterStage(target string) {
	next := g.s.Stage + 1
	if target != "next" {
		next = int64(slices.IndexFunc(g.c.Stages, func(s Stage) bool { return s.Key == target }))
	}
	if next <= g.s.Stage || next >= int64(len(g.c.Stages)) {
		return
	}
	g.s.Stage, g.s.StageDay = next, g.s.Day
	g.note(fmt.Sprintf("进入「%s」阶段", g.stage().Name))
	g.table(g.stage().OnEnter)
}

func (g *game) finish(outcome, reason string) {
	if !g.active() {
		return
	}
	def, _ := g.c.outcome(outcome)
	g.run.OutcomeKey = outcome
	g.run.Status = contract.RunFailed
	if def.Success {
		g.run.Status = contract.RunCompleted
	}
	g.s.Scene = nil
	if reason != "" {
		g.note(fmt.Sprintf("结局：%s（%s）", def.Label, reason))
	} else {
		g.note(fmt.Sprintf("结局：%s", def.Label))
	}
}

// confess settles a confession through the pack's table; the table reports
// 1 success, 2 not yet, -1 refused via Effect.Result.
func (g *game) confess(final bool) {
	cf := g.c.Confess
	g.final = b2i(final)
	g.result = 0
	for _, arg := range cf.Args {
		if g.ref("arg."+arg.Key) == 1 && g.checks(fullChecks(arg.When)) {
			g.apply(arg.Do, 100)
		}
	}
	g.bonus = g.terms(cf.Bonus)
	for _, key := range cf.Clear {
		delete(g.s.Flags, g.fill(key))
	}
	g.s.Flags["confess.count"]++
	before := len(g.notes)
	g.table(cf.Table)
	if cf.List != "" && len(g.notes) > before {
		g.push(cf.List, fmt.Sprintf("%s：%s", g.fill(cf.Hook), g.notes[before]))
	}
	g.table(cf.Then)
}
