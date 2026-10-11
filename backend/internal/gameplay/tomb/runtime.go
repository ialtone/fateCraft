package tomb

import (
	"fmt"
	"github.com/ialtone/fatecraft/internal/gameplay/components/expedition"
	"github.com/ialtone/fatecraft/internal/gameplay/components/ruledata"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"slices"
)

type State struct {
	Journey  expedition.State  `json:"journey"`
	Clues    []string          `json:"clues"`
	Resolved map[string]string `json:"resolved"`
	Light    int64             `json:"light"`
	Tools    int64             `json:"tools"`
}
type Module struct{}

func (Module) NarrativeFactPaths() []string {
	return []string{"location", "health", "supplies", "light", "tools", "remaining_minutes", "outcome_key"}
}

func (Module) Actions() []contract.RuntimeAction {
	result := []contract.RuntimeAction{}
	for _, a := range [][3]string{{"move", "前往墓室", "destination"}, {"inspect", "研读线索", ""}, {"solve", "解除机关", "trap"}, {"bypass", "冒险绕过机关", "trap"}, {"take", "收取遗物", "item"}, {"drop", "舍弃携带物", "item"}, {"rest", "安全休整", ""}, {"wait", "等待片刻", ""}, {"extract", "携遗物撤离", ""}, {"retreat", "提前撤回", ""}, {"abandon", "放弃远征", ""}} {
		action := contract.RuntimeAction{Key: "tomb." + a[0], Label: a[1], Description: a[1] + "，照明与期限只随实际行动流逝", Actors: []string{"player", "model"}, Writes: []string{"journey", "clues", "resolved", "light", "tools"}, Events: []string{"tomb.acted", "tomb.trap_resolved", "tomb.ended"}}
		if a[2] != "" {
			action.Parameters = []contract.RuntimeParameter{{Key: a[2], Label: a[1] + "目标", Type: "string", Required: true}}
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
	data, err := ruledata.Object(State{Journey: expedition.Initialize(c.World), Clues: []string{}, Resolved: map[string]string{}, Light: c.Light, Tools: c.Tools})
	return contract.RunState{SchemaVersion: 1, Status: contract.RunActive, Data: data}, err
}
func hasClues(s State, keys []string) bool {
	for _, key := range keys {
		if !slices.Contains(s.Clues, key) {
			return false
		}
	}
	return true
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
	if run.SchemaVersion != 1 || run.GameTime > c.World.Deadline || s.Light != max(0, c.Light-run.GameTime) || s.Tools < 0 || s.Tools > c.Tools {
		return fmt.Errorf("墓室状态头或消耗不一致")
	}
	if err := expedition.ValidateState(c.World, s.Journey); err != nil {
		return err
	}
	clues := map[string]bool{}
	for _, key := range s.Clues {
		found := false
		for _, clue := range c.Clues {
			if key == clue.Key && slices.Contains(s.Journey.Visited, clue.Node) && hasClues(s, clue.Requires) {
				found = true
			}
		}
		if !found || clues[key] {
			return fmt.Errorf("线索缺少探索来源")
		}
		clues[key] = true
	}
	tools, count := c.Tools, 0
	for _, p := range c.Traps {
		if how := s.Resolved[p.Key]; how != "" {
			count++
			if how == "solved" {
				if !hasClues(s, p.Clues) {
					return fmt.Errorf("机关缺少线索证据")
				}
			} else if how == "bypassed" {
				tools -= p.Tools
			} else {
				return fmt.Errorf("机关处理方式无效")
			}
		}
		if slices.Contains(s.Journey.Visited, p.Node) && s.Resolved[p.Key] == "" {
			return fmt.Errorf("越过了未解除的机关")
		}
	}
	if count != len(s.Resolved) || tools != s.Tools {
		return fmt.Errorf("机关或工具消耗无效")
	}
	if run.Status == contract.RunActive && (s.Journey.Health == 0 || s.Light == 0 || run.GameTime >= c.World.Deadline || len(s.Journey.Secured) > 0) {
		return fmt.Errorf("墓室终止条件未处理")
	}
	if run.Status == contract.RunCompleted && (run.OutcomeKey != "recovered" || s.Journey.Position != c.World.Start || s.Journey.Secured[c.World.Goal] != expedition.FindLoot(c.World, c.World.Goal).Quantity) {
		return fmt.Errorf("遗物尚未安全带出")
	}
	if run.Status != contract.RunActive && len(s.Journey.Carried) > 0 {
		return fmt.Errorf("结束后仍有未处理遗物")
	}
	if run.Status == contract.RunFailed {
		switch run.OutcomeKey {
		case "retreated":
			if s.Journey.Position != c.World.Start {
				return fmt.Errorf("未返回入口")
			}
		case "perished":
			if s.Journey.Health != 0 {
				return fmt.Errorf("缺少死亡证据")
			}
		case "darkness":
			if s.Light != 0 {
				return fmt.Errorf("照明尚未耗尽")
			}
		case "timeout":
			if run.GameTime != c.World.Deadline {
				return fmt.Errorf("尚未到期限")
			}
		case "abandoned":
		default:
			return fmt.Errorf("墓室失败结局无效")
		}
	}
	return nil
}
func nearby(c Config, s State, p Trap) bool {
	return p.Node == s.Journey.Position || slices.Contains(expedition.Find(c.World, s.Journey.Position).Links, p.Node)
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
	result := []contract.ActionPreview{}
	for _, a := range m.Actions() {
		if !slices.Contains(a.Actors, actor) {
			continue
		}
		p := contract.ActionPreview{Action: a, Available: true, CostLabels: map[string]string{"supplies": "补给", "tools": "机关工具", "light": "照明分钟"}, Risk: "照明耗尽或超过期限将失去未带出的遗物"}
		add := func(key, label string, minutes int64, cost map[string]int64, risk string) {
			p.Plans = append(p.Plans, contract.ActionPlan{Label: label, Arguments: map[string]any{a.Parameters[0].Key: key}, Minutes: minutes, Costs: cost, Risk: risk})
		}
		switch a.Key {
		case "tomb.move":
			for _, key := range expedition.Find(c.World, s.Journey.Position).Links {
				n := expedition.Find(c.World, key)
				trap := trapAt(c, key)
				if expedition.CanMove(c.World, s.Journey, key) && (trap.Key == "" || s.Resolved[trap.Key] != "") {
					add(key, n.Name, n.Minutes, map[string]int64{"supplies": 1, "light": n.Minutes}, "深入后仍需留足返程补给与照明")
				}
			}
			p.Reason = "通路被机关阻挡或补给不足"
		case "tomb.inspect":
			p.Minutes = 5
			p.Available = false
			for _, clue := range c.Clues {
				p.Available = p.Available || clue.Node == s.Journey.Position && !slices.Contains(s.Clues, clue.Key) && hasClues(s, clue.Requires)
			}
			p.Reason = "此处没有可继续研读的线索，可能尚缺前置发现"
		case "tomb.solve", "tomb.bypass":
			for _, trap := range c.Traps {
				if !nearby(c, s, trap) || s.Resolved[trap.Key] != "" {
					continue
				}
				if a.Key == "tomb.solve" && hasClues(s, trap.Clues) {
					add(trap.Key, trap.Name, 8, map[string]int64{"light": 8}, "依照已知线索解除机关，不额外判定答案")
				}
				if a.Key == "tomb.bypass" && s.Tools >= trap.Tools {
					add(trap.Key, trap.Name, 5, map[string]int64{"tools": trap.Tools, "light": 5}, fmt.Sprintf("消耗工具绕过；%d%% 概率损失 %d 体力", trap.Risk, trap.Damage))
				}
			}
			p.Reason = "需要相邻且未解除的机关，以及足够线索或工具"
		case "tomb.take":
			for _, l := range c.World.Loot {
				if expedition.CanTake(c.World, s.Journey, l.Key) {
					add(l.Key, l.Name, 2, map[string]int64{"light": 2}, "此刻仅为携带，返回入口撤离才确认带出")
				}
			}
			p.Reason = "此处没有未取遗物，或剩余负重不足"
		case "tomb.drop":
			for _, l := range c.World.Loot {
				if s.Journey.Carried[l.Key] > 0 {
					add(l.Key, l.Name, 0, nil, "舍弃后不可重复拾取")
				}
			}
			p.Reason = "当前未携带遗物"
		case "tomb.rest":
			p.Minutes = 15
			p.Costs = map[string]int64{"supplies": 1, "light": 15}
			p.Available = expedition.CanRest(c.World, s.Journey)
			p.Reason = "需在可休整墓室，且有补给、受伤及剩余次数"
		case "tomb.wait":
			p.Minutes = 5
		case "tomb.extract":
			p.Available = s.Journey.Position == c.World.Start && s.Journey.Carried[c.World.Goal] > 0
			p.Reason = "需携带目标遗物返回入口"
			p.Risk = "确认带出所有携带遗物并结束远征"
		case "tomb.retreat":
			p.Available = s.Journey.Position == c.World.Start
			p.Reason = "需先返回入口"
			p.Risk = "提前结束，保留已携带遗物，记为提前撤回"
		case "tomb.abandon":
			p.Risk = "所有携带遗物遗失，结束本次远征"
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
		return contract.Transition{}, contract.Reject("tomb.unavailable", "当前无法执行该行动")
	}
	started := run.GameTime
	run.GameTime = min(run.GameTime+minutes, c.World.Deadline, c.Light)
	s.Light = c.Light - run.GameTime
	events := []contract.RuntimeEvent{}
	arg := func(key string) string { value, _ := request.Arguments[key].(string); return value }
	if s.Light == 0 {
		run.Status = contract.RunFailed
		run.OutcomeKey = "darkness"
	} else if run.GameTime == c.World.Deadline {
		run.Status = contract.RunFailed
		run.OutcomeKey = "timeout"
	} else {
		switch request.ActionKey {
		case "tomb.move":
			err = expedition.Move(c.World, &s.Journey, arg("destination"))
		case "tomb.inspect":
			for _, clue := range c.Clues {
				if clue.Node == s.Journey.Position && !slices.Contains(s.Clues, clue.Key) && hasClues(s, clue.Requires) {
					s.Clues = append(s.Clues, clue.Key)
				}
			}
		case "tomb.solve", "tomb.bypass":
			for _, p := range c.Traps {
				if p.Key == arg("trap") {
					how := "solved"
					if request.ActionKey == "tomb.bypass" {
						how = "bypassed"
						s.Tools -= p.Tools
						draw, e := request.Random.Intn("tomb.bypass", 100)
						if e != nil {
							return contract.Transition{}, e
						}
						if draw < p.Risk {
							expedition.Damage(&s.Journey, p.Damage)
						}
					}
					s.Resolved[p.Key] = how
					events = append(events, contract.RuntimeEvent{Kind: "tomb.trap_resolved", Visibility: contract.VisibilityPlayer, Data: map[string]any{"name": p.Name, "method": how}})
				}
			}
		case "tomb.take":
			err = expedition.Take(c.World, &s.Journey, arg("item"))
		case "tomb.drop":
			err = expedition.Drop(&s.Journey, arg("item"))
		case "tomb.rest":
			err = expedition.Rest(c.World, &s.Journey)
		case "tomb.wait":
		case "tomb.extract":
			run.Status = contract.RunCompleted
			run.OutcomeKey = "recovered"
			expedition.Extract(&s.Journey)
		case "tomb.retreat":
			run.Status = contract.RunFailed
			run.OutcomeKey = "retreated"
			expedition.Extract(&s.Journey)
		case "tomb.abandon":
			run.Status = contract.RunFailed
			run.OutcomeKey = "abandoned"
		default:
			return contract.Transition{}, contract.Reject("action.unknown", "未知墓室动作")
		}
	}
	if err != nil {
		return contract.Transition{}, err
	}
	if s.Journey.Health == 0 {
		run.Status = contract.RunFailed
		run.OutcomeKey = "perished"
	}
	if run.Status != contract.RunActive {
		expedition.Lose(&s.Journey, true)
		events = append(events, contract.RuntimeEvent{Kind: "tomb.ended", Visibility: contract.VisibilityPlayer, Data: map[string]any{"outcome_key": run.OutcomeKey}})
	}
	run.Data, err = ruledata.Object(s)
	events = append(events, contract.RuntimeEvent{Kind: "tomb.acted", Visibility: contract.VisibilityPlayer, Data: map[string]any{"action": request.ActionKey, "minutes": run.GameTime - started}})
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
	clues := []string{}
	for _, clue := range c.Clues {
		if slices.Contains(s.Clues, clue.Key) {
			clues = append(clues, clue.Name)
		}
	}
	traps := []any{}
	for _, p := range c.Traps {
		if slices.Contains(s.Journey.Known, p.Node) {
			status := "尚未解除"
			if s.Resolved[p.Key] == "solved" {
				status = "依线索解除"
			}
			if s.Resolved[p.Key] == "bypassed" {
				status = "已绕过"
			}
			traps = append(traps, map[string]any{"name": p.Name, "status": status, "description": status})
		}
	}
	view["clues"] = clues
	view["traps"] = traps
	view["light"] = s.Light
	view["tools"] = s.Tools
	view["remaining_minutes"] = c.World.Deadline - run.GameTime
	view["outcome_key"] = run.OutcomeKey
	return view, nil
}
