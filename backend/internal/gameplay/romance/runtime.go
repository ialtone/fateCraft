package romance

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/ialtone/fatecraft/internal/gameplay/components/ruledata"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
)

type Module struct{}

var aidKeys = []string{"aid_1", "aid_2", "aid_3"}

func (Module) NarrativeFactPaths() []string {
	return []string{"lead.name", "stage", "day", "slot", "intimacy_tier", "facts", "warning", "rivals", "pending_messages", "scene", "last_result", "outcome_key"}
}

func (Module) Actions() []contract.RuntimeAction {
	text := func(key, label string, required bool, options ...string) contract.RuntimeParameter {
		return contract.RuntimeParameter{Key: key, Label: label, Type: "string", Required: required, Options: options}
	}
	aids := []contract.RuntimeParameter{}
	for index, key := range aidKeys {
		aids = append(aids, contract.RuntimeParameter{Key: key, Label: fmt.Sprintf("告白助力 %d", index+1), Type: "boolean"})
	}
	actions := []contract.RuntimeAction{
		{Key: "romance.start", Label: "选择角色", Description: "开局选择本局要追的角色", Parameters: []contract.RuntimeParameter{text("lead", "角色", true)}},
		{Key: "romance.act", Label: "行动", Description: "执行规则包定义的行动；平台判定成败并推进时段", Parameters: []contract.RuntimeParameter{text("activity", "行动", true)}},
		{Key: "romance.reply", Label: "回复消息", Description: "回复限时消息；模型给出回复质量候选，平台按喜好/雷区最终判定", Parameters: []contract.RuntimeParameter{text("message", "消息", true), text("quality", "回复质量", true, "low", "ok", "good"), text("topic", "触及话题", false)}},
		{Key: "romance.confess", Label: "告白", Description: "按规则包的告白判定表结算", Parameters: aids},
		{Key: "romance.choose", Label: "做出选择", Description: "回应当前关系事件、抉择或结局场景", Parameters: []contract.RuntimeParameter{text("option", "选项", true)}},
		{Key: "romance.trip", Label: "旅行", Description: "一次回合完成多日旅行；行程片段用逗号分隔、天之间用分号分隔", Parameters: []contract.RuntimeParameter{text("destination", "目的地", true), text("plan", "行程片段", true)}},
		{Key: "romance.rival", Label: "应对情敌", Description: "使用规则包定义的情敌手段", Parameters: []contract.RuntimeParameter{text("tactic", "手段", true), text("rival", "情敌", true)}},
		{Key: "romance.remember", Label: "记下细节", Description: "记录可回收细节、专属称呼或内部梗候选", Parameters: []contract.RuntimeParameter{text("kind", "类型", true), text("text", "内容", true)}},
		{Key: "romance.boundary", Label: "分寸反馈", Description: "报告玩家在亲密推进中的分寸", Parameters: []contract.RuntimeParameter{text("kind", "类型", true)}},
		{Key: "romance.abandon", Label: "放弃", Description: "结束本局，只结算已达成阶段"},
	}
	for index := range actions {
		actions[index].Actors = []string{"player", "model"}
		switch actions[index].Key {
		case "romance.remember", "romance.boundary":
			actions[index].Actors = []string{"model"}
		case "romance.start", "romance.abandon":
			actions[index].Actors = []string{"player"}
			actions[index].Ending = actions[index].Key == "romance.abandon"
		}
		actions[index].Writes = stateKeys
		actions[index].Events = []string{"romance.acted", "romance.stage", "romance.ended"}
	}
	return actions
}

func loadConfig(content contract.Content) (Config, error) {
	config, err := ruledata.Singleton[Config](content, DefinitionKind)
	if err != nil {
		return config, err
	}
	return config, validateConfig(config)
}

func (Module) Initialize(content contract.Content, _ contract.InitialInput) (contract.RunState, error) {
	config, err := loadConfig(content)
	if err != nil {
		return contract.RunState{}, err
	}
	state := State{Day: 1, StageDay: 1, Stats: map[string]int64{}, Practice: map[string]int64{}, Likes: []string{}, Dislikes: []string{}, Rivals: []RivalState{}, Messages: []Message{}, Lists: map[string][]Entry{}, Flags: map[string]int64{}, Log: []string{}, NextID: 1}
	for _, def := range config.Stats {
		if len(def.Sum) == 0 {
			state.Stats[def.Key] = def.Initial
		}
	}
	for _, list := range config.Lists {
		state.Lists[list.Key] = []Entry{}
	}
	data, err := ruledata.Object(state)
	return contract.RunState{SchemaVersion: 1, Status: contract.RunActive, Data: data}, err
}

func load(content contract.Content, run *contract.RunState) (*game, error) {
	config, err := loadConfig(content)
	if err != nil {
		return nil, err
	}
	s, err := ruledata.Decode[State](run.Data)
	if err != nil {
		return nil, err
	}
	for _, m := range []*map[string]int64{&s.Stats, &s.Practice, &s.Flags} {
		if *m == nil {
			*m = map[string]int64{}
		}
	}
	if s.Lists == nil {
		s.Lists = map[string][]Entry{}
	}
	lead, _ := config.lead(s.Lead)
	return &game{c: &config, l: lead, s: &s, run: run}, nil
}

func (Module) ValidateState(content contract.Content, run contract.RunState) error {
	g, err := load(content, &run)
	if err != nil {
		return err
	}
	c, s := g.c, g.s
	in := func(value, low, high int64) bool { return value >= low && value <= high }
	if run.SchemaVersion != 1 || !in(s.Day, 1, c.Deadline.Max) || !in(s.Slot, 0, int64(len(c.Slots)-1)) || !in(s.Half, 0, 1) || !in(s.Stage, 0, int64(len(c.Stages)-1)) || !in(s.StageDay, 1, s.Day) {
		return fmt.Errorf("养成时间或阶段无效")
	}
	for _, def := range c.Stats {
		if value, ok := s.Stats[def.Key]; len(def.Sum) == 0 && (!ok || !in(value, def.Min, def.Max)) {
			return fmt.Errorf("数值 %s 越界", def.Key)
		}
	}
	if len(s.Stats) > len(c.Stats) || int64(len(s.Rivals)) > max(c.Rivals.Max, 0) || int64(len(s.Messages)) > max(c.Messages.Max, 0) || len(s.Log) > 8 || len(s.Flags) > 400 || len(s.Practice) > len(c.Stats) {
		return fmt.Errorf("养成列表超过上限")
	}
	for key, entries := range s.Lists {
		if def, ok := c.list(key); !ok || int64(len(entries)) > def.Max {
			return fmt.Errorf("列表 %s 无效", key)
		}
	}
	if s.Lead == "" {
		if s.Day != 1 || s.Stage != 0 || run.Status == contract.RunCompleted {
			return fmt.Errorf("未选择角色的状态无效")
		}
		return nil
	}
	if g.l.Key == "" {
		return fmt.Errorf("角色无效")
	}
	for _, key := range s.Likes {
		if !slices.ContainsFunc(g.l.Likes, func(t Tag) bool { return t.Key == key }) {
			return fmt.Errorf("解锁信息无效")
		}
	}
	for _, key := range s.Dislikes {
		if !slices.ContainsFunc(g.l.Dislikes, func(t Tag) bool { return t.Key == key }) {
			return fmt.Errorf("解锁信息无效")
		}
	}
	for _, rival := range s.Rivals {
		if _, ok := c.rival(rival.Key); !ok || !in(rival.Progress, 0, 100) {
			return fmt.Errorf("情敌状态无效")
		}
	}
	if s.Scene != nil {
		scene, ok := c.scene(s.Scene.Key)
		if !ok || len(s.Scene.Options) == 0 {
			return fmt.Errorf("场景无效")
		}
		for _, option := range s.Scene.Options {
			if !slices.ContainsFunc(scene.Options, func(o Choice) bool { return o.Key == option }) {
				return fmt.Errorf("场景选项无效")
			}
		}
	}
	if run.Status != contract.RunActive {
		outcome, ok := c.outcome(run.OutcomeKey)
		if !ok || outcome.Success != (run.Status == contract.RunCompleted) {
			return fmt.Errorf("结局无效")
		}
		if !g.checks(outcome.Requires) {
			return fmt.Errorf("结局 %s 事实不完整", outcome.Key)
		}
	}
	return nil
}

// ---- time ----

func (g *game) advance(halves int64) {
	for range halves {
		if !g.active() || g.err != nil {
			return
		}
		g.s.Half++
		if g.s.Half < 2 {
			continue
		}
		g.s.Half = 0
		next := g.s.Slot + 1
		if next < int64(len(g.c.Slots)) && (!g.c.Slots[next].WeekendOnly || weekend(g.s.Day)) {
			g.s.Slot = next
			g.expire()
			continue
		}
		g.newDay()
	}
}

func (g *game) kind(key string) MessageKind {
	i := slices.IndexFunc(g.c.Messages.Kinds, func(k MessageKind) bool { return k.Key == key })
	if i < 0 {
		return MessageKind{Key: key, Label: key}
	}
	return g.c.Messages.Kinds[i]
}

func (g *game) expire() {
	kept := []Message{}
	for _, message := range g.s.Messages {
		if message.Day > g.s.Day || message.Day == g.s.Day && message.Slot >= g.s.Slot {
			kept = append(kept, message)
			continue
		}
		kind := g.kind(message.Kind)
		g.apply(kind.Expire, 100)
		g.note(g.fill(strings.ReplaceAll(g.c.Messages.Expired, "{kind}", kind.Label)))
	}
	g.s.Messages = kept
}

func (g *game) newDay() {
	s := g.s
	g.table(g.c.Hooks.Night)
	for _, def := range g.c.Stats {
		if len(def.Sum) > 0 {
			continue
		}
		value := s.Stats[def.Key]
		if def.DecayPercent > 0 {
			value = value * def.DecayPercent / 100
		}
		value += def.Daily
		if def.Drift > 0 {
			value -= clamp(value, -def.Drift, def.Drift)
		}
		s.Stats[def.Key] = g.bound(def, value)
	}
	s.Day++
	s.Slot, s.Half = 0, 0
	g.expire()
	if !g.active() {
		return
	}
	g.table(g.c.Hooks.Morning)
	if !g.active() {
		return
	}
	g.rivalsDaily()
	if !g.active() {
		return
	}
	g.morningMessages()
	g.events()
	if g.active() {
		g.table(g.c.Hooks.Schedule)
	}
}

func (g *game) morningMessages() {
	m := g.c.Messages
	for _, kind := range m.Kinds {
		if int64(len(g.s.Messages)) >= m.Max {
			return
		}
		if !g.checks(kind.When) || kind.Chance > 0 && g.roll(100) >= kind.Chance {
			continue
		}
		g.s.Messages = append(g.s.Messages, Message{ID: g.s.NextID, Kind: kind.Key, Day: g.s.Day, Slot: kind.Slot})
		g.s.NextID++
	}
}

func (g *game) rivalsDaily() {
	r, s := g.c.Rivals, g.s
	if len(r.Pool) == 0 {
		return
	}
	flag := "rival_roll." + g.stage().Key
	if s.Flags[flag] == 0 && int64(len(s.Rivals)) < r.Max && g.checks(r.When) {
		s.Flags[flag] = 1
		if g.roll(100) < r.Chance {
			pool := []string{}
			for _, rival := range r.Pool {
				if slices.ContainsFunc(s.Rivals, func(x RivalState) bool { return x.Key == rival.Key }) {
					continue
				}
				pool = append(pool, rival.Key)
				if rival.Key == g.l.Favored {
					pool = append(pool, rival.Key)
				}
			}
			if len(pool) > 0 {
				def, _ := g.c.rival(pool[g.roll(int64(len(pool)))])
				s.Rivals = append(s.Rivals, RivalState{Key: def.Key, Progress: def.Start, Since: s.Day})
				g.note(r.Note)
			}
		}
	}
	for _, rival := range g.activeRivals() {
		g.rival = rival
		def, _ := g.c.rival(rival.Key)
		daily := def.Daily
		for _, adjust := range append(slices.Clone(def.Adjust), r.Adjust...) {
			if !g.checks(fullChecks(adjust.When)) {
				continue
			}
			if adjust.Daily != 0 {
				daily = adjust.Daily
			}
			if adjust.Percent > 0 && daily > 0 {
				daily = daily * adjust.Percent / 100
			}
		}
		rival.Progress = clamp(rival.Progress+daily, 0, 100)
		g.table(r.Fall)
		g.rival = nil
		if !g.active() {
			return
		}
	}
}

func (g *game) events() {
	e := g.c.Events
	for _, pool := range e.Pools {
		if !g.active() {
			return
		}
		if !g.checks(pool.When) || pool.Chance > 0 && g.roll(100) >= pool.Chance {
			continue
		}
		candidates := []Event{}
		for _, event := range e.Events {
			if event.Pool == pool.Key && event.MinStage <= g.s.Stage && g.checks(event.When) {
				candidates = append(candidates, event)
			}
		}
		if len(candidates) == 0 {
			continue
		}
		event := candidates[g.roll(int64(len(candidates)))]
		g.lastRoll = "无需判定"
		success := g.checks(event.Check)
		verdict := "成功"
		if success {
			g.apply(event.Success, 100)
		} else {
			verdict = "失败"
			g.apply(event.Fail, 100)
		}
		g.apply(event.Always, 100)
		g.note(fmt.Sprintf("%s「%s」%s：%s", e.Note, event.Name, g.lastRoll, verdict))
	}
}

// ---- activities ----

func (g *game) capFlag(category string) string { return "cap." + category + "." + g.stage().Key }

func (g *game) costReason(costs map[string]int64) string {
	for _, key := range sorted(costs) {
		if have := g.s.Stats[key]; have < costs[key] {
			def, _ := g.c.stat(key)
			return fmt.Sprintf("%s不够，差 %d", def.Label, costs[key]-have)
		}
	}
	return ""
}

func (g *game) block(a Activity) string {
	s := g.s
	g.act = a
	cat := g.c.category(a.Category)
	slot := g.c.Slots[s.Slot]
	last := s.Flags["last."+a.Key]
	switch {
	case !slices.Contains(a.Slots, slot.Key):
		return fmt.Sprintf("%s不能进行", slot.Label)
	case a.MinStage > s.Stage:
		return fmt.Sprintf("需要「%s」阶段", g.c.Stages[a.MinStage].Name)
	}
	if reason := g.costReason(a.Cost); reason != "" {
		return reason
	}
	switch {
	case a.Cooldown > 0 && last > 0 && s.Day-last < a.Cooldown:
		return fmt.Sprintf("每 %d 天一次", a.Cooldown)
	case cat.PerSlot && s.Flags["slot."+a.Key] == g.slotMark():
		return "这个时段已经做过"
	}
	if reason := g.gate(a.Gates); reason != "" {
		return reason
	}
	if reason := g.gate(cat.Gates); reason != "" {
		return reason
	}
	if limit, ok := g.stage().Caps[a.Category]; ok && s.Flags[g.capFlag(a.Category)] >= limit && (cat.FreeFlag == "" || s.Flags[cat.FreeFlag] != s.Day) {
		return fmt.Sprintf("本阶段这类行动已满 %d 次", limit)
	}
	return g.gate(g.c.Tuning.Gates)
}

func (g *game) slotMark() int64 { return (g.s.Day-1)*1440 + g.c.Slots[g.s.Slot].Start + 1 }

func (g *game) rate(a Activity) int64 {
	if a.Rate == 0 {
		return 100
	}
	g.act = a
	t := g.c.Tuning
	rate := a.Rate + g.terms(a.RateTerms) + g.terms(t.RateTerms)
	if _, ok := matchTag(g.l.Likes, a.Tags); ok {
		rate += t.LikeRate
	}
	if _, ok := matchTag(g.l.Dislikes, a.Tags); ok {
		rate -= t.DislikeRate
	}
	return clamp(rate, t.Min, t.Max)
}

func (g *game) train(key string) {
	def, ok := g.c.stat(key)
	if !ok {
		return
	}
	g.s.Practice[key]++
	level, need := g.s.Stats[key], int64(1)
	if len(def.Practice) > 0 {
		need = def.Practice[clamp(level, 0, int64(len(def.Practice)-1))]
	}
	if level < def.Max && g.s.Practice[key] >= need {
		g.s.Stats[key]++
		g.s.Practice[key] = 0
	}
}

func (g *game) income(amount int64) {
	in := g.c.Income
	value := amount * (100 + g.s.Flags[in.PercentFlag]) / 100
	g.s.Stats[in.Stat] += value
	if in.Tally != "" {
		before := g.s.Flags[in.Tally]
		g.s.Flags[in.Tally] += value
		if in.Each > 0 && in.Bump != "" {
			for range g.s.Flags[in.Tally]/in.Each - before/in.Each {
				g.s.Stats[in.Bump]++
			}
		}
	}
	g.normalize()
}

func (g *game) doActivity(a Activity) {
	s, t := g.s, g.c.Tuning
	g.act = a
	cat := g.c.category(a.Category)
	s.Flags["last."+a.Key] = s.Day
	if cat.PerSlot {
		s.Flags["slot."+a.Key] = g.slotMark()
	}
	if _, ok := g.stage().Caps[a.Category]; ok {
		if cat.FreeFlag != "" && s.Flags[cat.FreeFlag] == s.Day {
			delete(s.Flags, cat.FreeFlag)
		} else {
			s.Flags[g.capFlag(a.Category)]++
		}
	}
	for _, key := range sorted(a.Cost) {
		s.Stats[key] -= a.Cost[key]
	}
	if a.Income > 0 && g.c.Income.Stat != "" {
		g.income(a.Income)
	}
	if a.Train != "" {
		g.train(a.Train)
	}
	if mark := g.c.Slots[s.Slot].Mark; mark != "" {
		s.Flags[mark] = s.Day
	}
	g.normalize()
	g.apply(a.Always, 100)
	if !cat.Check {
		g.note(a.Label + "完成")
		return
	}
	if cat.Contact {
		g.apply(g.c.Contact, 100)
	}
	if a.Table != "" {
		g.table(a.Table)
		return
	}
	rate := g.rate(a)
	if g.roll(100) >= rate {
		fail := a.Fail
		if empty(fail) {
			fail = t.Fail
		}
		g.apply(fail, 100)
		g.note(fmt.Sprintf("%s没能成功（成功率 %d%%）", a.Label, rate))
		return
	}
	percent := int64(100)
	if cat.RepeatDays > 0 {
		recent := s.Flags["recent."+cat.Key]
		if recent > 0 && recent >= s.Day-cat.RepeatDays {
			percent = cat.RepeatPercent
		}
		s.Flags["recent."+cat.Key] = s.Day
	}
	g.table(cat.OnSuccess)
	if p, ok := g.stage().Percent[a.Category]; ok {
		percent = percent * p / 100
	}
	like, liked := matchTag(g.l.Likes, a.Tags)
	dislike, disliked := matchTag(g.l.Dislikes, a.Tags)
	switch {
	case disliked:
		reversed := Effect{Stats: map[string]int64{}}
		for key, value := range t.Dislike.Stats {
			reversed.Stats[key] = value
		}
		for _, key := range t.Reverse {
			reversed.Stats[key] -= a.Effects.Stats[key]
		}
		g.apply(reversed, percent)
		g.apply(t.Landmine, 100)
		g.unlock(dislike.Key)
		g.note(fmt.Sprintf("%s踩到了%s的雷区：%s", a.Label, g.l.Name, dislike.Label))
		return
	case liked:
		percent = percent * t.LikePercent / 100
		g.unlock(like.Key)
		g.note(fmt.Sprintf("%s命中%s的喜好：%s", a.Label, g.l.Name, like.Label))
	default:
		g.note(fmt.Sprintf("%s顺利（成功率 %d%%）", a.Label, rate))
	}
	g.apply(a.Effects, percent)
	g.table(g.c.Hooks.Success)
	g.apply(cat.Bonus, 100)
	g.table(a.After)
}

// ---- trips, replies, scenes ----

func (g *game) parsePlan(plan string, days int64) ([][]string, error) {
	tr := g.c.Travel
	result, total := [][]string{}, int64(0)
	for index, part := range strings.Split(plan, ";") {
		if int64(index) >= days {
			return nil, contract.Reject("trip.days", "行程超过 %d 天", days)
		}
		segments := []string{}
		for _, raw := range strings.Split(part, ",") {
			tag := strings.TrimSpace(raw)
			if tag == "" {
				continue
			}
			if !slices.Contains(tr.Segments, tag) {
				tag = tr.Fallback
			}
			segments = append(segments, tag)
			total++
		}
		result = append(result, segments)
	}
	if total == 0 || total > tr.MaxSegments {
		return nil, contract.Reject("trip.segments", "行程片段需要 1–%d 个", tr.MaxSegments)
	}
	return result, nil
}

func (g *game) trip(t Trip, plan [][]string) {
	s, tr := g.s, g.c.Travel
	s.Stats[tr.CostStat] -= t.Cost
	if tr.CountFlag != "" {
		s.Flags[tr.CountFlag]++
	}
	g.apply(g.c.Contact, 100)
	if len(s.Messages) > 0 {
		s.Messages = s.Messages[1:]
		g.apply(tr.Reply, 100)
	}
	bonus, downgraded := int64(0), 0
	for day := range t.Days {
		segments := []string{}
		if int(day) < len(plan) {
			segments = plan[day]
		}
		for _, tag := range segments {
			if tag == tr.Downgrade && (tr.Unless == "" || !g.cond(tr.Unless)) {
				tag = tr.To
				downgraded++
			}
			if _, ok := matchTag(g.l.Likes, []string{tag}); ok {
				bonus += tr.Like
			}
			if _, ok := matchTag(g.l.Dislikes, []string{tag}); ok {
				bonus += tr.Dislike
				g.apply(tr.DislikeDo, 100)
			}
		}
		g.apply(t.Daily, 100)
		if tr.BusyOver > 0 && int64(len(segments)) > tr.BusyOver {
			g.apply(tr.Busy, 100)
		}
		if len(tr.DayCheck) > 0 && !g.checks(tr.DayCheck) {
			g.apply(tr.DayFail, 100)
			g.note(fmt.Sprintf("旅行第 %d 天的天命审判失败（%s）", day+1, g.lastRoll))
		}
	}
	bonus = clamp(bonus, tr.Floor, tr.Cap)
	g.apply(t.Effects, 100+bonus)
	g.note(fmt.Sprintf("%s之旅结算：片段匹配 %+d%%，降级 %d 个片段", t.Name, bonus, downgraded))
	for range t.Days {
		if !g.active() {
			return
		}
		s.Slot, s.Half = int64(len(g.c.Slots)-1), 0
		g.newDay()
	}
}

func (g *game) reply(message Message, quality, topic string) {
	kind := g.kind(message.Kind)
	if topic != "" {
		if tag, ok := matchTag(g.l.Dislikes, []string{topic}); ok {
			g.unlock(tag.Key)
			g.apply(g.c.Messages.Dislike, 100)
			quality = "low"
		} else if tag, ok := matchTag(g.l.Likes, []string{topic}); ok {
			g.unlock(tag.Key)
			if quality != "low" {
				quality = "hit"
			}
		}
	}
	if quality == "good" {
		quality = "ok"
	}
	g.choice = quality
	g.apply(map[string]Effect{"low": kind.Low, "ok": kind.Ok, "hit": kind.Hit}[quality], 100)
	g.apply(g.c.Contact, 100)
	g.note(fmt.Sprintf("回复了%s（%s）", kind.Label, map[string]string{"low": "敷衍", "ok": "得体", "hit": "说到心坎"}[quality]))
}

func (g *game) runChoice(choice Choice) {
	g.choice = choice.Key
	g.apply(choice.Do, 100)
	g.table(choice.Table)
}

func findChoice(choices []Choice, key string) (Choice, bool) {
	i := slices.IndexFunc(choices, func(c Choice) bool { return c.Key == key })
	if i < 0 {
		return Choice{}, false
	}
	return choices[i], true
}

// ---- kernel surface ----

func (Module) Preview(content contract.Content, run contract.RunState, actor string) ([]contract.ActionPreview, error) {
	g, err := load(content, &run)
	if err != nil {
		return nil, err
	}
	c, s := g.c, g.s
	previews := []contract.ActionPreview{}
actions:
	for _, action := range (Module{}).Actions() {
		if !slices.Contains(action.Actors, actor) {
			continue
		}
		// Pack-level absence: omit the action entirely instead of a greyed
		// “这个世界没有…” row. State-dependent unavailability still returns
		// Available=false below.
		switch action.Key {
		case "romance.confess":
			if c.Confess.Table == "" {
				continue actions
			}
		case "romance.trip":
			if len(c.Travel.Trips) == 0 {
				continue actions
			}
		case "romance.reply":
			if len(c.Messages.Kinds) == 0 {
				continue actions
			}
		case "romance.rival":
			if len(c.Rivals.Pool) == 0 {
				continue actions
			}
		case "romance.remember":
			if !slices.ContainsFunc(c.Lists, func(l ListDef) bool { return l.Writable }) {
				continue actions
			}
		case "romance.boundary":
			if len(c.Reactions) == 0 {
				continue actions
			}
		}
		preview := contract.ActionPreview{Action: action, Available: true}
		block := func(reason string) { preview.Available, preview.Reason = false, reason }
		param := func(index int, options []string, labels map[string]string) {
			preview.Action.Parameters = slices.Clone(preview.Action.Parameters)
			preview.Action.Parameters[index].Options = options
			preview.Action.Parameters[index].OptionLabels = labels
		}
		choices := func(index int, list []Choice) {
			options, labels := []string{}, map[string]string{}
			for _, choice := range list {
				if g.checks(fullChecks(choice.When)) {
					options = append(options, choice.Key)
					labels[choice.Key] = g.fill(choice.Label)
				}
			}
			if len(options) == 0 {
				block("当前没有可用选项")
				return
			}
			param(index, options, labels)
		}
		switch {
		case action.Key == "romance.abandon":
		case action.Key == "romance.start":
			if s.Lead != "" {
				block("已经选定了角色")
				break
			}
			options, labels := []string{}, map[string]string{}
			for _, lead := range c.Leads {
				options = append(options, lead.Key)
				labels[lead.Key] = fmt.Sprintf("%s · %d 岁 · %s", lead.Name, lead.Age, lead.Title)
				preview.Plans = append(preview.Plans, contract.ActionPlan{Label: labels[lead.Key], Arguments: map[string]any{"lead": lead.Key}})
			}
			param(0, options, labels)
		case s.Lead == "":
			block("先选择角色")
		case action.Key == "romance.choose":
			if s.Scene == nil {
				block("当前没有需要选择的场景")
				break
			}
			scene, _ := c.scene(s.Scene.Key)
			labels := map[string]string{}
			for _, key := range s.Scene.Options {
				option, _ := findChoice(scene.Options, key)
				labels[key] = g.fill(option.Label)
				preview.Plans = append(preview.Plans, contract.ActionPlan{Label: labels[key], Arguments: map[string]any{"option": key}})
			}
			param(0, s.Scene.Options, labels)
		case s.Scene != nil && action.Key != "romance.remember" && action.Key != "romance.reply":
			block("先回应当前场景")
		case action.Key == "romance.act":
			options, labels := []string{}, map[string]string{}
			for _, a := range c.Activities {
				label := a.Label
				if a.LabelList != "" {
					if list := g.l.Lists[a.LabelList]; s.Stage < int64(len(list)) {
						label = list[s.Stage]
					}
				}
				g.act = a
				label = g.fill(label)
				options = append(options, a.Key)
				labels[a.Key] = label
				if g.block(a) == "" && len(preview.Plans) < 64 {
					cost := []string{}
					for _, key := range sorted(a.Cost) {
						def, _ := c.stat(key)
						cost = append(cost, fmt.Sprintf("%s %d", def.Label, a.Cost[key]))
					}
					costText := "免费"
					if len(cost) > 0 {
						costText = "花费 " + strings.Join(cost, " ")
					}
					preview.Plans = append(preview.Plans, contract.ActionPlan{Label: fmt.Sprintf("%s · %s · 成功率 %d%%", label, costText, g.rate(a)), Arguments: map[string]any{"activity": a.Key}, Minutes: a.Halves * c.Slots[s.Slot].Half, Costs: a.Cost})
				}
			}
			param(0, options, labels)
			if len(preview.Plans) == 0 {
				block("这个时段没有可做的行动")
			}
		case action.Key == "romance.reply":
			options, labels := []string{}, map[string]string{}
			for _, message := range s.Messages {
				key := fmt.Sprint(message.ID)
				options = append(options, key)
				labels[key] = g.kind(message.Kind).Label
			}
			if len(options) == 0 {
				block("没有待回复的消息")
				break
			}
			param(0, options, labels)
			topics, topicLabels := []string{}, map[string]string{}
			for _, tag := range append(slices.Clone(g.l.Likes), g.l.Dislikes...) {
				unlocked := slices.Contains(s.Likes, tag.Key) || slices.Contains(s.Dislikes, tag.Key)
				if actor == "player" && !unlocked {
					continue
				}
				topics = append(topics, tag.Key)
				topicLabels[tag.Key] = tag.Label
			}
			preview.Action.Parameters[2].Options, preview.Action.Parameters[2].OptionLabels = topics, topicLabels
		case action.Key == "romance.confess":
			preview.Action.Parameters = slices.Clone(preview.Action.Parameters)
			// The kernel requires a stable parameter shape; aids the pack does
			// not declare keep their generic label and are ignored.
			for index, parameter := range preview.Action.Parameters {
				if arg, ok := findChoice(c.Confess.Args, parameter.Key); ok {
					preview.Action.Parameters[index].Label = g.fill(arg.Label)
				}
			}
			if reason := g.gate(c.Confess.Gates); reason != "" {
				block(reason)
			}
			preview.Minutes = c.Slots[s.Slot].Half
		case action.Key == "romance.trip":
			options, labels := []string{}, map[string]string{}
			for _, t := range c.Travel.Trips {
				options = append(options, t.Key)
				labels[t.Key] = fmt.Sprintf("%s · %d 天 · %d", t.Name, t.Days, t.Cost)
			}
			param(0, options, labels)
			if reason := g.gate(c.Travel.Gates); reason != "" {
				block(reason)
			}
		case action.Key == "romance.rival":
			options, labels := []string{}, map[string]string{}
			for _, rival := range g.activeRivals() {
				g.rival = rival
				options = append(options, rival.Key)
				labels[rival.Key] = g.fill("{rival_name}")
			}
			g.rival = nil
			if len(options) == 0 {
				block("没有情敌")
				break
			}
			param(1, options, labels)
			choices(0, c.Rivals.Tactics)
		case action.Key == "romance.remember":
			options, labels := []string{}, map[string]string{}
			for _, list := range c.Lists {
				if list.Writable {
					options = append(options, list.Key)
					labels[list.Key] = list.Label
				}
			}
			if len(options) == 0 {
				block("没有可记录的列表")
				break
			}
			param(0, options, labels)
		case action.Key == "romance.boundary":
			choices(0, c.Reactions)
		}
		previews = append(previews, preview)
	}
	return previews, nil
}

func (Module) Execute(content contract.Content, run contract.RunState, request contract.RuntimeRequest) (contract.Transition, error) {
	g, err := load(content, &run)
	if err != nil {
		return contract.Transition{}, err
	}
	c, s := g.c, g.s
	g.random, g.args = request.Random, request.Arguments
	text := func(key string) string { value, _ := request.Arguments[key].(string); return value }
	startStage := s.Stage
	reject := func(code, reason string) (contract.Transition, error) {
		return contract.Transition{}, contract.Reject(code, "%s", reason)
	}
	switch request.ActionKey {
	case "romance.abandon":
		g.finish(c.Abandon, "主动放弃")
	case "romance.start":
		g.l, _ = c.lead(text("lead"))
		s.Lead = g.l.Key
		g.apply(c.Contact, 100)
		g.table(c.Hooks.Start)
		g.morningMessages()
		g.note(fmt.Sprintf("你遇见了%s", g.l.Name))
	case "romance.choose":
		if s.Scene == nil {
			return reject("scene.missing", "当前没有需要选择的场景")
		}
		scene, ok := c.scene(s.Scene.Key)
		if !ok {
			return reject("scene.unknown", "未知场景")
		}
		option, ok := findChoice(scene.Options, text("option"))
		if !ok {
			return reject("scene.option", "无效选项")
		}
		s.Scene = nil
		g.runChoice(option)
	case "romance.act":
		a, ok := c.activity(text("activity"))
		if !ok {
			return reject("activity.unknown", "未知行动")
		}
		if reason := g.block(a); reason != "" {
			return reject("activity.unavailable", reason)
		}
		g.doActivity(a)
		g.advance(a.Halves)
	case "romance.reply":
		index := slices.IndexFunc(s.Messages, func(m Message) bool { return fmt.Sprint(m.ID) == text("message") })
		if index < 0 {
			return reject("message.missing", "没有这条待回复消息")
		}
		message := s.Messages[index]
		s.Messages = slices.Delete(s.Messages, index, index+1)
		g.reply(message, text("quality"), text("topic"))
	case "romance.confess":
		g.confess(c.Confess.Final != "" && g.cond(c.Confess.Final))
		if g.active() {
			g.advance(1)
		}
	case "romance.trip":
		trip, _ := findTrip(c, text("destination"))
		if reason := g.gate(trip.Gates); reason != "" {
			return reject("trip.unavailable", reason)
		}
		plan, err := g.parsePlan(text("plan"), trip.Days)
		if err != nil {
			return contract.Transition{}, err
		}
		if reason := g.costReason(map[string]int64{c.Travel.CostStat: trip.Cost}); reason != "" {
			return reject("trip.cost", reason)
		}
		g.trip(trip, plan)
	case "romance.rival":
		for _, rival := range g.activeRivals() {
			if rival.Key == text("rival") {
				g.rival = rival
			}
		}
		tactic, _ := findChoice(c.Rivals.Tactics, text("tactic"))
		if reason := g.gate([]Gate{tactic.Gate}); reason != "" {
			return reject("rival.unavailable", reason)
		}
		g.runChoice(tactic)
		g.rival = nil
		if g.active() {
			g.advance(tactic.Halves)
		}
	case "romance.remember":
		list, _ := c.list(text("kind"))
		value := strings.TrimSpace(text("text"))
		if value == "" || list.Chars > 0 && int64(utf8.RuneCountInString(value)) > list.Chars {
			return reject("remember.length", fmt.Sprintf("细节需在 %d 字以内", list.Chars))
		}
		if reason := g.gate(list.Gates); reason != "" {
			return reject("remember.unavailable", reason)
		}
		if !list.Roll && slices.ContainsFunc(s.Lists[list.Key], func(e Entry) bool { return e.Text == value }) || !g.push(list.Key, value) {
			return reject("remember.unavailable", fmt.Sprintf("%s最多 %d 条且不能重复", list.Label, list.Max))
		}
	case "romance.boundary":
		key := fmt.Sprintf("boundary.%d", g.gameTime())
		if s.Flags[key] == 1 {
			return reject("boundary.repeat", "这一刻已经记录过分寸")
		}
		for flag := range s.Flags {
			if strings.HasPrefix(flag, "boundary.") {
				delete(s.Flags, flag)
			}
		}
		s.Flags[key] = 1
		reaction, _ := findChoice(c.Reactions, text("kind"))
		if reason := g.gate([]Gate{reaction.Gate}); reason != "" {
			return reject("boundary.unavailable", reason)
		}
		g.runChoice(reaction)
	}
	if g.err != nil {
		return contract.Transition{}, g.err
	}
	if s.Lead != "" {
		g.table(c.Hooks.AfterAction)
	}
	if g.err != nil {
		return contract.Transition{}, g.err
	}
	s.Log = append(s.Log, g.notes...)
	if len(s.Log) > 8 {
		s.Log = s.Log[len(s.Log)-8:]
	}
	today := (s.Day - 1) * 1440
	for key, value := range s.Flags {
		if strings.HasPrefix(key, "slot.") && value <= today {
			delete(s.Flags, key)
		}
	}
	run.GameTime = max(run.GameTime, g.gameTime())
	run.Data, err = ruledata.Object(s)
	if err != nil {
		return contract.Transition{}, err
	}
	events := []contract.RuntimeEvent{{Kind: "romance.acted", Visibility: contract.VisibilityPlayer, Data: map[string]any{"action": request.ActionKey, "notes": g.notes}}}
	if s.Stage != startStage {
		events = append(events, contract.RuntimeEvent{Kind: "romance.stage", Visibility: contract.VisibilityPlayer, Data: map[string]any{"stage": g.stage().Key}})
	}
	if run.Status != contract.RunActive {
		events = append(events, contract.RuntimeEvent{Kind: "romance.ended", Visibility: contract.VisibilityPlayer, Data: map[string]any{"outcome_key": run.OutcomeKey}})
	}
	return contract.Transition{State: run, Events: events, Result: map[string]any{"action": request.ActionKey, "notes": g.notes, "outcome_key": run.OutcomeKey}}, nil
}

func findTrip(c *Config, key string) (Trip, bool) {
	i := slices.IndexFunc(c.Travel.Trips, func(t Trip) bool { return t.Key == key })
	if i < 0 {
		return Trip{}, false
	}
	return c.Travel.Trips[i], true
}

func bandLabel(bands []Band, value int64) string {
	for _, band := range bands {
		if value >= band.Min {
			return band.Label
		}
	}
	return ""
}

func tagNames(all []Tag, keys []string) []any {
	result := []any{}
	for _, tag := range all {
		if keys == nil || slices.Contains(keys, tag.Key) {
			result = append(result, map[string]any{"name": tag.Label})
		}
	}
	return result
}

func (Module) Project(content contract.Content, run contract.RunState, audience string) (map[string]any, error) {
	g, err := load(content, &run)
	if err != nil {
		return nil, err
	}
	c, s := g.c, g.s
	model := audience == contract.AudienceModel
	stage := g.stage()
	view := map[string]any{"day": s.Day, "slot": c.Slots[s.Slot].Label, "weekend": weekend(s.Day), "outcome_key": run.OutcomeKey, "log": slices.Clone(s.Log), "last_result": strings.Join(s.Log[max(0, len(s.Log)-3):], "；"),
		"stage": stage.Name, "stage_key": stage.Key, "stage_rank": s.Stage + 1, "stage_day": g.stageDays(), "intimacy_tier": stage.Tier}
	if s.Lead == "" {
		choices := []any{}
		for _, lead := range c.Leads {
			item := map[string]any{"name": lead.Name, "age": lead.Age, "title": lead.Title}
			for _, field := range c.ChoiceFields {
				item[field] = lead.Profile[field]
			}
			choices = append(choices, item)
		}
		view["lead"] = map[string]any{"name": c.NoLead}
		view["choices"], view["goal"] = choices, c.Goal
		view["stage"], view["stage_day"] = "未开始", 0
	} else {
		lead := map[string]any{"name": g.l.Name, "age": g.l.Age, "title": g.l.Title}
		for key, value := range g.l.Profile {
			lead[key] = value
		}
		view["lead"] = lead
	}
	hidden := map[string]any{}
	for _, def := range c.Stats {
		value := g.stat(def.Key)
		switch {
		case def.Visibility == "model" && model:
			hidden[def.Key] = value
		case def.Visibility != "player":
		case def.Group == "":
			view[def.Key] = value
		default:
			group, _ := view[def.Group].(map[string]any)
			if group == nil {
				group = map[string]any{}
				view[def.Group] = group
			}
			group[def.Key] = value
		}
	}
	facts := map[string]any{}
	for _, fact := range c.Facts {
		value := g.ref(fact.Ref)
		switch {
		case fact.Bool:
			facts[fact.Key] = value != 0
		case len(fact.Bands) > 0:
			facts[fact.Key] = bandLabel(fact.Bands, value)
		default:
			facts[fact.Key] = value
		}
	}
	view["facts"] = facts
	caps := map[string]any{}
	for _, key := range sorted(stage.Caps) {
		caps[key] = fmt.Sprintf("%d/%d", s.Flags[g.capFlag(key)], stage.Caps[key])
	}
	view["caps"] = caps
	messages := []any{}
	for _, message := range s.Messages {
		messages = append(messages, map[string]any{"id": message.ID, "kind": g.kind(message.Kind).Label, "expires": fmt.Sprintf("第 %d 天%s", message.Day, c.Slots[clamp(message.Slot, 0, int64(len(c.Slots)-1))].Label)})
	}
	view["messages"], view["pending_messages"] = messages, len(messages)
	rivals := []any{}
	for _, rival := range g.activeRivals() {
		g.rival = rival
		item := map[string]any{"name": g.fill("{rival_name}"), "hint": bandLabel(c.Rivals.Hints, rival.Progress)}
		if model {
			item["progress"] = rival.Progress
			def, _ := c.rival(rival.Key)
			for key, value := range def.Profile {
				item[key] = value
			}
		}
		rivals = append(rivals, item)
	}
	g.rival = nil
	view["rivals"] = rivals
	warning := c.Deadline.None
	if c.Deadline.Flag != "" && s.Flags[c.Deadline.Flag] > 0 {
		warning = g.fill(c.Deadline.Active)
	}
	view["warning"] = warning
	view["known_likes"], view["known_dislikes"] = tagNames(g.l.Likes, s.Likes), tagNames(g.l.Dislikes, s.Dislikes)
	for _, list := range c.Lists {
		items := []any{}
		for _, entry := range s.Lists[list.Key] {
			if list.Detail {
				items = append(items, map[string]any{"text": entry.Text, "day": entry.Day})
			} else {
				items = append(items, entry.Text)
			}
		}
		view[list.Key] = items
	}
	for _, reveal := range c.Reveals {
		items := []any{}
		values := g.l.Lists[reveal.List]
		for index := range min(int(max(g.stat(reveal.Stat), 0)), len(values)) {
			items = append(items, map[string]any{"name": values[index]})
		}
		view[reveal.Key] = items
	}
	for _, text := range c.Texts {
		if s.Lead != "" && g.checks(text.When) {
			view[text.Key] = g.fill(text.Template)
		} else {
			view[text.Key] = text.Else
		}
	}
	if s.Scene != nil {
		def, _ := c.scene(s.Scene.Key)
		view["scene"] = map[string]any{"key": def.Key, "title": g.fill(def.Title)}
	} else {
		view["scene"] = nil // JSON null; typed nil map would not compare equal to nil in any
	}
	if model && s.Lead != "" {
		for key, value := range g.l.Hidden {
			hidden[key] = value
		}
		hidden["likes"], hidden["dislikes"] = tagNames(g.l.Likes, nil), tagNames(g.l.Dislikes, nil)
		for key, values := range g.l.Lists {
			hidden[key] = slices.Clone(values)
		}
		view["hidden"] = hidden
	}
	return view, nil
}
