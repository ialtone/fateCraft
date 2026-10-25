package relationship

import (
	"fmt"
	"github.com/ialtone/fatecraft/internal/gameplay/components/ruledata"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"slices"
	"sort"
)

type PromiseState struct {
	Status  string `json:"status"`
	Started int64  `json:"started"`
	Due     int64  `json:"due"`
}
type State struct {
	Trust    map[string]int64        `json:"trust"`
	Listened []string                `json:"listened"`
	Facts    []string                `json:"facts"`
	Choices  map[string]string       `json:"choices"`
	Promises map[string]PromiseState `json:"promises"`
	Effort   int64                   `json:"effort"`
}
type Module struct{}

func (Module) NarrativeFactPaths() []string {
	return []string{"effort", "game_time", "remaining_minutes", "outcome_key"}
}

func (Module) Actions() []contract.RuntimeAction {
	result := []contract.RuntimeAction{}
	for _, a := range [][4]string{{"listen", "倾听往事", "认真倾听一位人物，一次性获得其已知事实", "person"}, {"choose", "回应关系事件", "作出当前事件的一次性选择，其他选项随后关闭", "choice"}, {"fulfill", "履行承诺", "投入精力完成已建立的承诺，影响相关人物", "promise"}, {"wait", "等待片刻", "推进半小时，承诺和故事期限仍会流逝", ""}, {"conclude", "确认故事结局", "根据已发生事件和履约事实确认当前关系结局", "ending"}, {"leave", "离开小镇", "结束本次故事，不补全未完成承诺", ""}} {
		action := contract.RuntimeAction{Key: "relationship." + a[0], Label: a[1], Description: a[2], Actors: []string{"player", "model"}, Writes: []string{"trust", "listened", "facts", "choices", "promises", "effort"}, Events: []string{"relationship.interacted", "relationship.promise_expired", "relationship.ended"}}
		if a[3] != "" {
			action.Parameters = []contract.RuntimeParameter{{Key: a[3], Label: a[1] + "对象", Type: "string", Required: true}}
		}
		if a[0] == "conclude" || a[0] == "leave" {
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
	s := State{Trust: map[string]int64{}, Listened: []string{}, Facts: []string{}, Choices: map[string]string{}, Promises: map[string]PromiseState{}, Effort: c.Effort}
	for _, p := range c.People {
		s.Trust[p.Key] = p.Trust
	}
	data, err := ruledata.Object(s)
	return contract.RunState{SchemaVersion: 1, Status: contract.RunActive, Data: data}, err
}
func meets(s State, requires Conditions) bool {
	for _, fact := range requires.Facts {
		if !slices.Contains(s.Facts, fact) {
			return false
		}
	}
	for key, n := range requires.Trust {
		if s.Trust[key] < n {
			return false
		}
	}
	for _, key := range requires.Promises {
		if s.Promises[key].Status != "fulfilled" {
			return false
		}
	}
	return true
}
func changeTrust(s *State, changes map[string]int64) {
	for key, n := range changes {
		s.Trust[key] = min(100, max(-100, s.Trust[key]+n))
	}
}

func commitmentsResolved(s State) bool {
	for _, p := range s.Promises {
		if p.Status == "pending" {
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
	if run.SchemaVersion != 1 || run.GameTime > c.Deadline || len(s.Trust) != len(c.People) || len(s.Listened) > len(c.People) || len(s.Choices) > len(c.Events) || len(s.Promises) > len(c.Promises) || s.Effort < 0 || s.Effort > c.Effort {
		return fmt.Errorf("关系状态格式无效")
	}
	people := map[string]Person{}
	expected := map[string]bool{}
	for _, p := range c.People {
		people[p.Key] = p
		if trust, ok := s.Trust[p.Key]; !ok || trust < -100 || trust > 100 {
			return fmt.Errorf("信任状态无效")
		}
	}
	heard := map[string]bool{}
	for _, key := range s.Listened {
		p, ok := people[key]
		if !ok || heard[key] {
			return fmt.Errorf("人物互动记录无效")
		}
		heard[key] = true
		expected[p.Fact] = true
	}
	promiseSources := map[string]bool{}
	validChoices := 0
	for _, e := range c.Events {
		if chosen := s.Choices[e.Key]; chosen != "" {
			found := false
			for _, choice := range e.Choices {
				if choice.Key == chosen {
					found = true
					for fact := range choice.Facts {
						expected[fact] = true
					}
					if choice.Promise != "" {
						promiseSources[choice.Promise] = true
					}
				}
			}
			if !found {
				return fmt.Errorf("事件选择无效")
			}
			validChoices++
		}
	}
	if validChoices != len(s.Choices) || len(expected) != len(s.Facts) {
		return fmt.Errorf("事件或事实来源无效")
	}
	for _, fact := range s.Facts {
		if !expected[fact] {
			return fmt.Errorf("存在无来源关系事实")
		}
		delete(expected, fact)
	}
	effort := c.Effort
	breaches := int64(0)
	count := 0
	for _, p := range c.Promises {
		entry, exists := s.Promises[p.Key]
		if !exists {
			continue
		}
		count++
		if !promiseSources[p.Key] || !slices.Contains([]string{"pending", "fulfilled", "broken"}, entry.Status) || entry.Started < 0 || entry.Started > run.GameTime || entry.Due != entry.Started+p.Duration {
			return fmt.Errorf("承诺记录无效")
		}
		if entry.Status == "fulfilled" {
			effort -= p.Effort
		}
		if entry.Status == "broken" {
			breaches++
		}
		if entry.Status == "pending" && entry.Due < run.GameTime {
			return fmt.Errorf("承诺过期未处理")
		}
	}
	if count != len(s.Promises) || count != len(promiseSources) || effort != s.Effort {
		return fmt.Errorf("承诺来源或精力消耗不一致")
	}
	if run.Status == contract.RunActive && (run.GameTime >= c.Deadline || breaches >= c.MaxBreaches) {
		return fmt.Errorf("关系终止条件未处理")
	}
	if run.Status == contract.RunFailed {
		switch run.OutcomeKey {
		case "broken":
			if breaches < c.MaxBreaches {
				return fmt.Errorf("缺少违约证据")
			}
		case "timeout":
			if run.GameTime != c.Deadline {
				return fmt.Errorf("故事期限尚未到达")
			}
		case "leave":
		default:
			return fmt.Errorf("关系失败结局无效")
		}
	}
	if run.Status == contract.RunCompleted {
		if !commitmentsResolved(s) {
			return fmt.Errorf("仍有尚待处理的承诺")
		}
		for _, e := range c.Endings {
			if e.Key == run.OutcomeKey && meets(s, e.Requires) {
				return nil
			}
		}
		return fmt.Errorf("缺少结局事件与承诺证据")
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
	result := []contract.ActionPreview{}
	for _, a := range m.Actions() {
		if !slices.Contains(a.Actors, actor) {
			continue
		}
		p := contract.ActionPreview{Action: a, Available: true, CostLabels: map[string]string{"effort": "精力"}, Risk: "互动会推进时间；承诺可能过期"}
		add := func(key, label string, minutes int64, cost map[string]int64, risk string) {
			p.Plans = append(p.Plans, contract.ActionPlan{Label: label, Arguments: map[string]any{a.Parameters[0].Key: key}, Minutes: minutes, Costs: cost, Risk: risk})
		}
		switch a.Key {
		case "relationship.listen":
			for _, person := range c.People {
				if !slices.Contains(s.Listened, person.Key) {
					add(person.Key, person.Name, 15, nil, "首次倾听后获得一条事实；重复闲聊不会继续提高信任")
				}
			}
			p.Reason = "已经倾听过所有人物，可在聊天中继续交流"
		case "relationship.choose":
			for _, event := range c.Events {
				if s.Choices[event.Key] != "" || !meets(s, event.Requires) {
					continue
				}
				for _, choice := range event.Choices {
					risk := "本事件其他选择将关闭"
					if choice.Promise != "" {
						for _, promise := range c.Promises {
							if promise.Key == choice.Promise {
								risk += fmt.Sprintf("；承诺需 %d 点精力，须在 %d 分钟内履行", promise.Effort, promise.Duration)
							}
						}
					}
					add(choice.Key, event.Name+"："+choice.Name, 15, nil, risk)
				}
			}
			p.Reason = "当前事件已作答，或人物信任和已知事实尚不足"
		case "relationship.fulfill":
			for _, promise := range c.Promises {
				entry := s.Promises[promise.Key]
				if entry.Status == "pending" && run.GameTime+30 <= entry.Due && s.Effort >= promise.Effort && meets(s, promise.Requires) {
					add(promise.Key, promise.Name, 30, map[string]int64{"effort": promise.Effort}, "履约会改变相关人物的信任")
				}
			}
			p.Reason = "需要尚未过期的承诺、足够精力和相应事实"
		case "relationship.wait":
			p.Minutes = 30
		case "relationship.conclude":
			for _, ending := range c.Endings {
				if commitmentsResolved(s) && meets(s, ending.Requires) {
					add(ending.Key, ending.Name, 0, nil, "确认后结束本次故事")
				}
			}
			p.Reason = "需满足结局事件条件，并处理完所有待履行承诺"
		case "relationship.leave":
			p.Risk = "未完成的故事目标不再推进"
		}
		if len(a.Parameters) > 0 {
			p.Available = len(p.Plans) > 0
			p.Action.Parameters[0].Options = []string{}
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
func (Module) Execute(content contract.Content, run contract.RunState, request contract.RuntimeRequest) (contract.Transition, error) {
	c, err := ruledata.Singleton[Config](content, DefinitionKind)
	if err != nil {
		return contract.Transition{}, err
	}
	s, err := ruledata.Decode[State](run.Data)
	if err != nil {
		return contract.Transition{}, err
	}
	arg := func(key string) string { value, _ := request.Arguments[key].(string); return value }
	minutes := int64(15)
	if request.ActionKey == "relationship.wait" || request.ActionKey == "relationship.fulfill" {
		minutes = 30
	}
	if request.ActionKey == "relationship.conclude" || request.ActionKey == "relationship.leave" {
		minutes = 0
	}
	started := run.GameTime
	events := advance(c, &s, &run, minutes)
	applied := run.Status == contract.RunActive
	// Time consequences happen before an interaction's completion. A stopped
	// story must not receive facts, trust or promises from unfinished work.
	if run.Status == contract.RunActive {
		switch request.ActionKey {
		case "relationship.listen":
			for _, p := range c.People {
				if p.Key == arg("person") {
					s.Listened = append(s.Listened, p.Key)
					s.Facts = append(s.Facts, p.Fact)
					changeTrust(&s, map[string]int64{p.Key: p.ListenGain})
				}
			}
		case "relationship.choose":
			for _, event := range c.Events {
				for _, choice := range event.Choices {
					if choice.Key == arg("choice") {
						if !meets(s, event.Requires) {
							applied = false
							continue
						}
						s.Choices[event.Key] = choice.Key
						keys := []string{}
						for key := range choice.Facts {
							keys = append(keys, key)
						}
						slices.Sort(keys)
						s.Facts = append(s.Facts, keys...)
						changeTrust(&s, choice.TrustChanges)
						if choice.Promise != "" {
							for _, p := range c.Promises {
								if p.Key == choice.Promise {
									s.Promises[p.Key] = PromiseState{Status: "pending", Started: run.GameTime, Due: run.GameTime + p.Duration}
								}
							}
						}
					}
				}
			}
		case "relationship.fulfill":
			minutes = 30
			for _, p := range c.Promises {
				if p.Key == arg("promise") {
					entry := s.Promises[p.Key]
					if entry.Status != "pending" || entry.Due < run.GameTime || !meets(s, p.Requires) {
						applied = false
						continue
					}
					entry.Status = "fulfilled"
					s.Promises[p.Key] = entry
					s.Effort -= p.Effort
					changeTrust(&s, p.TrustChanges)
				}
			}
		case "relationship.wait":
			minutes = 30
		case "relationship.conclude":
			minutes = 0
			run.Status = contract.RunCompleted
			run.OutcomeKey = arg("ending")
		case "relationship.leave":
			minutes = 0
			run.Status = contract.RunFailed
			run.OutcomeKey = "leave"
		default:
			return contract.Transition{}, contract.Reject("action.unknown", "未知关系动作")
		}
	}
	minutes = run.GameTime - started
	run.Data, err = ruledata.Object(s)
	if err != nil {
		return contract.Transition{}, err
	}
	events = append(events, contract.RuntimeEvent{Kind: "relationship.interacted", Visibility: contract.VisibilityPlayer, Data: map[string]any{"action": request.ActionKey, "minutes": minutes, "completed": applied}})
	if run.Status != contract.RunActive {
		events = append(events, contract.RuntimeEvent{Kind: "relationship.ended", Visibility: contract.VisibilityPlayer, Data: map[string]any{"outcome_key": run.OutcomeKey}})
	}
	result := map[string]any{"minutes": minutes, "outcome_key": run.OutcomeKey, "completed": applied}
	if !applied {
		result["reason"] = "行动过程中承诺到期或故事结束，未获得该次互动结果"
	}
	return contract.Transition{State: run, Events: events, Result: result}, nil
}

func advance(c Config, s *State, run *contract.RunState, minutes int64) []contract.RuntimeEvent {
	end := min(run.GameTime+minutes, c.Deadline)
	events := []contract.RuntimeEvent{}
	promises := slices.Clone(c.Promises)
	sort.SliceStable(promises, func(i, j int) bool { return s.Promises[promises[i].Key].Due < s.Promises[promises[j].Key].Due })
	breaches := int64(0)
	for _, entry := range s.Promises {
		if entry.Status == "broken" {
			breaches++
		}
	}
	for _, p := range promises {
		entry := s.Promises[p.Key]
		if entry.Status == "pending" && entry.Due < end {
			entry.Status = "broken"
			s.Promises[p.Key] = entry
			changeTrust(s, p.BreachChanges)
			breaches++
			events = append(events, contract.RuntimeEvent{Kind: "relationship.promise_expired", Visibility: contract.VisibilityPlayer, Data: map[string]any{"promise": p.Name}})
			if breaches >= c.MaxBreaches {
				run.Status = contract.RunFailed
				run.OutcomeKey = "broken"
				end = entry.Due + 1
				// Resolve every promise expiring at this instant, even when the
				// first one already ended the story.
			}
		}
	}
	run.GameTime = end
	if run.Status == contract.RunActive && run.GameTime >= c.Deadline {
		run.Status = contract.RunFailed
		run.OutcomeKey = "timeout"
	}
	return events
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
	people := []any{}
	labels := map[string]string{}
	for _, p := range c.People {
		people = append(people, map[string]any{"key": p.Key, "name": p.Name, "trust": s.Trust[p.Key], "listened": slices.Contains(s.Listened, p.Key)})
		labels[p.Fact] = p.FactLabel
	}
	for _, e := range c.Events {
		for _, choice := range e.Choices {
			for key, label := range choice.Facts {
				labels[key] = label
			}
		}
	}
	facts := []string{}
	for _, key := range s.Facts {
		facts = append(facts, labels[key])
	}
	promises := []any{}
	statusLabels := map[string]string{"pending": "待履行", "fulfilled": "已履行", "broken": "已违约"}
	for _, p := range c.Promises {
		if entry, exists := s.Promises[p.Key]; exists {
			promises = append(promises, map[string]any{"key": p.Key, "name": p.Name, "status": statusLabels[entry.Status], "remaining_minutes": max(0, entry.Due-run.GameTime), "effort": p.Effort})
		}
	}
	return map[string]any{"people": people, "facts": facts, "promises": promises, "effort": s.Effort, "game_time": run.GameTime, "remaining_minutes": max(0, c.Deadline-run.GameTime), "goal": "倾听人物往事，作出选择并处理自己许下的承诺", "outcome_key": run.OutcomeKey, "listened_count": int64(len(s.Listened)), "fact_count": int64(len(s.Facts)), "event_choice_count": int64(len(s.Choices))}, nil
}
