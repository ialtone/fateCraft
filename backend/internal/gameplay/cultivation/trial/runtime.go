package trial

import (
	"fmt"
	"github.com/ialtone/fatecraft/internal/gameplay/components/ruledata"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"slices"
)

type Tribulation struct {
	Attempt        int64 `json:"attempt"`
	Active         bool  `json:"active"`
	Wave           int64 `json:"wave"`
	Failures       int64 `json:"failures"`
	ProtectedPhase int64 `json:"protected_phase"`
	Ward           bool  `json:"ward"`
}
type State struct {
	Birth             string           `json:"birth"`
	AgeAtBirth        int64            `json:"age_at_birth"`
	Vigor             int64            `json:"vigor"`
	Insight           int64            `json:"insight"`
	Luck              int64            `json:"luck"`
	Realm             int              `json:"realm"`
	Progress          int64            `json:"progress"`
	Method            string           `json:"method"`
	HP                int64            `json:"hp"`
	Injury            int64            `json:"injury"`
	Karma             int64            `json:"karma"`
	Stock             map[string]int64 `json:"stock"`
	Completed         map[string]int64 `json:"completed"`
	LifePills         int64            `json:"life_pills"`
	AffinityQualified bool             `json:"affinity_qualified"`
	AffinityAttempts  int64            `json:"affinity_attempts"`
	Token             bool             `json:"token"`
	TokenUsed         bool             `json:"token_used"`
	Rift              bool             `json:"rift"`
	RiftCrossed       bool             `json:"rift_crossed"`
	CooldownUntil     int64            `json:"cooldown_until"`
	Tribulation       Tribulation      `json:"tribulation"`
	DeathCause        string           `json:"death_cause"`
}
type Module struct{}

func (Module) NarrativeFactPaths() []string {
	return []string{"realm", "realm_key", "progress", "health", "injury", "age", "lifespan", "method", "outcome_key"}
}

func read(content contract.Content, run contract.RunState) (Config, State, error) {
	c, err := ruledata.Singleton[Config](content, DefinitionKind)
	if err != nil {
		return c, State{}, err
	}
	s, err := ruledata.Decode[State](run.Data["life"])
	return c, s, err
}
func lifeRemaining(c Config, s State, run contract.RunState) int64 {
	return (c.Realms[s.Realm].LifespanYears+s.LifePills*10)*Year - s.AgeAtBirth - run.GameTime
}
func method(c Config, s State) Method {
	for _, m := range c.Methods {
		if m.Key == s.Method {
			return m
		}
	}
	return Method{}
}
func clamp(n, lo, hi int64) int64 { return max(lo, min(hi, n)) }
func chance(c Config, s State, base, stat int64) int64 {
	return clamp(base+(stat-65)/3-s.Injury/5-s.Karma/2-c.Difficulty, 5, 95)
}
func waveChance(c Config, s State) int64 {
	return chance(c, s, c.Routes.TribulationChance+int64(s.Realm-5)*3-(s.Tribulation.Wave-1)/3*3, s.Vigor)
}
func phase(wave int64) int64 { return (wave-1)/3 + 1 }

func (Module) Initialize(content contract.Content, input contract.InitialInput) (contract.RunState, error) {
	c, err := ruledata.Singleton[Config](content, DefinitionKind)
	if err != nil {
		return contract.RunState{}, err
	}
	if err = validateConfig(c); err != nil {
		return contract.RunState{}, err
	}
	var birth *Birth
	for i := range c.Births {
		if c.Births[i].Key == input.PersonaKey {
			birth = &c.Births[i]
			break
		}
	}
	if birth == nil || input.Random == nil {
		return contract.RunState{}, fmt.Errorf("出身没有可信配置或缺少随机源")
	}
	values := []int64{}
	for _, p := range []string{"birth.age", "birth.vigor", "birth.insight", "birth.luck"} {
		upper := int64(21)
		if p == "birth.age" {
			upper = 8
		}
		v, e := input.Random.Intn(p, upper)
		if e != nil {
			return contract.RunState{}, e
		}
		values = append(values, v)
	}
	s := State{Birth: birth.Key, AgeAtBirth: (18 + values[0]) * Year, Vigor: 55 + values[1] + birth.VigorBonus, Insight: 55 + values[2] + birth.InsightBonus, Luck: 55 + values[3] + birth.LuckBonus, HP: 100, Stock: map[string]int64{}, Completed: map[string]int64{}}
	for _, key := range itemKeys {
		s.Stock[key] = birth.Stock[key]
	}
	data, err := ruledata.Object(s)
	return contract.RunState{SchemaVersion: 2, Status: contract.RunActive, Data: map[string]any{"life": data}}, err
}

func (Module) ValidateState(content contract.Content, run contract.RunState) error {
	c, s, err := read(content, run)
	if err != nil {
		return err
	}
	if run.SchemaVersion != 2 || len(run.Data) != 1 || s.Realm < 0 || s.Realm >= len(c.Realms) || s.Progress < 0 || s.Progress > 100 || s.HP < 0 || s.HP > 100 || s.Injury < 0 || s.Injury > 100 || s.Karma < -20 || s.Karma > 20 || s.LifePills < 0 || s.LifePills > 3 || s.AffinityAttempts < 0 || s.AffinityAttempts > c.Routes.AffinityAttempts || s.CooldownUntil < 0 || s.CooldownUntil > run.GameTime+694*Day {
		return fmt.Errorf("单局状态数值或版本无效")
	}
	var birth *Birth
	for i := range c.Births {
		if c.Births[i].Key == s.Birth {
			birth = &c.Births[i]
		}
	}
	if birth == nil || s.AgeAtBirth < 18*Year || s.AgeAtBirth > 25*Year || s.AgeAtBirth%Year != 0 {
		return fmt.Errorf("出生事实无效")
	}
	for _, v := range [][2]int64{{s.Vigor, birth.VigorBonus}, {s.Insight, birth.InsightBonus}, {s.Luck, birth.LuckBonus}} {
		if v[0] < 55+v[1] || v[0] > 75+v[1] {
			return fmt.Errorf("天赋属性超出出生范围")
		}
	}
	if err = ruledata.ValidateStock(s.Stock); err != nil {
		return err
	}
	if len(s.Stock) != len(itemKeys) {
		return fmt.Errorf("物品状态不完整")
	}
	for k := range s.Stock {
		if !slices.Contains(itemKeys, k) {
			return fmt.Errorf("未知物品")
		}
	}
	if s.Completed == nil || len(s.Completed) > len(c.Activities) {
		return fmt.Errorf("历练事实超出预算")
	}
	qualified, learned := false, false
	for k, n := range s.Completed {
		found := false
		for _, a := range c.Activities {
			if a.Key == k {
				found = true
				if n < 1 || n > 1000000 || a.Limit > 0 && n > a.Limit {
					return fmt.Errorf("历练次数无效")
				}
				qualified = qualified || a.Kind == "affinity"
				learned = learned || a.Kind == "inheritance" && a.MethodKey == s.Method
			}
		}
		if !found {
			return fmt.Errorf("未知历练事实")
		}
	}
	if s.AffinityQualified != qualified || s.Method != "" && (!learned || method(c, s).Key == "") || s.Realm > 0 && s.Method == "" || s.Method == "" && s.Progress > 0 {
		return fmt.Errorf("传承或资格缺少合法来源")
	}
	if (s.Token || s.TokenUsed) && (!s.AffinityQualified || s.AffinityAttempts < 1 || s.Realm < 3) || s.Token && s.TokenUsed || s.RiftCrossed && s.Realm < 4 || s.RiftCrossed && s.Rift {
		return fmt.Errorf("路线事实不一致")
	}
	t := s.Tribulation
	if t.Attempt < 0 || t.Attempt > 1000000 || t.Wave < 0 || t.Wave > 10 || t.Failures < 0 || t.Failures > 1000000 || t.ProtectedPhase < 0 || t.ProtectedPhase > 3 || t.Attempt == 0 && (t.Wave != 0 || t.Ward || t.Active || t.Failures != 0 || t.ProtectedPhase != 0) || t.Attempt > 0 && (s.Realm < 5 || t.Wave < 1) || t.Active && (t.Wave > 9 || run.Status != contract.RunActive) || t.ProtectedPhase > 0 && t.ProtectedPhase > phase(t.Wave) {
		return fmt.Errorf("雷劫实例或波次无效")
	}
	remaining := lifeRemaining(c, s, run)
	if run.Status == contract.RunActive && (remaining <= 0 || s.HP <= 0 || s.DeathCause != "" || s.TokenUsed || s.RiftCrossed || t.Wave == 10) {
		return fmt.Errorf("终局事实未结束")
	}
	if run.Status != contract.RunActive {
		valid := false
		switch run.OutcomeKey {
		case "affinity_ascension":
			valid = s.TokenUsed && run.Status == contract.RunCompleted
		case "void_ascension":
			valid = s.RiftCrossed && run.Status == contract.RunCompleted
		case "orthodox_ascension":
			valid = t.Attempt > 0 && t.Wave == 10 && !t.Active && run.Status == contract.RunCompleted
		case "lifespan_exhausted":
			valid = remaining == 0 && s.DeathCause == "lifespan" && run.Status == contract.RunFailed
		case "abandoned":
			valid = s.DeathCause == "" && remaining > 0 && s.HP > 0 && run.Status == contract.RunFailed
		case "peaceful_passing":
			valid = s.DeathCause == "voluntary" && remaining > 0 && remaining <= 5*Year && run.Status == contract.RunFailed
		case "perished":
			valid = s.HP == 0 && slices.Contains([]string{"combat", "tribulation", "void_rift"}, s.DeathCause) && run.Status == contract.RunFailed
		}
		if !valid || run.Status == contract.RunCompleted && (s.HP == 0 || remaining <= 0 || s.DeathCause != "") {
			return fmt.Errorf("结局缺少本世可信事实")
		}
	}
	return nil
}

func (Module) Actions() []contract.RuntimeAction {
	selectKey := func(label string) []contract.RuntimeParameter {
		return []contract.RuntimeParameter{{Key: "key", Label: label, Type: "string", Required: true}}
	}
	a := []contract.RuntimeAction{
		{Key: "trial.activity", Label: "历练与求法", Description: "按当前境界参与谋生、探索、战斗、交涉或获取首部传承", Parameters: selectKey("历练")},
		{Key: "trial.practice", Label: "闭关修行", Description: "投入灵石和时间积累本境界修为，伤重时须先休整"},
		{Key: "trial.breakthrough", Label: "冲击瓶颈", Description: "圆满后消耗破境丹与灵石，失败损失修为并受伤"},
		{Key: "trial.buy", Label: "购买补给", Description: "从坊市买入一份物品", Parameters: selectKey("物品")},
		{Key: "trial.sell", Label: "出售物品", Description: "按购价一半（向下取整）出售一份物品", Parameters: selectKey("物品")},
		{Key: "trial.craft", Label: "炼制", Description: "消耗配方材料和时间，失败也会消耗材料", Parameters: selectKey("配方")},
		{Key: "trial.consume", Label: "服用丹药", Description: "治疗或有限延寿，延寿丹每世最多三枚", Parameters: selectKey("丹药")},
		{Key: "trial.rest", Label: "休整疗伤", Description: "七日静养恢复生命与伤势，不产出资源"},
		{Key: "trial.wait", Label: "等待一年", Description: "明确等待一年，不获得资源；寿元不足一年将寿尽"},
		{Key: "trial.affinity_seek", Label: "寻求仙缘", Description: "完成仙缘委托后，在结丹及以上消耗一次有限机会寻符"},
		{Key: "trial.affinity_use", Label: "使用仙缘符", Description: "由玩家决定消耗已获得的仙缘符，结束此世"},
		{Key: "trial.rift_search", Label: "寻找虚空裂缝", Description: "元婴后投入资源探查空间，失败后需要等待冷却"},
		{Key: "trial.rift_enter", Label: "穿越虚空裂缝", Description: "由玩家决定穿越已发现的裂缝；定空锚会消耗并改善风险"},
		{Key: "trial.tribulation_begin", Label: "引动天劫", Description: "化神后由玩家开始新的九波雷劫，消耗灵石和一件护体法宝"},
		{Key: "trial.tribulation_wave", Label: "承受下一波", Description: "只结算当前雷劫当前波；失败可付费重试或撤退，致命伤害会结束此世"},
		{Key: "trial.tribulation_retreat", Label: "撤离雷劫", Description: "放弃本次波次进度，受伤并进入准备冷却；再次引劫从第一波开始"},
		{Key: "trial.abandon", Label: "提前终止本世", Description: "由玩家提前结束本世，不获得通关积分"},
		{Key: "trial.pass_away", Label: "安排后事并坐化", Description: "剩余寿元不超过五年时由玩家选择终结此世，无积分奖励"},
	}
	for i := range a {
		a[i].Actors = []string{"player", "model"}
		if slices.Contains([]string{"trial.affinity_use", "trial.rift_enter", "trial.tribulation_begin", "trial.tribulation_retreat", "trial.pass_away", "trial.abandon"}, a[i].Key) {
			a[i].Actors = []string{"player"}
		}
		a[i].Ending = slices.Contains([]string{"trial.affinity_use", "trial.rift_enter", "trial.pass_away", "trial.abandon"}, a[i].Key)
		a[i].Writes = []string{"life"}
		a[i].Events = []string{"trial.acted", "trial.ended"}
	}
	return a
}

type option struct {
	plan        contract.ActionPlan
	activity    *Activity
	item        *Item
	recipe      *Recipe
	probability int64
}

func options(c Config, s State, run contract.RunState, key string) ([]option, string) {
	out := []option{}
	add := func(label, arg string, minutes int64, cost map[string]int64, risk string, prob int64) bool {
		if !ruledata.CanPay(s.Stock, cost) {
			return false
		}
		args := map[string]any{}
		if arg != "" {
			args["key"] = arg
		}
		if minutes >= lifeRemaining(c, s, run) {
			risk += "；本次耗时将耗尽剩余寿元，届时停止行动并结束此世"
		}
		out = append(out, option{plan: contract.ActionPlan{Label: label, Arguments: args, Minutes: minutes, Costs: cost, Risk: risk}, probability: prob})
		return true
	}
	if s.Tribulation.Active && !slices.Contains([]string{"trial.tribulation_wave", "trial.tribulation_retreat", "trial.consume", "trial.abandon"}, key) {
		return out, "正在渡劫，只能承受当前波、服药或撤退"
	}
	realm := c.Realms[s.Realm]
	switch key {
	case "trial.activity":
		for _, a := range c.Activities {
			if s.Realm < a.MinRealm || a.Limit > 0 && s.Completed[a.Key] >= a.Limit || a.Kind == "inheritance" && s.Method != "" {
				continue
			}
			p := chance(c, s, a.Chance, s.Luck)
			if a.Kind == "work" && a.Chance == 100 {
				p = 100
			}
			risk := fmt.Sprintf("成功率 %d%%；成功获得灵石 %d、灵草 %d；失败伤害 %d", p, a.Stones*int64(s.Realm+1), a.Herbs, a.Damage)
			if a.Kind == "inheritance" {
				risk += "；成功后获得对应传承"
			}
			if add(a.Name, a.Key, a.Days*Day, map[string]int64{"stone": a.Cost}, risk, p) {
				out[len(out)-1].activity = &a
			}
		}
	case "trial.practice":
		if s.Method == "" {
			return out, "先通过历练取得首部传承"
		}
		if s.Progress >= 100 || s.Injury >= 50 {
			return out, "修为已满或伤势过重，需要突破或休整"
		}
		gain := min(100-s.Progress, realm.PracticeGain+method(c, s).GrowthBonus+(s.Insight-55)/10)
		add("闭关修行", "", realm.PracticeDays*Day, map[string]int64{"stone": realm.PracticeCost}, fmt.Sprintf("修为增加 %d，至多达到本境界圆满", gain), 100)
	case "trial.breakthrough":
		if s.Realm == 8 || s.Progress != 100 || s.Injury >= 30 {
			return out, "需要本境界圆满、伤势低于 30，且尚有下一境界"
		}
		p := chance(c, s, realm.BreakthroughChance, s.Insight)
		add("冲击瓶颈", "", 30*Day, map[string]int64{"breakthrough-pill": 1, "stone": realm.PracticeCost}, fmt.Sprintf("成功率 %d%%；失败损失 20 修为、15 生命并增加 10 伤势", p), p)
	case "trial.buy", "trial.sell":
		for _, item := range c.Items {
			if item.Key == "stone" || s.Realm < item.MinRealm {
				continue
			}
			cost := map[string]int64{"stone": item.Price}
			risk := "购入一份补给"
			if key == "trial.sell" {
				if item.Price < 2 {
					continue
				}
				cost = map[string]int64{item.Key: 1}
				risk = fmt.Sprintf("获得 %d 灵石", item.Price/2)
			}
			if add(item.Name, item.Key, 60, cost, risk, 100) {
				out[len(out)-1].item = &item
			}
		}
	case "trial.craft":
		for _, r := range c.Recipes {
			if s.Realm < r.MinRealm {
				continue
			}
			p := chance(c, s, r.Chance, s.Insight)
			if add(r.Name, r.Key, r.Days*Day, r.Inputs, fmt.Sprintf("成功率 %d%%；失败材料仍消耗", p), p) {
				out[len(out)-1].recipe = &r
			}
		}
	case "trial.consume":
		for _, item := range c.Items {
			if item.Key == "healing-pill" && s.HP < 100 || item.Key == "life-pill" && s.LifePills < 3 && !s.Tribulation.Active {
				risk := "恢复 40 生命，不消除伤势"
				if item.Key == "life-pill" {
					risk = "增加十年寿元上限，每世最多三枚"
				}
				if add(item.Name, item.Key, 10, map[string]int64{item.Key: 1}, risk, 100) {
					out[len(out)-1].item = &item
				}
			}
		}
	case "trial.rest":
		if s.HP == 100 && s.Injury == 0 {
			return out, "当前无需恢复"
		}
		add("静养七日", "", 7*Day, nil, "恢复 35 生命、减少 25 伤势", 100)
	case "trial.wait":
		add("等待一年", "", Year, nil, "只推进时间，不获得资源", 100)
	case "trial.affinity_seek":
		if s.Realm < 3 || !s.AffinityQualified || s.AffinityAttempts >= c.Routes.AffinityAttempts || s.Token {
			return out, "需要结丹、完成仙缘委托、尚有机会且未持有仙缘符"
		}
		p := chance(c, s, c.Routes.AffinityChance, s.Luck)
		add("赴仙缘约", "", 90*Day, map[string]int64{"stone": 10}, fmt.Sprintf("成功率 %d%%；成功失败均消耗一次机会（剩余 %d 次）", p, c.Routes.AffinityAttempts-s.AffinityAttempts), p)
	case "trial.affinity_use":
		if s.Realm < 3 || !s.Token {
			return out, "需要结丹及已取得的仙缘符"
		}
		add("消耗仙缘符飞升", "", 10, nil, "使用后本世结束", 100)
	case "trial.rift_search":
		if s.Realm < 4 || s.Rift || run.GameTime < s.CooldownUntil {
			return out, "需要元婴、准备冷却结束且尚未发现可用裂缝"
		}
		p := chance(c, s, c.Routes.RiftDiscoveryChance, s.Insight)
		add("探查虚空", "", 30*Day, map[string]int64{"stone": 8}, fmt.Sprintf("发现概率 %d%%；失败进入准备冷却", p), p)
	case "trial.rift_enter":
		if s.Realm < 4 || !s.Rift || run.GameTime < s.CooldownUntil {
			return out, "需要元婴、已发现裂缝且准备冷却结束"
		}
		cost := map[string]int64{"stone": 12}
		bonus := int64(0)
		if s.Stock["void-anchor"] > 0 {
			cost["void-anchor"] = 1
			bonus = 15
		}
		p := chance(c, s, c.Routes.RiftChance+int64(s.Realm-4)*4+bonus, s.Vigor)
		damage := int64(40)
		if bonus > 0 {
			damage -= 15
		}
		add("穿越裂缝", "", Day, cost, fmt.Sprintf("成功率 %d%%；失败损失 %d 生命、增加 20 伤势，裂缝关闭并进入冷却；生命归零将陨落", p, damage), p)
	case "trial.tribulation_begin":
		if s.Realm < 5 || s.Progress < 100 || s.HP < 60 || s.Injury >= 30 || run.GameTime < s.CooldownUntil {
			return out, "需要化神及以上圆满、生命至少 60、伤势低于 30、冷却结束"
		}
		add("引动九波雷劫", "", Day, map[string]int64{"stone": 10, "ward": 1}, "新的雷劫从第一波开始；法宝减伤，每个三波阶段至多消耗一张雷符提供防护", 100)
	case "trial.tribulation_wave":
		if !s.Tribulation.Active {
			return out, "当前没有进行中的雷劫"
		}
		cost := map[string]int64{"stone": c.Routes.WaveCost + s.Tribulation.Failures}
		if (s.Tribulation.Wave-1)%3 == 0 && s.Tribulation.ProtectedPhase != phase(s.Tribulation.Wave) && s.Stock["thunder-talisman"] > 0 {
			cost["thunder-talisman"] = 1
		}
		p := waveChance(c, s)
		damage := waveDamage(c, s, cost)
		add(fmt.Sprintf("第 %d 次天劫 · 第 %d 波", s.Tribulation.Attempt, s.Tribulation.Wave), "", 60, cost, fmt.Sprintf("成功率 %d%%；失败伤害 %d，大失败额外 15；增加 %d 伤势，本波不前进；重试多消耗灵石，生命归零陨落", p, damage, waveInjury(s, cost)), p)
	case "trial.tribulation_retreat":
		if !s.Tribulation.Active {
			return out, "当前没有进行中的雷劫"
		}
		add("放弃本次天劫", "", Day, nil, "增加 15 伤势，失去全部波次进度并进入准备冷却", 100)
	case "trial.abandon":
		add("提前终止本世", "", 0, nil, "不获得通关积分", 100)
	case "trial.pass_away":
		if lifeRemaining(c, s, run) > 5*Year {
			return out, "剩余寿元不超过五年才可坐化"
		}
		add("安排后事并坐化", "", 0, nil, "结束本世，无积分奖励", 100)
	}
	return out, "当前前置条件未满足、资源不足或没有可用方案"
}

func waveDamage(c Config, s State, cost map[string]int64) int64 {
	d := c.Routes.WaveDamage + (s.Tribulation.Wave-1)/3*5 + s.Tribulation.Failures*3 - method(c, s).GuardBonus - int64(s.Realm-5)*2
	if s.Tribulation.Ward {
		d -= 8
	}
	if s.Tribulation.ProtectedPhase == phase(s.Tribulation.Wave) || cost["thunder-talisman"] > 0 {
		d -= 10
	}
	return max(5, d)
}

func waveInjury(s State, cost map[string]int64) int64 {
	if s.Tribulation.ProtectedPhase == phase(s.Tribulation.Wave) || cost["thunder-talisman"] > 0 {
		return 4
	}
	return 10
}

func (m Module) Preview(content contract.Content, run contract.RunState, actor string) ([]contract.ActionPreview, error) {
	c, s, err := read(content, run)
	if err != nil {
		return nil, err
	}
	out := []contract.ActionPreview{}
	if run.Status != contract.RunActive {
		return out, nil
	}
	labels := map[string]string{}
	for _, i := range c.Items {
		labels[i.Key] = i.Name
	}
	for _, action := range m.Actions() {
		if !slices.Contains(action.Actors, actor) {
			continue
		}
		choices, reason := options(c, s, run, action.Key)
		p := contract.ActionPreview{Action: action, Available: len(choices) > 0, CostLabels: labels}
		if len(choices) == 0 {
			p.Reason = reason
		} else {
			p.Minutes = choices[0].plan.Minutes
			p.Costs = choices[0].plan.Costs
			p.Risk = choices[0].plan.Risk
		}
		for _, o := range choices {
			p.Plans = append(p.Plans, o.plan)
		}
		if len(action.Parameters) > 0 && len(choices) > 0 {
			p.Action.Parameters[0].Options = []string{}
			p.Action.Parameters[0].OptionLabels = map[string]string{}
			for _, o := range choices {
				k := o.plan.Arguments["key"].(string)
				p.Action.Parameters[0].Options = append(p.Action.Parameters[0].Options, k)
				p.Action.Parameters[0].OptionLabels[k] = o.plan.Label
			}
		}
		out = append(out, p)
	}
	return out, nil
}
