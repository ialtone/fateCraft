package romance

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ialtone/fatecraft/internal/gameplay/components/ruledata"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
)

// The romance engine is fully data driven: packs declare stats, stages, rule
// tables, leads, rivals, messages and endings; Go only interprets them.

const (
	EngineKey      = "romance.story"
	DefinitionKind = "romance.story"
)

// Term is ref*mul/div (mul 0 means 1).
type Term struct {
	Ref string `json:"ref"`
	Mul int64  `json:"mul"`
	Div int64  `json:"div"`
}

// Check compares ref+plus (mod Mod, plus a dice roll) against value+than.
type Check struct {
	Ref   string `json:"ref"`
	Plus  []Term `json:"plus"`
	Mod   int64  `json:"mod"`
	Dice  int64  `json:"dice"`
	Crit  bool   `json:"crit"`
	Op    string `json:"op"`
	Value int64  `json:"value"`
	Than  string `json:"than"`
}

// Test is a shallow Check (no dice or plus terms). Gates and rule guards use
// it so packs stay within the 8-level config depth; put dice and sums in a
// named cond and reference it as cond.<key>.
type Test struct {
	Ref   string `json:"ref"`
	Mod   int64  `json:"mod"`
	Op    string `json:"op"`
	Value int64  `json:"value"`
	Than  string `json:"than"`
}

func (t Test) full() Check {
	return Check{Ref: t.Ref, Mod: t.Mod, Op: t.Op, Value: t.Value, Than: t.Than}
}

func fullChecks(tests []Test) []Check {
	out := make([]Check, 0, len(tests))
	for _, t := range tests {
		out = append(out, t.full())
	}
	return out
}

type Gate struct {
	If     []Test `json:"if"`
	Reason string `json:"reason"`
}

type Cond struct {
	All []Check `json:"all"`
	Any []Check `json:"any"`
}

type Effect struct {
	Stats    map[string]int64 `json:"stats"`
	Set      map[string]int64 `json:"set"`
	Flags    map[string]int64 `json:"flags"`
	Until    map[string]int64 `json:"until"`
	Add      map[string]int64 `json:"add"`
	Rivals   int64            `json:"rivals"`
	RivalCap int64            `json:"rival_cap"`
	Rival    int64            `json:"rival"`
	RivalOut bool             `json:"rival_out"`
	Reveal   int64            `json:"reveal"`
	Expire   bool             `json:"expire"`
	Collect  string           `json:"collect"`
	Choice   string           `json:"choice"`
	Result   int64            `json:"result"`
	Confess  string           `json:"confess"`
	Stage    string           `json:"stage"`
	Table    string           `json:"table"`
	Scene    string           `json:"scene"`
	Force    bool             `json:"force"`
	Outcome  string           `json:"outcome"`
	Note     string           `json:"note"`
}

type Rule struct {
	When     []Test `json:"when"`
	Chance   int64  `json:"chance"`
	Do       Effect `json:"do"`
	Continue bool   `json:"continue"`
}

type Table struct {
	Key   string `json:"key"`
	Rules []Rule `json:"rules"`
}

type StatDef struct {
	Key          string  `json:"key"`
	Label        string  `json:"label"`
	Group        string  `json:"group"`
	Min          int64   `json:"min"`
	Max          int64   `json:"max"`
	Initial      int64   `json:"initial"`
	Visibility   string  `json:"visibility"`
	DecayPercent int64   `json:"decay_percent"`
	Daily        int64   `json:"daily"`
	Drift        int64   `json:"drift"`
	MinStage     int64   `json:"min_stage"`
	CapFlag      string  `json:"cap_flag"`
	CapMax       int64   `json:"cap_max"`
	Practice     []int64 `json:"practice"`
	Sum          []Term  `json:"sum"`
}

type Stage struct {
	Key     string           `json:"key"`
	Name    string           `json:"name"`
	Tier    string           `json:"tier"`
	MinDays int64            `json:"min_days"`
	MaxDays int64            `json:"max_days"`
	Rise    []Check          `json:"rise"`
	Push    []Check          `json:"push"`
	Caps    map[string]int64 `json:"caps"`
	Percent map[string]int64 `json:"percent"`
	OnEnter string           `json:"on_enter"`
}

type Slot struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Start       int64  `json:"start"`
	Half        int64  `json:"half"`
	WeekendOnly bool   `json:"weekend_only"`
	Mark        string `json:"mark"`
}

type Tag struct {
	Key   string   `json:"key"`
	Label string   `json:"label"`
	Tags  []string `json:"tags"`
}

type Lead struct {
	Key      string              `json:"key"`
	Name     string              `json:"name"`
	Age      int64               `json:"age"`
	Title    string              `json:"title"`
	Profile  map[string]string   `json:"profile"`
	Hidden   map[string]string   `json:"hidden"`
	Lists    map[string][]string `json:"lists"`
	Likes    []Tag               `json:"likes"`
	Dislikes []Tag               `json:"dislikes"`
	Params   map[string]int64    `json:"params"`
	Gain     map[string]int64    `json:"gain"`
	Favored  string              `json:"favored"`
}

type Category struct {
	Key           string `json:"key"`
	Contact       bool   `json:"contact"`
	Check         bool   `json:"check"`
	PerSlot       bool   `json:"per_slot"`
	Gates         []Gate `json:"gates"`
	RepeatDays    int64  `json:"repeat_days"`
	RepeatPercent int64  `json:"repeat_percent"`
	FreeFlag      string `json:"free_flag"`
	OnSuccess     string `json:"on_success"`
	Bonus         Effect `json:"bonus"`
}

type Activity struct {
	Key       string           `json:"key"`
	Label     string           `json:"label"`
	LabelList string           `json:"label_list"`
	Category  string           `json:"category"`
	Location  string           `json:"location"`
	Halves    int64            `json:"halves"`
	Slots     []string         `json:"slots"`
	MinStage  int64            `json:"min_stage"`
	Gates     []Gate           `json:"gates"`
	Cost      map[string]int64 `json:"cost"`
	Income    int64            `json:"income"`
	Cooldown  int64            `json:"cooldown"`
	Rate      int64            `json:"rate"`
	RateTerms []Term           `json:"rate_terms"`
	Tags      []string         `json:"tags"`
	Train     string           `json:"train"`
	Effects   Effect           `json:"effects"`
	Fail      Effect           `json:"fail"`
	Always    Effect           `json:"always"`
	Table     string           `json:"table"`
	After     string           `json:"after"`
}

type Location struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type Tuning struct {
	LikeRate    int64    `json:"like_rate"`
	DislikeRate int64    `json:"dislike_rate"`
	LikePercent int64    `json:"like_percent"`
	Reverse     []string `json:"reverse"`
	Dislike     Effect   `json:"dislike"`
	Landmine    Effect   `json:"landmine"`
	Fail        Effect   `json:"fail"`
	RateTerms   []Term   `json:"rate_terms"`
	Min         int64    `json:"min"`
	Max         int64    `json:"max"`
	Gates       []Gate   `json:"gates"`
}

type Income struct {
	Stat        string `json:"stat"`
	PercentFlag string `json:"percent_flag"`
	Each        int64  `json:"each"`
	Bump        string `json:"bump"`
	Tally       string `json:"tally"`
}

type MessageKind struct {
	Key    string  `json:"key"`
	Label  string  `json:"label"`
	Slot   int64   `json:"slot"`
	When   []Check `json:"when"`
	Chance int64   `json:"chance"`
	Expire Effect  `json:"expire"`
	Low    Effect  `json:"low"`
	Ok     Effect  `json:"ok"`
	Hit    Effect  `json:"hit"`
}

type Messages struct {
	Max     int64         `json:"max"`
	Kinds   []MessageKind `json:"kinds"`
	Dislike Effect        `json:"dislike"`
	Expired string        `json:"expired"`
}

type Adjust struct {
	When    []Test `json:"when"`
	Daily   int64  `json:"daily"`
	Percent int64  `json:"percent"`
}

type Rival struct {
	Key     string            `json:"key"`
	Name    string            `json:"name"`
	Start   int64             `json:"start"`
	Daily   int64             `json:"daily"`
	Adjust  []Adjust          `json:"adjust"`
	Profile map[string]string `json:"profile"`
}

type Band struct {
	Min   int64  `json:"min"`
	Label string `json:"label"`
}

type Choice struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Halves int64  `json:"halves"`
	When   []Test `json:"when"`
	Gate   Gate   `json:"gate"`
	Do     Effect `json:"do"`
	Table  string `json:"table"`
}

type Rivals struct {
	Chance  int64    `json:"chance"`
	Max     int64    `json:"max"`
	When    []Check  `json:"when"`
	Note    string   `json:"note"`
	Pool    []Rival  `json:"pool"`
	Adjust  []Adjust `json:"adjust"`
	Fall    string   `json:"fall"`
	Hints   []Band   `json:"hints"`
	Tactics []Choice `json:"tactics"`
}

type Pool struct {
	Key    string  `json:"key"`
	Chance int64   `json:"chance"`
	When   []Check `json:"when"`
}

type Event struct {
	Key      string  `json:"key"`
	Name     string  `json:"name"`
	Pool     string  `json:"pool"`
	MinStage int64   `json:"min_stage"`
	When     []Check `json:"when"`
	Check    []Check `json:"check"`
	Success  Effect  `json:"success"`
	Fail     Effect  `json:"fail"`
	Always   Effect  `json:"always"`
}

type Events struct {
	Pools  []Pool  `json:"pools"`
	Events []Event `json:"events"`
	Note   string  `json:"note"`
}

type Trip struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	Days    int64  `json:"days"`
	Cost    int64  `json:"cost"`
	Gates   []Gate `json:"gates"`
	Daily   Effect `json:"daily"`
	Effects Effect `json:"effects"`
}

type Travel struct {
	Max         int64    `json:"max"`
	CountFlag   string   `json:"count_flag"`
	CostStat    string   `json:"cost_stat"`
	Gates       []Gate   `json:"gates"`
	Segments    []string `json:"segments"`
	Fallback    string   `json:"fallback"`
	MaxSegments int64    `json:"max_segments"`
	Like        int64    `json:"like"`
	Dislike     int64    `json:"dislike"`
	DislikeDo   Effect   `json:"dislike_do"`
	Cap         int64    `json:"cap"`
	Floor       int64    `json:"floor"`
	Downgrade   string   `json:"downgrade"`
	To          string   `json:"to"`
	Unless      string   `json:"unless"`
	BusyOver    int64    `json:"busy_over"`
	Busy        Effect   `json:"busy"`
	DayCheck    []Check  `json:"day_check"`
	DayFail     Effect   `json:"day_fail"`
	Reply       Effect   `json:"reply"`
	Trips       []Trip   `json:"trips"`
}

type Deadline struct {
	Warning    int64  `json:"warning"`
	LeaveAfter int64  `json:"leave_after"`
	Max        int64  `json:"max"`
	Flag       string `json:"flag"`
	Delay      string `json:"delay"`
	Cancel     string `json:"cancel"`
	Active     string `json:"active"`
	None       string `json:"none"`
}

type Confess struct {
	Gates []Gate   `json:"gates"`
	Bonus []Term   `json:"bonus"`
	Args  []Choice `json:"args"`
	Clear []string `json:"clear"`
	Final string   `json:"final"`
	Table string   `json:"table"`
	Hook  string   `json:"hook"`
	List  string   `json:"list"`
	Then  string   `json:"then"`
}

type Scene struct {
	Key     string   `json:"key"`
	Title   string   `json:"title"`
	Options []Choice `json:"options"`
}

type ListDef struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Max      int64  `json:"max"`
	Roll     bool   `json:"roll"`
	Writable bool   `json:"writable"`
	Detail   bool   `json:"detail"`
	Chars    int64  `json:"chars"`
	Gates    []Gate `json:"gates"`
}

type Outcome struct {
	Key      string  `json:"key"`
	Label    string  `json:"label"`
	Success  bool    `json:"success"`
	Requires []Check `json:"requires"`
}

type Fact struct {
	Key   string `json:"key"`
	Ref   string `json:"ref"`
	Bool  bool   `json:"bool"`
	Bands []Band `json:"bands"`
}

type Text struct {
	Key      string  `json:"key"`
	Template string  `json:"template"`
	When     []Check `json:"when"`
	Else     string  `json:"else"`
}

type Reveal struct {
	Key  string `json:"key"`
	List string `json:"list"`
	Stat string `json:"stat"`
}

type Hooks struct {
	Start       string `json:"start"`
	Night       string `json:"night"`
	Morning     string `json:"morning"`
	Schedule    string `json:"schedule"`
	AfterAction string `json:"after_action"`
	Success     string `json:"success"`
}

type Config struct {
	Key          string          `json:"key"`
	Name         string          `json:"name"`
	World        string          `json:"world"`
	Goal         string          `json:"goal"`
	NoLead       string          `json:"no_lead"`
	ChoiceFields []string        `json:"choice_fields"`
	Stats        []StatDef       `json:"stats"`
	Stages       []Stage         `json:"stages"`
	Slots        []Slot          `json:"slots"`
	Leads        []Lead          `json:"leads"`
	Categories   []Category      `json:"categories"`
	Activities   []Activity      `json:"activities"`
	Locations    []Location      `json:"locations"`
	Tuning       Tuning          `json:"tuning"`
	Income       Income          `json:"income"`
	Contact      Effect          `json:"contact"`
	Messages     Messages        `json:"messages"`
	Rivals       Rivals          `json:"rivals"`
	Events       Events          `json:"events"`
	Travel       Travel          `json:"travel"`
	Deadline     Deadline        `json:"deadline"`
	Confess      Confess         `json:"confess"`
	Scenes       []Scene         `json:"scenes"`
	Reactions    []Choice        `json:"reactions"`
	Lists        []ListDef       `json:"lists"`
	Outcomes     []Outcome       `json:"outcomes"`
	Abandon      string          `json:"abandon"`
	Facts        []Fact          `json:"facts"`
	Texts        []Text          `json:"texts"`
	Reveals      []Reveal        `json:"reveals"`
	Conds        map[string]Cond `json:"conds"`
	Tables       []Table         `json:"tables"`
	Hooks        Hooks           `json:"hooks"`
}

func (c *Config) stat(key string) (StatDef, bool) {
	i := slices.IndexFunc(c.Stats, func(s StatDef) bool { return s.Key == key })
	if i < 0 {
		return StatDef{}, false
	}
	return c.Stats[i], true
}

func (c *Config) lead(key string) (Lead, bool) {
	i := slices.IndexFunc(c.Leads, func(l Lead) bool { return l.Key == key })
	if i < 0 {
		return Lead{}, false
	}
	return c.Leads[i], true
}

func (c *Config) table(key string) (Table, bool) {
	i := slices.IndexFunc(c.Tables, func(t Table) bool { return t.Key == key })
	if i < 0 {
		return Table{}, false
	}
	return c.Tables[i], true
}

func (c *Config) scene(key string) (Scene, bool) {
	i := slices.IndexFunc(c.Scenes, func(s Scene) bool { return s.Key == key })
	if i < 0 {
		return Scene{}, false
	}
	return c.Scenes[i], true
}

func (c *Config) activity(key string) (Activity, bool) {
	i := slices.IndexFunc(c.Activities, func(a Activity) bool { return a.Key == key })
	if i < 0 {
		return Activity{}, false
	}
	return c.Activities[i], true
}

func (c *Config) category(key string) Category {
	i := slices.IndexFunc(c.Categories, func(a Category) bool { return a.Key == key })
	if i < 0 {
		return Category{Key: key}
	}
	return c.Categories[i]
}

func (c *Config) rival(key string) (Rival, bool) {
	i := slices.IndexFunc(c.Rivals.Pool, func(r Rival) bool { return r.Key == key })
	if i < 0 {
		return Rival{}, false
	}
	return c.Rivals.Pool[i], true
}

func (c *Config) list(key string) (ListDef, bool) {
	i := slices.IndexFunc(c.Lists, func(l ListDef) bool { return l.Key == key })
	if i < 0 {
		return ListDef{}, false
	}
	return c.Lists[i], true
}

func (c *Config) outcome(key string) (Outcome, bool) {
	i := slices.IndexFunc(c.Outcomes, func(o Outcome) bool { return o.Key == key })
	if i < 0 {
		return Outcome{}, false
	}
	return c.Outcomes[i], true
}

// validator walks every reference in the config so packs fail at import time.
type validator struct {
	c    *Config
	errs []string
}

func (v *validator) fail(format string, args ...any) {
	if len(v.errs) < 8 {
		v.errs = append(v.errs, fmt.Sprintf(format, args...))
	}
}

// reservedProjectionKeys are top-level Project fields that must not be reused as
// StatDef.Group (or they would overwrite non-group projection values).
var reservedProjectionKeys = []string{
	"log", "stage", "lead", "day", "slot", "weekend", "outcome_key", "last_result",
	"facts", "caps", "messages", "rivals", "warning", "hidden", "stage_rank",
	"stage_key", "stage_day", "intimacy_tier", "choices", "goal", "pending_messages",
	"known_likes", "known_dislikes", "scene",
}

func placeholderKeys(text string) []string {
	keys := []string{}
	rest := text
	for {
		open := strings.IndexByte(rest, '{')
		if open < 0 {
			break
		}
		end := strings.IndexByte(rest[open:], '}')
		if end < 0 {
			break
		}
		keys = append(keys, rest[open+1:open+end])
		rest = rest[open+end+1:]
	}
	return keys
}

func (v *validator) fillKey(where, key string) {
	switch key {
	case "name", "stage_name", "slot_name", "label", "roll", "rival", "rival_name", "kind":
		return
	}
	if strings.HasPrefix(key, "text.") {
		if strings.TrimPrefix(key, "text.") == "" {
			v.fail("%s: 占位符 {%s} 无效", where, key)
		}
		return
	}
	v.ref(where, key)
}

func (v *validator) template(where, text string) {
	for _, key := range placeholderKeys(text) {
		v.fillKey(where, key)
	}
}

func (v *validator) ref(where, ref string) {
	if ref == "" {
		return
	}
	if strings.Contains(ref, "{") {
		// Dynamic refs such as push.{stage} defer fill; still check placeholders.
		v.template(where, ref)
		return
	}
	if _, ok := resolveRef(v.c, ref); !ok {
		v.fail("%s: 未知引用 %s", where, ref)
	}
}

func (v *validator) checks(where string, checks []Check) {
	for _, c := range checks {
		v.ref(where, c.Ref)
		v.ref(where, c.Than)
		v.terms(where, c.Plus)
		if !slices.Contains([]string{"", "ge", "le", "gt", "lt", "eq", "ne"}, c.Op) || c.Dice < 0 || c.Dice > 100 || c.Mod < 0 {
			v.fail("%s: 判定无效", where)
		}
	}
}

func (v *validator) terms(where string, terms []Term) {
	for _, t := range terms {
		v.ref(where, t.Ref)
		if t.Div < 0 {
			v.fail("%s: 除数无效", where)
		}
	}
}

func (v *validator) gates(where string, gates []Gate) {
	for _, g := range gates {
		if len(g.If) == 0 && g.Reason == "" {
			continue
		}
		v.checks(where, fullChecks(g.If))
		if g.Reason == "" {
			v.fail("%s: 门槛缺少原因", where)
		}
		v.template(where, g.Reason)
	}
}

func (v *validator) tableRef(where, key string) {
	if key == "" {
		return
	}
	if _, ok := v.c.table(key); !ok {
		v.fail("%s: 未定义规则表 %s", where, key)
	}
}

func (v *validator) effect(where string, e Effect) {
	for _, m := range []map[string]int64{e.Stats, e.Set} {
		for key := range m {
			if s, ok := v.c.stat(key); !ok || len(s.Sum) > 0 {
				v.fail("%s: 效果数值 %s 无效", where, key)
			}
		}
	}
	v.tableRef(where, e.Table)
	if e.Scene != "" {
		if _, ok := v.c.scene(e.Scene); !ok {
			v.fail("%s: 未定义场景 %s", where, e.Scene)
		}
	}
	if e.Outcome != "" {
		if _, ok := v.c.outcome(e.Outcome); !ok {
			v.fail("%s: 未定义结局 %s", where, e.Outcome)
		}
	}
	if e.Stage != "" && e.Stage != "next" && !slices.ContainsFunc(v.c.Stages, func(s Stage) bool { return s.Key == e.Stage }) {
		v.fail("%s: 未定义阶段 %s", where, e.Stage)
	}
	if e.Collect != "" {
		if _, ok := v.c.list(e.Collect); !ok {
			v.fail("%s: 未定义列表 %s", where, e.Collect)
		}
	}
	if !slices.Contains([]string{"", "final", "auto"}, e.Confess) {
		v.fail("%s: confess 取值无效", where)
	}
	v.template(where, e.Note)
}

func (v *validator) choices(where string, choices []Choice) {
	seen := map[string]bool{}
	for _, c := range choices {
		if !contract.ValidStableKey(c.Key) || c.Label == "" || seen[c.Key] || c.Halves < 0 || c.Halves > 6 {
			v.fail("%s: 选项 %s 无效", where, c.Key)
		}
		seen[c.Key] = true
		v.template(where+"."+c.Key, c.Label)
		v.checks(where+"."+c.Key, fullChecks(c.When))
		v.gates(where+"."+c.Key, []Gate{c.Gate})
		v.effect(where+"."+c.Key, c.Do)
		v.tableRef(where+"."+c.Key, c.Table)
	}
}

func unique[T any](v *validator, kind string, items []T, key func(T) string) {
	seen := map[string]bool{}
	for _, item := range items {
		k := key(item)
		if !contract.ValidStableKey(k) || seen[k] {
			v.fail("%s 标识重复或无效: %s", kind, k)
		}
		seen[k] = true
	}
}

func validateConfig(c Config) error {
	v := &validator{c: &c}
	if !contract.ValidStableKey(c.Key) || c.Name == "" {
		v.fail("基础配置无效")
	}
	if len(c.Stats) == 0 || len(c.Stages) == 0 || len(c.Slots) == 0 || len(c.Leads) == 0 || len(c.Outcomes) == 0 {
		v.fail("数值、阶段、时段、角色与结局都至少需要一项")
	}
	unique(v, "数值", c.Stats, func(s StatDef) string { return s.Key })
	unique(v, "阶段", c.Stages, func(s Stage) string { return s.Key })
	unique(v, "时段", c.Slots, func(s Slot) string { return s.Key })
	unique(v, "角色", c.Leads, func(s Lead) string { return s.Key })
	unique(v, "分类", c.Categories, func(s Category) string { return s.Key })
	unique(v, "行动", c.Activities, func(s Activity) string { return s.Key })
	unique(v, "地点", c.Locations, func(s Location) string { return s.Key })
	unique(v, "情敌", c.Rivals.Pool, func(s Rival) string { return s.Key })
	unique(v, "事件", c.Events.Events, func(s Event) string { return s.Key })
	unique(v, "旅行", c.Travel.Trips, func(s Trip) string { return s.Key })
	unique(v, "场景", c.Scenes, func(s Scene) string { return s.Key })
	unique(v, "列表", c.Lists, func(s ListDef) string { return s.Key })
	unique(v, "结局", c.Outcomes, func(s Outcome) string { return s.Key })
	unique(v, "规则表", c.Tables, func(s Table) string { return s.Key })
	unique(v, "消息", c.Messages.Kinds, func(s MessageKind) string { return s.Key })
	statKeys := map[string]bool{}
	for _, s := range c.Stats {
		statKeys[s.Key] = true
	}
	for _, s := range c.Stats {
		if s.Min > s.Max || s.Initial < s.Min || s.Initial > s.Max || !slices.Contains([]string{"player", "model", "hidden"}, s.Visibility) || s.DecayPercent < 0 || s.DecayPercent > 100 || s.Drift < 0 {
			v.fail("数值 %s 范围无效", s.Key)
		}
		if s.Group != "" {
			if slices.Contains(reservedProjectionKeys, s.Group) || statKeys[s.Group] {
				v.fail("数值 %s 的分组 %s 与投影保留键冲突", s.Key, s.Group)
			}
		}
		v.template("数值 "+s.Key, s.Label)
		v.terms("数值 "+s.Key, s.Sum)
	}
	for _, s := range c.Stages {
		if s.Name == "" || s.MinDays < 0 || s.MaxDays < s.MinDays {
			v.fail("阶段 %s 无效", s.Key)
		}
		v.template("阶段 "+s.Key, s.Name)
		v.checks("阶段 "+s.Key, s.Rise)
		v.checks("阶段 "+s.Key, s.Push)
		v.tableRef("阶段 "+s.Key, s.OnEnter)
	}
	for _, s := range c.Slots {
		if s.Label == "" || s.Start < 0 || s.Half < 1 {
			v.fail("时段 %s 无效", s.Key)
		}
		v.template("时段 "+s.Key, s.Label)
	}
	for _, l := range c.Leads {
		if l.Name == "" || l.Age < 18 {
			v.fail("角色 %s 必须是有名字的成年人", l.Key)
		}
		for _, t := range append(slices.Clone(l.Likes), l.Dislikes...) {
			if !contract.ValidStableKey(t.Key) || t.Label == "" {
				v.fail("角色 %s 喜好或雷区无效", l.Key)
			}
		}
		if l.Favored != "" {
			if _, ok := c.rival(l.Favored); !ok {
				v.fail("角色 %s 关联情敌未定义", l.Key)
			}
		}
	}
	for _, cat := range c.Categories {
		v.gates("分类 "+cat.Key, cat.Gates)
		v.tableRef("分类 "+cat.Key, cat.OnSuccess)
		v.effect("分类 "+cat.Key, cat.Bonus)
	}
	locations := map[string]bool{}
	for _, l := range c.Locations {
		locations[l.Key] = true
	}
	for _, a := range c.Activities {
		w := "行动 " + a.Key
		if a.Label == "" || a.Halves < 1 || a.Halves > 6 || a.Rate < 0 || a.Rate > 100 || len(a.Slots) == 0 || a.MinStage < 0 || a.MinStage >= int64(len(c.Stages)) {
			v.fail("%s 配置无效", w)
		}
		if a.Location != "" && !locations[a.Location] {
			v.fail("%s 地点未定义", w)
		}
		for _, slot := range a.Slots {
			if !slices.ContainsFunc(c.Slots, func(s Slot) bool { return s.Key == slot }) {
				v.fail("%s 时段无效", w)
			}
		}
		for key, value := range a.Cost {
			if _, ok := c.stat(key); !ok || value < 0 {
				v.fail("%s 花费无效", w)
			}
		}
		if a.Train != "" {
			if _, ok := c.stat(a.Train); !ok {
				v.fail("%s 训练数值无效", w)
			}
		}
		v.template(w, a.Label)
		v.template(w, a.LabelList)
		v.gates(w, a.Gates)
		v.terms(w, a.RateTerms)
		v.effect(w, a.Effects)
		v.effect(w, a.Fail)
		v.effect(w, a.Always)
		v.tableRef(w, a.Table)
		v.tableRef(w, a.After)
	}
	v.terms("tuning", c.Tuning.RateTerms)
	v.gates("tuning", c.Tuning.Gates)
	v.effect("tuning", c.Tuning.Dislike)
	v.effect("tuning", c.Tuning.Landmine)
	v.effect("tuning", c.Tuning.Fail)
	if c.Tuning.Min > c.Tuning.Max {
		v.fail("成功率上下限无效")
	}
	if c.Income.Stat != "" {
		v.ref("income", "stat."+c.Income.Stat)
		if c.Income.Bump != "" {
			v.ref("income", "stat."+c.Income.Bump)
		}
	}
	v.effect("contact", c.Contact)
	for _, k := range c.Messages.Kinds {
		w := "消息 " + k.Key
		if k.Slot < 0 || k.Slot >= int64(len(c.Slots)) || k.Label == "" {
			v.fail("%s 无效", w)
		}
		v.template(w, k.Label)
		v.checks(w, k.When)
		for _, e := range []Effect{k.Expire, k.Low, k.Ok, k.Hit} {
			v.effect(w, e)
		}
	}
	v.effect("消息", c.Messages.Dislike)
	v.template("消息", c.Messages.Expired)
	v.checks("情敌", c.Rivals.When)
	v.template("情敌", c.Rivals.Note)
	v.tableRef("情敌", c.Rivals.Fall)
	for _, a := range c.Rivals.Adjust {
		v.checks("情敌", fullChecks(a.When))
	}
	for _, r := range c.Rivals.Pool {
		if r.Name == "" || r.Start < 0 || r.Start > 100 {
			v.fail("情敌 %s 无效", r.Key)
		}
		v.template("情敌 "+r.Key, r.Name)
		for _, a := range r.Adjust {
			v.checks("情敌 "+r.Key, fullChecks(a.When))
		}
	}
	v.choices("情敌手段", c.Rivals.Tactics)
	for _, p := range c.Events.Pools {
		v.checks("事件池 "+p.Key, p.When)
	}
	v.template("事件", c.Events.Note)
	for _, e := range c.Events.Events {
		w := "事件 " + e.Key
		if e.Name == "" || !slices.ContainsFunc(c.Events.Pools, func(p Pool) bool { return p.Key == e.Pool }) {
			v.fail("%s 无效", w)
		}
		v.template(w, e.Name)
		v.checks(w, e.When)
		v.checks(w, e.Check)
		v.effect(w, e.Success)
		v.effect(w, e.Fail)
		v.effect(w, e.Always)
	}
	t := c.Travel
	if len(t.Trips) > 0 {
		v.ref("旅行", "stat."+t.CostStat)
		if t.Unless != "" {
			v.ref("旅行", "cond."+t.Unless)
		}
		v.gates("旅行", t.Gates)
		v.checks("旅行", t.DayCheck)
		for _, e := range []Effect{t.DislikeDo, t.Busy, t.DayFail, t.Reply} {
			v.effect("旅行", e)
		}
		if t.MaxSegments < 1 || t.MaxSegments > 12 || t.Floor > t.Cap || !slices.Contains(t.Segments, t.Fallback) {
			v.fail("旅行设置无效")
		}
	}
	for _, trip := range t.Trips {
		if trip.Name == "" || trip.Days < 1 || trip.Days > 6 || trip.Cost < 0 {
			v.fail("旅行 %s 无效", trip.Key)
		}
		v.template("旅行 "+trip.Key, trip.Name)
		v.gates("旅行 "+trip.Key, trip.Gates)
		v.effect("旅行 "+trip.Key, trip.Daily)
		v.effect("旅行 "+trip.Key, trip.Effects)
	}
	d := c.Deadline
	if d.Max < 2 || (d.Warning > 0 && (d.LeaveAfter < 1 || d.Flag == "")) {
		v.fail("期限设置无效")
	}
	v.template("期限", d.Active)
	v.template("期限", d.None)
	v.gates("告白", c.Confess.Gates)
	v.terms("告白", c.Confess.Bonus)
	v.tableRef("告白", c.Confess.Table)
	v.tableRef("告白", c.Confess.Then)
	v.template("告白", c.Confess.Hook)
	if c.Confess.Final != "" {
		v.ref("告白", "cond."+c.Confess.Final)
	}
	if c.Confess.List != "" {
		v.ref("告白", "list."+c.Confess.List)
	}
	if len(c.Confess.Args) > 3 {
		v.fail("告白最多 3 个可选助力")
	}
	v.choices("告白助力", c.Confess.Args)
	for _, arg := range c.Confess.Args {
		if !slices.Contains(aidKeys, arg.Key) {
			v.fail("告白助力键必须是 aid_1–aid_3")
		}
	}
	for _, s := range c.Scenes {
		if s.Title == "" || len(s.Options) == 0 {
			v.fail("场景 %s 无效", s.Key)
		}
		v.template("场景 "+s.Key, s.Title)
		v.choices("场景 "+s.Key, s.Options)
	}
	v.choices("反应", c.Reactions)
	topKeys := map[string]bool{}
	for _, key := range reservedProjectionKeys {
		topKeys[key] = true
	}
	for _, s := range c.Stats {
		topKeys[s.Key] = true
	}
	for _, l := range c.Lists {
		if l.Label == "" || l.Max < 1 || l.Max > 32 || l.Chars < 0 {
			v.fail("列表 %s 无效", l.Key)
		}
		if topKeys[l.Key] {
			v.fail("列表 %s 与投影保留键冲突", l.Key)
		}
		topKeys[l.Key] = true
		v.template("列表 "+l.Key, l.Label)
		v.gates("列表 "+l.Key, l.Gates)
	}
	for _, o := range c.Outcomes {
		if o.Label == "" {
			v.fail("结局 %s 缺少名称", o.Key)
		}
		v.template("结局 "+o.Key, o.Label)
		v.checks("结局 "+o.Key, o.Requires)
	}
	if _, ok := c.outcome(c.Abandon); !ok {
		v.fail("放弃结局未定义")
	} else if o, _ := c.outcome(c.Abandon); o.Success {
		v.fail("放弃结局必须是失败结局")
	}
	unique(v, "事实", c.Facts, func(s Fact) string { return s.Key })
	if len(c.Facts) > 12 {
		v.fail("叙事事实最多 12 项")
	}
	for _, f := range c.Facts {
		v.ref("事实 "+f.Key, f.Ref)
	}
	for _, x := range c.Texts {
		if topKeys[x.Key] {
			v.fail("文本 %s 与投影保留键冲突", x.Key)
		}
		topKeys[x.Key] = true
		v.template("文本 "+x.Key, x.Template)
		v.template("文本 "+x.Key, x.Else)
		v.checks("文本 "+x.Key, x.When)
	}
	for _, r := range c.Reveals {
		if topKeys[r.Key] {
			v.fail("揭示 %s 与投影保留键冲突", r.Key)
		}
		topKeys[r.Key] = true
		v.ref("揭示 "+r.Key, "stat."+r.Stat)
	}
	for _, s := range c.Stats {
		if s.Group != "" && topKeys[s.Group] {
			v.fail("数值 %s 的分组 %s 与投影保留键冲突", s.Key, s.Group)
		}
	}
	for key, cond := range c.Conds {
		v.checks("条件 "+key, cond.All)
		v.checks("条件 "+key, cond.Any)
	}
	for _, table := range c.Tables {
		for i, rule := range table.Rules {
			w := fmt.Sprintf("规则表 %s#%d", table.Key, i)
			v.checks(w, fullChecks(rule.When))
			v.effect(w, rule.Do)
			if rule.Chance < 0 || rule.Chance > 100 {
				v.fail("%s 概率无效", w)
			}
		}
	}
	for _, key := range []string{c.Hooks.Start, c.Hooks.Night, c.Hooks.Morning, c.Hooks.Schedule, c.Hooks.AfterAction, c.Hooks.Success} {
		v.tableRef("钩子", key)
	}
	if len(v.errs) > 0 {
		return fmt.Errorf("养成规则无效: %s", strings.Join(v.errs, "；"))
	}
	return nil
}

func ValidateContent(content contract.Content) error {
	config, err := ruledata.Singleton[Config](content, DefinitionKind)
	if err != nil {
		return err
	}
	if err := validateConfig(config); err != nil {
		return err
	}
	for _, o := range config.Outcomes {
		if !slices.ContainsFunc(content.Outcomes, func(outcome contract.Outcome) bool { return outcome.Key == o.Key && outcome.Enabled }) {
			return fmt.Errorf("缺少已启用结局 %s", o.Key)
		}
	}
	return nil
}

func Descriptor() contract.ModuleDescriptor {
	labels := map[string]string{"key": "标识", "name": "名称", "world": "世界观", "goal": "目标", "stats": "数值", "stages": "阶段", "slots": "时段", "leads": "角色", "categories": "行动分类", "activities": "行动", "locations": "地点", "tuning": "成功率调校", "income": "收入", "contact": "联系效果", "messages": "限时消息", "rivals": "情敌", "events": "随机事件", "travel": "旅行", "deadline": "期限", "confess": "告白", "scenes": "场景", "reactions": "边界反应", "lists": "记忆列表", "outcomes": "结局", "abandon": "放弃结局", "facts": "叙事事实", "texts": "文本", "reveals": "渐进揭示", "conds": "命名条件", "tables": "规则表", "hooks": "钩子"}
	return contract.ModuleDescriptor{DefinitionSchemas: map[string]map[string]any{DefinitionKind: ruledata.Schema[Config](labels)}, DefinitionLabels: map[string]string{DefinitionKind: "恋爱养成规则"}, Key: EngineKey, Version: 1, Name: "恋爱养成", Category: "relationship", Description: "数据驱动的阶段制养成引擎：数值、阶段、告白、情敌与结局全部由规则包声明，平台裁定，模型只负责叙事。", Modes: []string{contract.RuntimeModeChallenge}, Actions: []string{"update_state", "domain_action", "complete_challenge"}, Verifiers: []string{"core.conditions"}, SettlementHandlers: []string{"core.challenge_completion"}, Runtime: Module{}, AbandonAction: "romance.abandon", DefinitionValidators: map[string]contract.DefinitionValidator{DefinitionKind: func(definition contract.Definition) error {
		config, err := ruledata.Decode[Config](definition.Content)
		if err != nil {
			return err
		}
		return validateConfig(config)
	}}, ValidateContent: ValidateContent}
}
