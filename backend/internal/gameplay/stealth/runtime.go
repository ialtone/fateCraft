package stealth

import (
	"fmt"
	"github.com/ialtone/fatecraft/internal/gameplay/components/ruledata"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"slices"
)

type State struct {
	Location    string   `json:"location"`
	Alert       int64    `json:"alert"`
	Phase       int64    `json:"phase"`
	Lockpicks   int64    `json:"lockpicks"`
	Decoys      int64    `json:"decoys"`
	Distracted  bool     `json:"distracted"`
	Intel       bool     `json:"intel"`
	Target      bool     `json:"target"`
	Discovered  []string `json:"discovered"`
	Unlocked    []string `json:"unlocked"`
	Observation string   `json:"observation"`
	ObservedAt  int64    `json:"observed_at"`
}
type Module struct{}

func (Module) NarrativeFactPaths() []string {
	return []string{"location", "alert", "pursuit", "has_intel", "has_target", "remaining_minutes", "outcome_key"}
}

func (Module) Actions() []contract.RuntimeAction {
	result := []contract.RuntimeAction{}
	for _, a := range [][4]string{{"observe", "观察动静", "观察当前地点、相邻路线和门禁情报", ""}, {"sneak", "谨慎潜行", "沿已发现路线缓慢移动", "destination"}, {"rush", "快速穿行", "缩短路程耗时，但提高暴露风险", "destination"}, {"unlock", "解除门禁", "消耗开锁器或使用已掌握情报解锁相邻地点", "destination"}, {"distract", "制造干扰", "消耗一个干扰器降低警戒并掩护下一次移动", ""}, {"hide", "暂时藏身", "在有掩护的地点花费时间摆脱追捕", ""}, {"take", "取得任务目标", "在目标地点获取文件，同时提高警戒", ""}, {"escape", "撤离封锁区", "在出口携带目标且摆脱追捕后完成行动", ""}, {"withdraw", "空手撤离", "在出口摆脱追捕后放弃本次目标", ""}, {"abandon", "放弃行动", "结束本次潜入行动", ""}} {
		action := contract.RuntimeAction{Key: "stealth." + a[0], Label: a[1], Description: a[2], Actors: []string{"player", "model"}, Writes: []string{"location", "alert", "phase", "lockpicks", "decoys", "distracted", "intel", "target", "discovered", "unlocked", "observation", "observed_at"}, Events: []string{"stealth.acted", "stealth.detected", "stealth.ended"}}
		if a[3] != "" {
			action.Parameters = []contract.RuntimeParameter{{Key: a[3], Label: "目的地", Type: "string", Required: true}}
		}
		if slices.Contains([]string{"escape", "withdraw", "abandon"}, a[0]) {
			action.Actors = []string{"player"}
			action.Ending = true
		}
		result = append(result, action)
	}
	return result
}
func (Module) Initialize(content contract.Content, input contract.InitialInput) (contract.RunState, error) {
	c, err := ruledata.Singleton[Config](content, DefinitionKind)
	if err != nil {
		return contract.RunState{}, err
	}
	if err := validate(c); err != nil {
		return contract.RunState{}, err
	}
	if input.Random == nil {
		return contract.RunState{}, fmt.Errorf("潜入开局缺少随机源")
	}
	phase, err := input.Random.Intn("stealth.initial_patrol", int64(len(c.Patrol)))
	if err != nil {
		return contract.RunState{}, err
	}
	data, err := ruledata.Object(State{Location: c.Start, Phase: phase, Lockpicks: c.Lockpicks, Decoys: c.Decoys, Discovered: []string{c.Start}, Unlocked: []string{}, ObservedAt: -1})
	return contract.RunState{SchemaVersion: 1, Status: contract.RunActive, Data: data}, err
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
	if run.SchemaVersion != 1 || find(c, s.Location).Key == "" || s.Alert < 0 || s.Alert > 100 || s.Phase < 0 || s.Phase >= int64(len(c.Patrol)) || s.Lockpicks < 0 || s.Lockpicks > c.Lockpicks || s.Decoys < 0 || s.Decoys > c.Decoys || s.ObservedAt < -1 || s.ObservedAt > run.GameTime {
		return fmt.Errorf("潜入状态数值无效")
	}
	for _, list := range [][]string{s.Discovered, s.Unlocked} {
		if len(list) > len(c.Locations) {
			return fmt.Errorf("地点记录超过上限")
		}
		seen := map[string]bool{}
		for _, key := range list {
			if find(c, key).Key == "" || seen[key] {
				return fmt.Errorf("地点记录无效")
			}
			seen[key] = true
		}
	}
	if !slices.Contains(s.Discovered, s.Location) {
		return fmt.Errorf("当前地点未被发现")
	}
	if run.Status == contract.RunActive && (s.Alert >= 100 || run.GameTime >= c.Deadline) {
		return fmt.Errorf("潜入终止条件未处理")
	}
	if run.OutcomeKey == "escaped" && (!s.Target || !slices.Contains(c.Exits, s.Location) || s.Alert >= c.PursuitThreshold) {
		return fmt.Errorf("未携带目标安全撤离")
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
	location := find(c, s.Location)
	result := []contract.ActionPreview{}
	for _, a := range m.Actions() {
		if !slices.Contains(a.Actors, actor) {
			continue
		}
		p := contract.ActionPreview{Action: a, Available: true, CostLabels: map[string]string{"lockpick": "开锁器", "decoy": "干扰器"}, Risk: "行动会推进巡逻，警戒达到 100 时被捕"}
		switch a.Key {
		case "stealth.observe":
			p.Minutes = 5
		case "stealth.sneak", "stealth.rush", "stealth.unlock":
			p.Action.Parameters[0].Options = []string{}
			p.Action.Parameters[0].OptionLabels = map[string]string{}
			for _, key := range location.Links {
				l := find(c, key)
				if !slices.Contains(s.Discovered, key) {
					continue
				}
				locked := l.Locked && !slices.Contains(s.Unlocked, key)
				minutes := l.Minutes
				cost := map[string]int64{}
				risk := fmt.Sprintf("地点基础发现概率 %d%%，巡逻与当前警戒可能增加风险", l.Risk)
				if a.Key == "stealth.unlock" {
					if !locked || (!s.Intel && s.Lockpicks == 0) {
						continue
					}
					minutes = 5
					if !s.Intel {
						cost["lockpick"] = 1
					}
					risk = "解锁会增加 10 点警戒"
				} else {
					if locked {
						continue
					}
					if a.Key == "stealth.rush" {
						minutes = max(1, minutes/2)
						risk += "；快速穿行额外增加 20% 发现概率"
					}
				}
				p.Action.Parameters[0].Options = append(p.Action.Parameters[0].Options, key)
				p.Action.Parameters[0].OptionLabels[key] = l.Name
				p.Plans = append(p.Plans, contract.ActionPlan{Label: l.Name, Arguments: map[string]any{"destination": key}, Minutes: minutes, Costs: cost, Risk: risk})
			}
			p.Available = len(p.Plans) > 0
			p.Reason = "需先观察路线；门禁需情报或开锁器"
		case "stealth.distract":
			p.Available = s.Decoys > 0 && !s.Distracted
			p.Reason = "没有干扰器，或已有干扰生效"
			p.Minutes = 5
			p.Costs = map[string]int64{"decoy": 1}
		case "stealth.hide":
			p.Available = location.Hiding && s.Alert > 0
			p.Reason = "当前位置没有掩护，或当前无需脱身"
			p.Minutes = 10
			p.Risk = "降低 30 点警戒，但仍消耗行动时间"
		case "stealth.take":
			p.Available = s.Location == c.Target && !s.Target
			p.Reason = "需抵达目标地点，且目标尚未取得"
			p.Minutes = 5
			p.Risk = "取得目标会增加 15 点警戒，随后还需撤离"
		case "stealth.escape":
			p.Available = s.Target && slices.Contains(c.Exits, s.Location) && s.Alert < c.PursuitThreshold
			p.Reason = "需要携带目标到达出口并摆脱追捕"
			p.Risk = "撤离后结束本次行动"
		case "stealth.withdraw":
			p.Available = !s.Target && slices.Contains(c.Exits, s.Location) && s.Alert < c.PursuitThreshold
			p.Reason = "需要在出口摆脱追捕"
			p.Risk = "空手撤离，本次目标未达成"
		case "stealth.abandon":
			p.Risk = "本次行动结束"
		}
		if p.Available {
			p.Reason = ""
		}
		result = append(result, p)
	}
	return result, nil
}
func (Module) Execute(content contract.Content, run contract.RunState, request contract.RuntimeRequest) (contract.Transition, error) {
	c, err := ruledata.Singleton[Config](content, DefinitionKind)
	if err != nil {
		return contract.Transition{}, err
	}
	s, err := ruledata.Decode[State](run.Data)
	if err != nil {
		return contract.Transition{}, err
	}
	minutes := int64(5)
	detected := false
	destination, _ := request.Arguments["destination"].(string)
	switch request.ActionKey {
	case "stealth.sneak":
		minutes = find(c, destination).Minutes
	case "stealth.rush":
		minutes = max(1, find(c, destination).Minutes/2)
	case "stealth.hide":
		minutes = 10
	case "stealth.escape", "stealth.withdraw", "stealth.abandon":
		minutes = 0
	}
	if run.GameTime+minutes >= c.Deadline {
		minutes = c.Deadline - run.GameTime
		s.Phase = (s.Phase + c.Deadline/c.PatrolMinutes - run.GameTime/c.PatrolMinutes) % int64(len(c.Patrol))
		run.GameTime = c.Deadline
		run.Status = contract.RunFailed
		run.OutcomeKey = "timeout"
		run.Data, err = ruledata.Object(s)
		return contract.Transition{State: run, Events: []contract.RuntimeEvent{{Kind: "stealth.acted", Visibility: contract.VisibilityPlayer, Data: map[string]any{"action": request.ActionKey, "minutes": minutes}}, {Kind: "stealth.ended", Visibility: contract.VisibilityPlayer, Data: map[string]any{"outcome_key": "timeout"}}}, Result: map[string]any{"minutes": minutes, "outcome_key": "timeout"}}, err
	}
	switch request.ActionKey {
	case "stealth.observe":
		l := find(c, s.Location)
		for _, key := range l.Links {
			if !slices.Contains(s.Discovered, key) {
				s.Discovered = append(s.Discovered, key)
			}
		}
		s.Intel = s.Intel || l.Intel
	case "stealth.sneak", "stealth.rush":
		l := find(c, destination)
		minutes = l.Minutes
		risk := l.Risk + s.Alert/4
		if request.ActionKey == "stealth.rush" {
			minutes = max(1, minutes/2)
			risk += 20
		}
		arrival := (s.Phase + (run.GameTime+minutes)/c.PatrolMinutes - run.GameTime/c.PatrolMinutes) % int64(len(c.Patrol))
		if c.Patrol[arrival] == destination {
			risk += c.PatrolRisk
		}
		if s.Distracted {
			risk = max(0, risk-40)
			s.Distracted = false
		}
		draw, err := request.Random.Intn("stealth.detection", 100)
		if err != nil {
			return contract.Transition{}, err
		}
		detected = draw < min(95, risk)
		if detected {
			s.Alert = min(100, s.Alert+35)
		}
		s.Location = destination
	case "stealth.unlock":
		if !s.Intel {
			s.Lockpicks--
		}
		s.Unlocked = append(s.Unlocked, destination)
		s.Alert = min(100, s.Alert+10)
	case "stealth.distract":
		s.Decoys--
		s.Distracted = true
		s.Alert = max(0, s.Alert-20)
	case "stealth.hide":
		minutes = 10
		s.Alert = max(0, s.Alert-30)
	case "stealth.take":
		s.Target = true
		s.Alert = min(100, s.Alert+15)
	case "stealth.escape":
		run.Status = contract.RunCompleted
		run.OutcomeKey = "escaped"
		minutes = 0
	case "stealth.withdraw":
		run.Status = contract.RunFailed
		run.OutcomeKey = "withdrawn"
		minutes = 0
	case "stealth.abandon":
		run.Status = contract.RunFailed
		run.OutcomeKey = "abandoned"
		minutes = 0
	default:
		return contract.Transition{}, contract.Reject("action.unknown", "未知潜入动作")
	}
	s.Phase = (s.Phase + (run.GameTime+minutes)/c.PatrolMinutes - run.GameTime/c.PatrolMinutes) % int64(len(c.Patrol))
	run.GameTime += minutes
	if request.ActionKey == "stealth.observe" {
		s.ObservedAt = run.GameTime
		s.Observation = "暂未听到附近巡逻动静"
		if c.Patrol[s.Phase] == s.Location || slices.Contains(find(c, s.Location).Links, c.Patrol[s.Phase]) {
			s.Observation = "附近传来巡逻脚步，行动应谨慎"
		}
	}
	if s.Alert >= 100 {
		run.Status = contract.RunFailed
		run.OutcomeKey = "captured"
	} else if run.GameTime >= c.Deadline {
		run.Status = contract.RunFailed
		run.OutcomeKey = "timeout"
	}
	run.Data, err = ruledata.Object(s)
	if err != nil {
		return contract.Transition{}, err
	}
	events := []contract.RuntimeEvent{{Kind: "stealth.acted", Visibility: contract.VisibilityPlayer, Data: map[string]any{"action": request.ActionKey, "minutes": minutes, "alert": s.Alert}}}
	if detected {
		events = append(events, contract.RuntimeEvent{Kind: "stealth.detected", Visibility: contract.VisibilityPlayer, Data: map[string]any{"pursued": s.Alert >= c.PursuitThreshold}})
	}
	if run.Status != contract.RunActive {
		events = append(events, contract.RuntimeEvent{Kind: "stealth.ended", Visibility: contract.VisibilityPlayer, Data: map[string]any{"outcome_key": run.OutcomeKey}})
	}
	return contract.Transition{State: run, Events: events, Result: map[string]any{"minutes": minutes, "outcome_key": run.OutcomeKey}}, nil
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
	locations := []any{}
	for _, l := range c.Locations {
		if slices.Contains(s.Discovered, l.Key) {
			locations = append(locations, map[string]any{"key": l.Key, "name": l.Name, "locked": l.Locked && !slices.Contains(s.Unlocked, l.Key)})
		}
	}
	observation := "巡逻情报已过时，可以重新观察"
	if s.ObservedAt == run.GameTime {
		observation = s.Observation
	}
	status := "尚未进入追捕"
	if s.Alert >= c.PursuitThreshold {
		status = "正在被追捕，寻找掩护降低警戒"
	}
	return map[string]any{"location": find(c, s.Location).Name, "alert": s.Alert, "pursuit": status, "lockpicks": s.Lockpicks, "decoys": s.Decoys, "has_intel": s.Intel, "has_target": s.Target, "locations": locations, "observation": observation, "game_time": run.GameTime, "remaining_minutes": max(0, c.Deadline-run.GameTime), "goal": "取得目标文件，摆脱追捕并回到撤离出口", "outcome_key": run.OutcomeKey, "discovered_count": int64(len(s.Discovered))}, nil
}
