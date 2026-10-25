package dungeon

import (
	"fmt"
	"github.com/ialtone/fatecraft/internal/gameplay/components/expedition"
	"github.com/ialtone/fatecraft/internal/gameplay/components/ruledata"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"slices"
)

type State struct {
	Journey  expedition.State `json:"journey"`
	Enemies  map[string]int64 `json:"enemies"`
	Medicine int64            `json:"medicine"`
	Healed   int64            `json:"healed"`
}
type Module struct{}

func (Module) NarrativeFactPaths() []string {
	return []string{"location", "health", "supplies", "medicine", "remaining_minutes", "outcome_key"}
}

func (Module) Actions() []contract.RuntimeAction {
	result := []contract.RuntimeAction{}
	for _, a := range [][3]string{{"move", "前往区域", "destination"}, {"fight", "迎战敌人", "style"}, {"flee", "退回上一区域", ""}, {"take", "收取战利品", "item"}, {"drop", "舍弃携带物", "item"}, {"heal", "使用药物", ""}, {"rest", "营地休整", ""}, {"wait", "等待片刻", ""}, {"extract", "完成远征", ""}, {"retreat", "提前撤离", ""}, {"abandon", "放弃远征", ""}} {
		action := contract.RuntimeAction{Key: "dungeon." + a[0], Label: a[1], Description: a[1] + "，消耗由当前路线和遭遇决定", Actors: []string{"player", "model"}, Writes: []string{"journey", "enemies", "medicine", "healed"}, Events: []string{"dungeon.acted", "dungeon.combat", "dungeon.ended"}}
		if a[2] != "" {
			action.Parameters = []contract.RuntimeParameter{{Key: a[2], Label: a[1] + "方式/目标", Type: "string", Required: true}}
		}
		if slices.Contains([]string{"extract", "retreat", "abandon"}, a[0]) {
			action.Actors = []string{"player"}
			action.Ending = true
		}
		result = append(result, action)
	}
	return result
}
func (Module) Initialize(content contract.Content, _ contract.InitialInput) (contract.RunState, error) {
	c, err := ruledata.Singleton[Config](content, DefinitionKind)
	if err != nil {
		return contract.RunState{}, err
	}
	if err := validate(c); err != nil {
		return contract.RunState{}, err
	}
	s := State{Journey: expedition.Initialize(c.World), Enemies: map[string]int64{}, Medicine: c.Medicine}
	for _, e := range c.Enemies {
		s.Enemies[e.Key] = e.Health
	}
	data, err := ruledata.Object(s)
	return contract.RunState{SchemaVersion: 1, Status: contract.RunActive, Data: data}, err
}
func engaged(c Config, s State) bool {
	enemy := enemyAt(c, s.Journey.Position)
	return enemy.Key != "" && s.Enemies[enemy.Key] > 0
}
func (Module) ValidateState(content contract.Content, run contract.RunState) error {
	c, err := ruledata.Singleton[Config](content, DefinitionKind)
	if err != nil {
		return err
	}
	s, err := ruledata.Decode[State](run.Data)
	if err != nil {
		return err
	}
	if run.SchemaVersion != 1 || run.GameTime > c.World.Deadline || len(s.Enemies) != len(c.Enemies) || s.Medicine < 0 || s.Healed < 0 || s.Medicine+s.Healed != c.Medicine {
		return fmt.Errorf("地牢状态格式或药物消耗无效")
	}
	if err := expedition.ValidateState(c.World, s.Journey); err != nil {
		return err
	}
	for _, e := range c.Enemies {
		hp, ok := s.Enemies[e.Key]
		if !ok || hp < 0 || hp > e.Health || hp < e.Health && !slices.Contains(s.Journey.Visited, e.Node) {
			return fmt.Errorf("遭遇生命或来源无效")
		}
	}
	for _, key := range s.Journey.Taken {
		enemy := enemyAt(c, expedition.FindLoot(c.World, key).Node)
		if enemy.Key != "" && s.Enemies[enemy.Key] != 0 {
			return fmt.Errorf("未击败守卫就领取战利品")
		}
	}
	if run.Status == contract.RunActive && (s.Journey.Health == 0 || run.GameTime == c.World.Deadline || len(s.Journey.Secured) > 0) {
		return fmt.Errorf("地牢终止条件未处理")
	}
	if run.Status != contract.RunActive && len(s.Journey.Carried) > 0 {
		return fmt.Errorf("结束后仍有未处理战利品")
	}
	if run.Status == contract.RunCompleted && (run.OutcomeKey != "cleared" || s.Enemies[c.Boss] != 0 || s.Journey.Position != c.World.Start || s.Journey.Secured[c.World.Goal] != expedition.FindLoot(c.World, c.World.Goal).Quantity) {
		return fmt.Errorf("缺少终点胜利及安全带出目标的证据")
	}
	if run.Status == contract.RunFailed {
		switch run.OutcomeKey {
		case "retreated":
			if s.Journey.Position != c.World.Start {
				return fmt.Errorf("未返回入口")
			}
		case "defeated":
			if s.Journey.Health != 0 {
				return fmt.Errorf("缺少战败证据")
			}
		case "timeout":
			if run.GameTime != c.World.Deadline {
				return fmt.Errorf("期限尚未到达")
			}
		case "abandoned":
		default:
			return fmt.Errorf("地牢失败结局无效")
		}
	}
	return nil
}
func (m Module) Preview(content contract.Content, run contract.RunState, actor string) ([]contract.ActionPreview, error) {
	c, err := ruledata.Singleton[Config](content, DefinitionKind)
	if err != nil {
		return nil, err
	}
	s, err := ruledata.Decode[State](run.Data)
	if err != nil {
		return nil, err
	}
	combat := engaged(c, s)
	result := []contract.ActionPreview{}
	for _, a := range m.Actions() {
		if !slices.Contains(a.Actors, actor) {
			continue
		}
		p := contract.ActionPreview{Action: a, Available: true, CostLabels: map[string]string{"supplies": "补给", "medicine": "治疗药物"}, Risk: "补给和休整次数有限，请为返程留出余量"}
		add := func(key, label string, minutes int64, cost map[string]int64, risk string) {
			p.Plans = append(p.Plans, contract.ActionPlan{Label: label, Arguments: map[string]any{a.Parameters[0].Key: key}, Minutes: minutes, Costs: cost, Risk: risk})
		}
		switch a.Key {
		case "dungeon.move":
			if !combat {
				for _, key := range expedition.Find(c.World, s.Journey.Position).Links {
					if expedition.CanMove(c.World, s.Journey, key) {
						n := expedition.Find(c.World, key)
						add(key, n.Name, n.Minutes, map[string]int64{"supplies": 1}, "未探索区域的敌人与战利品尚不确定")
					}
				}
			}
			p.Reason = "当前遭遇尚未解除，或补给不足"
		case "dungeon.fight":
			if combat && s.Journey.Supplies > 0 {
				enemy := enemyAt(c, s.Journey.Position)
				for _, style := range []string{"guarded", "assault"} {
					label := "稳健进攻"
					bonus, guard := int64(0), int64(3)
					if style == "assault" {
						label = "强攻"
						bonus = 4
						guard = 0
					}
					risk := fmt.Sprintf("造成 %d–%d 伤害；敌人存活时反击 %d–%d 伤害", c.Power+bonus, c.Power+bonus+5, max(1, enemy.Attack-c.Defense-guard), max(1, enemy.Attack+3-c.Defense-guard))
					add(style, label, 5, map[string]int64{"supplies": 1}, risk)
				}
			}
			p.Reason = "需有当前存活敌人和至少一份补给"
		case "dungeon.flee":
			p.Minutes = 5
			p.Available = combat && expedition.CanMove(c.World, s.Journey, s.Journey.Previous)
			p.Costs = map[string]int64{"supplies": 1}
			p.Reason = "需处于遭遇中且有退路与补给"
			p.Risk = "损失 5 生命，并丢失每种携带物的一半（向上取整）；敌人伤势保留"
		case "dungeon.take":
			if !combat {
				for _, l := range c.World.Loot {
					if expedition.CanTake(c.World, s.Journey, l.Key) {
						add(l.Key, l.Name, 2, nil, "只领取一次；安全返回入口后才能带出")
					}
				}
			}
			p.Reason = "需先击败守卫，并有未取战利品和足够负重"
		case "dungeon.drop":
			for _, l := range c.World.Loot {
				if s.Journey.Carried[l.Key] > 0 {
					add(l.Key, l.Name, 0, nil, "舍弃的物品不能再次拾取")
				}
			}
			p.Reason = "当前没有携带物"
		case "dungeon.heal":
			p.Minutes = 5
			p.Costs = map[string]int64{"medicine": 1}
			p.Available = !combat && s.Medicine > 0 && s.Journey.Health < c.World.Health
			p.Reason = "需脱离战斗、有药物且生命未满"
			p.Risk = "恢复 25 生命，不超过初始上限"
		case "dungeon.rest":
			p.Minutes = 15
			p.Costs = map[string]int64{"supplies": 1}
			p.Available = !combat && expedition.CanRest(c.World, s.Journey)
			p.Reason = "需安全营地、受伤、补给和剩余休整次数"
			p.Risk = "恢复 20 生命；休整次数不会因离开区域刷新"
		case "dungeon.wait":
			p.Minutes = 5
		case "dungeon.extract":
			p.Available = s.Journey.Position == c.World.Start && s.Journey.Carried[c.World.Goal] > 0 && s.Enemies[c.Boss] == 0
			p.Reason = "需击败终点敌人并携主要目标返回入口"
			p.Risk = "带出全部携带物，确认完成远征"
		case "dungeon.retreat":
			p.Available = s.Journey.Position == c.World.Start
			p.Reason = "需先返回入口"
			p.Risk = "提前撤离损失每种携带物的一半（向上取整），剩余带出"
		case "dungeon.abandon":
			p.Risk = "所有未带出的战利品遗失"
		}
		if len(a.Parameters) > 0 {
			p.Available = len(p.Plans) > 0
			p.Action.Parameters[0].OptionLabels = map[string]string{}
			for _, plan := range p.Plans {
				key := plan.Arguments[a.Parameters[0].Key].(string)
				p.Action.Parameters[0].Options = append(p.Action.Parameters[0].Options, key)
				p.Action.Parameters[0].OptionLabels[key] = plan.Label
			}
		}
		if p.Available {
			p.Reason = ""
		}
		result = append(result, p)
	}
	return result, nil
}
func (m Module) Execute(content contract.Content, run contract.RunState, request contract.RuntimeRequest) (contract.Transition, error) {
	c, err := ruledata.Singleton[Config](content, DefinitionKind)
	if err != nil {
		return contract.Transition{}, err
	}
	s, err := ruledata.Decode[State](run.Data)
	if err != nil {
		return contract.Transition{}, err
	}
	previews, err := m.Preview(content, run, request.Actor)
	if err != nil {
		return contract.Transition{}, err
	}
	minutes := int64(-1)
	for _, p := range previews {
		if p.Action.Key == request.ActionKey && p.Available {
			minutes = p.Minutes
			for _, plan := range p.Plans {
				for key, value := range plan.Arguments {
					if request.Arguments[key] == value {
						minutes = plan.Minutes
					}
				}
			}
		}
	}
	if minutes < 0 {
		return contract.Transition{}, contract.Reject("dungeon.unavailable", "当前无法执行该行动")
	}
	started := run.GameTime
	run.GameTime = min(run.GameTime+minutes, c.World.Deadline)
	events := []contract.RuntimeEvent{}
	arg := func(key string) string { value, _ := request.Arguments[key].(string); return value }
	if run.GameTime == c.World.Deadline {
		run.Status = contract.RunFailed
		run.OutcomeKey = "timeout"
	} else {
		switch request.ActionKey {
		case "dungeon.move":
			err = expedition.Move(c.World, &s.Journey, arg("destination"))
		case "dungeon.fight":
			enemy := enemyAt(c, s.Journey.Position)
			s.Journey.Supplies--
			bonus, guard := int64(0), int64(3)
			if arg("style") == "assault" {
				bonus = 4
				guard = 0
			}
			draw, e := request.Random.Intn("dungeon.attack", 6)
			if e != nil {
				return contract.Transition{}, e
			}
			damage := min(s.Enemies[enemy.Key], c.Power+bonus+draw)
			s.Enemies[enemy.Key] -= damage
			counter := int64(0)
			if s.Enemies[enemy.Key] > 0 {
				draw, e = request.Random.Intn("dungeon.counter", 4)
				if e != nil {
					return contract.Transition{}, e
				}
				counter = max(1, enemy.Attack+draw-c.Defense-guard)
				expedition.Damage(&s.Journey, counter)
			}
			events = append(events, contract.RuntimeEvent{Kind: "dungeon.combat", Visibility: contract.VisibilityPlayer, Data: map[string]any{"enemy": enemy.Name, "damage": damage, "counter_damage": counter, "defeated": s.Enemies[enemy.Key] == 0}})
		case "dungeon.flee":
			err = expedition.Move(c.World, &s.Journey, s.Journey.Previous)
			expedition.Damage(&s.Journey, 5)
			expedition.Lose(&s.Journey, false)
		case "dungeon.take":
			err = expedition.Take(c.World, &s.Journey, arg("item"))
		case "dungeon.drop":
			err = expedition.Drop(&s.Journey, arg("item"))
		case "dungeon.heal":
			s.Medicine--
			s.Healed++
			s.Journey.Health = min(c.World.Health, s.Journey.Health+25)
		case "dungeon.rest":
			err = expedition.Rest(c.World, &s.Journey)
		case "dungeon.wait":
		case "dungeon.extract":
			run.Status = contract.RunCompleted
			run.OutcomeKey = "cleared"
			expedition.Extract(&s.Journey)
		case "dungeon.retreat":
			run.Status = contract.RunFailed
			run.OutcomeKey = "retreated"
			expedition.Lose(&s.Journey, false)
			expedition.Extract(&s.Journey)
		case "dungeon.abandon":
			run.Status = contract.RunFailed
			run.OutcomeKey = "abandoned"
		default:
			return contract.Transition{}, contract.Reject("action.unknown", "未知地牢动作")
		}
	}
	if err != nil {
		return contract.Transition{}, err
	}
	if s.Journey.Health == 0 {
		run.Status = contract.RunFailed
		run.OutcomeKey = "defeated"
	}
	if run.Status != contract.RunActive {
		expedition.Lose(&s.Journey, true)
		events = append(events, contract.RuntimeEvent{Kind: "dungeon.ended", Visibility: contract.VisibilityPlayer, Data: map[string]any{"outcome_key": run.OutcomeKey}})
	}
	run.Data, err = ruledata.Object(s)
	events = append(events, contract.RuntimeEvent{Kind: "dungeon.acted", Visibility: contract.VisibilityPlayer, Data: map[string]any{"action": request.ActionKey, "minutes": run.GameTime - started}})
	return contract.Transition{State: run, Events: events, Result: map[string]any{"minutes": run.GameTime - started, "outcome_key": run.OutcomeKey}}, err
}
func (Module) Project(content contract.Content, run contract.RunState, _ string) (map[string]any, error) {
	c, err := ruledata.Singleton[Config](content, DefinitionKind)
	if err != nil {
		return nil, err
	}
	s, err := ruledata.Decode[State](run.Data)
	if err != nil {
		return nil, err
	}
	view := expedition.Project(c.World, s.Journey)
	enemies := []any{}
	for _, e := range c.Enemies {
		if slices.Contains(s.Journey.Visited, e.Node) {
			description := fmt.Sprintf("%s，剩余生命 %d", expedition.Find(c.World, e.Node).Name, s.Enemies[e.Key])
			if s.Enemies[e.Key] == 0 {
				description = "已击败"
			}
			enemies = append(enemies, map[string]any{"name": e.Name, "location": expedition.Find(c.World, e.Node).Name, "health": s.Enemies[e.Key], "defeated": s.Enemies[e.Key] == 0, "description": description})
		}
	}
	view["enemies"] = enemies
	view["medicine"] = s.Medicine
	view["rest_remaining"] = max(0, expedition.Find(c.World, s.Journey.Position).Rest-s.Journey.Rested[s.Journey.Position])
	view["remaining_minutes"] = c.World.Deadline - run.GameTime
	view["outcome_key"] = run.OutcomeKey
	defeated := int64(0)
	for _, e := range c.Enemies {
		if s.Enemies[e.Key] == 0 {
			defeated++
		}
	}
	view["visited_count"] = int64(len(s.Journey.Visited))
	view["defeated_count"] = defeated
	view["boss_defeated"] = c.Boss != "" && s.Enemies[c.Boss] == 0
	return view, nil
}
