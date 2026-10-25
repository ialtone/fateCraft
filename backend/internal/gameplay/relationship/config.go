package relationship

import (
	"fmt"
	"github.com/ialtone/fatecraft/internal/gameplay/components/ruledata"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
)

const EngineKey = "relationship.story"
const DefinitionKind = "relationship.story"

type Person struct {
	Key        string `json:"key"`
	Name       string `json:"name"`
	Trust      int64  `json:"trust"`
	ListenGain int64  `json:"listen_gain"`
	Fact       string `json:"fact"`
	FactLabel  string `json:"fact_label"`
}
type Conditions struct {
	Facts    []string         `json:"facts"`
	Trust    map[string]int64 `json:"trust" minimum:"-100" maximum:"100"`
	Promises []string         `json:"promises"`
}
type Choice struct {
	Key          string            `json:"key"`
	Name         string            `json:"name"`
	Facts        map[string]string `json:"facts"`
	TrustChanges map[string]int64  `json:"trust_changes" minimum:"-40" maximum:"40"`
	Promise      string            `json:"promise"`
}
type Event struct {
	Key      string     `json:"key"`
	Name     string     `json:"name"`
	Requires Conditions `json:"requires"`
	Choices  []Choice   `json:"choices"`
}
type Promise struct {
	Key           string           `json:"key"`
	Name          string           `json:"name"`
	Requires      Conditions       `json:"requires"`
	Effort        int64            `json:"effort"`
	Duration      int64            `json:"duration"`
	TrustChanges  map[string]int64 `json:"trust_changes" minimum:"-40" maximum:"40"`
	BreachChanges map[string]int64 `json:"breach_changes" minimum:"-40" maximum:"40"`
}
type Ending struct {
	Key      string     `json:"key"`
	Name     string     `json:"name"`
	Requires Conditions `json:"requires"`
}
type Config struct {
	People      []Person  `json:"people"`
	Events      []Event   `json:"events"`
	Promises    []Promise `json:"promises"`
	Endings     []Ending  `json:"endings"`
	Effort      int64     `json:"effort"`
	MaxBreaches int64     `json:"max_breaches"`
	Deadline    int64     `json:"deadline"`
}

func validate(c Config) error {
	if len(c.People) < 3 || len(c.People) > 12 || len(c.Events) < 2 || len(c.Events) > 16 || len(c.Promises) < 1 || len(c.Promises) > 12 || len(c.Endings) < 2 || len(c.Endings) > 8 || c.Effort < 1 || c.Effort > 20 || c.MaxBreaches < 1 || c.MaxBreaches > int64(len(c.Promises)) || c.Deadline < 120 || c.Deadline > 7*1440 {
		return fmt.Errorf("关系角色、事件、承诺或时间配置无效")
	}
	people := map[string]bool{}
	facts := map[string]string{}
	factChoices := map[string]string{}
	choiceEvents := map[string]string{}
	promises := map[string]bool{}
	promiseSources := map[string]bool{}
	for _, p := range c.People {
		if !contract.ValidStableKey(p.Key) || people[p.Key] || p.Name == "" || p.Trust < 0 || p.Trust > 80 || p.ListenGain < 0 || p.ListenGain > 20 || !contract.ValidStableKey(p.Fact) || p.FactLabel == "" || facts[p.Fact] != "" {
			return fmt.Errorf("人物或已知事实定义无效 %s", p.Key)
		}
		people[p.Key] = true
		facts[p.Fact] = p.FactLabel
	}
	changes := func(values map[string]int64) error {
		if len(values) > len(c.People) {
			return fmt.Errorf("关系变更超过角色数量")
		}
		for key, n := range values {
			if !people[key] || n < -40 || n > 40 {
				return fmt.Errorf("角色关系变更无效 %s", key)
			}
		}
		return nil
	}
	for _, p := range c.Promises {
		if !contract.ValidStableKey(p.Key) || promises[p.Key] || p.Name == "" || p.Effort < 1 || p.Effort > c.Effort || p.Duration < 30 || p.Duration > c.Deadline {
			return fmt.Errorf("承诺无效 %s", p.Key)
		}
		if err := changes(p.TrustChanges); err != nil {
			return err
		}
		if err := changes(p.BreachChanges); err != nil {
			return err
		}
		promises[p.Key] = true
	}
	events := map[string]bool{}
	for _, e := range c.Events {
		if !contract.ValidStableKey(e.Key) || events[e.Key] || e.Name == "" || len(e.Choices) < 2 || len(e.Choices) > 4 {
			return fmt.Errorf("事件无效 %s", e.Key)
		}
		events[e.Key] = true
		for _, choice := range e.Choices {
			if !contract.ValidStableKey(choice.Key) || choiceEvents[choice.Key] != "" || choice.Name == "" || len(choice.Facts) > 4 {
				return fmt.Errorf("事件选项无效 %s", choice.Key)
			}
			choiceEvents[choice.Key] = e.Key
			if err := changes(choice.TrustChanges); err != nil {
				return err
			}
			if choice.Promise != "" {
				if !promises[choice.Promise] || promiseSources[choice.Promise] {
					return fmt.Errorf("承诺必须有唯一事件来源")
				}
				promiseSources[choice.Promise] = true
			}
			for key, label := range choice.Facts {
				if !contract.ValidStableKey(key) || label == "" || facts[key] != "" {
					return fmt.Errorf("事实定义重复或无效 %s", key)
				}
				facts[key] = label
				factChoices[key] = choice.Key
			}
		}
	}
	conditions := func(r Conditions) error {
		if len(r.Facts) > 32 || len(r.Trust) > len(c.People) || len(r.Promises) > len(c.Promises) {
			return fmt.Errorf("条件超过预算")
		}
		chosen := map[string]string{}
		for _, fact := range r.Facts {
			if facts[fact] == "" {
				return fmt.Errorf("未知事实条件 %s", fact)
			}
			choice := factChoices[fact]
			event := choiceEvents[choice]
			if event != "" {
				if prior := chosen[event]; prior != "" && prior != choice {
					return fmt.Errorf("条件要求同一事件的互斥选项")
				}
				chosen[event] = choice
			}
		}
		for key, n := range r.Trust {
			if !people[key] || n < -100 || n > 100 {
				return fmt.Errorf("信任前提无效")
			}
		}
		for _, key := range r.Promises {
			if !promises[key] || !promiseSources[key] {
				return fmt.Errorf("承诺条件缺少来源")
			}
		}
		return nil
	}
	for _, e := range c.Events {
		if err := conditions(e.Requires); err != nil {
			return fmt.Errorf("事件 %s: %w", e.Key, err)
		}
	}
	for _, p := range c.Promises {
		if !promiseSources[p.Key] {
			return fmt.Errorf("承诺 %s 缺少来源", p.Key)
		}
		if err := conditions(p.Requires); err != nil {
			return err
		}
	}
	endingKeys := map[string]bool{}
	for _, e := range c.Endings {
		if !contract.ValidStableKey(e.Key) || endingKeys[e.Key] || e.Name == "" || e.Key == "broken" || e.Key == "timeout" || e.Key == "leave" || len(e.Requires.Facts) == 0 {
			return fmt.Errorf("结局需要唯一名称和事件事实")
		}
		if err := conditions(e.Requires); err != nil {
			return err
		}
		endingKeys[e.Key] = true
	}
	// Check causal reachability without evaluating text or arbitrary expressions.
	reached := map[string]bool{}
	for _, p := range c.People {
		reached[p.Fact] = true
	}
	made := map[string]bool{}
	for range len(c.Events) + len(c.Promises) {
		for _, e := range c.Events {
			ok := true
			for _, f := range e.Requires.Facts {
				ok = ok && reached[f]
			}
			for _, p := range e.Requires.Promises {
				ok = ok && made[p]
			}
			if ok {
				for _, choice := range e.Choices {
					for f := range choice.Facts {
						reached[f] = true
					}
					if choice.Promise != "" {
						made[choice.Promise] = true
					}
				}
			}
		}
	}
	for _, e := range c.Endings {
		for _, f := range e.Requires.Facts {
			if !reached[f] {
				return fmt.Errorf("结局 %s 的事件事实不可达", e.Key)
			}
		}
	}
	return validateEndingDependencies(c)
}

// Each ending has a single coherent causal history. Facts from mutually
// exclusive choices must not become jointly reachable through a union of paths.
func validateEndingDependencies(c Config) error {
	type source struct {
		event  Event
		choice Choice
	}
	facts, promises := map[string]source{}, map[string]source{}
	for _, e := range c.Events {
		for _, choice := range e.Choices {
			for f := range choice.Facts {
				facts[f] = source{e, choice}
			}
			if choice.Promise != "" {
				promises[choice.Promise] = source{e, choice}
			}
		}
	}
	promiseByKey := map[string]Promise{}
	for _, p := range c.Promises {
		promiseByKey[p.Key] = p
	}
	for _, ending := range c.Endings {
		selected, visiting, done := map[string]string{}, map[string]bool{}, map[string]bool{}
		effort := int64(0)
		var conditions func(Conditions) error
		var choose func(source) error
		choose = func(src source) error {
			if src.event.Key == "" {
				return nil
			}
			if prior := selected[src.event.Key]; prior != "" && prior != src.choice.Key {
				return fmt.Errorf("互斥事件依赖 %s", src.event.Key)
			}
			selected[src.event.Key] = src.choice.Key
			key := "event:" + src.event.Key
			if visiting[key] {
				return fmt.Errorf("循环事件依赖 %s", src.event.Key)
			}
			if done[key] {
				return nil
			}
			visiting[key] = true
			if err := conditions(src.event.Requires); err != nil {
				return err
			}
			delete(visiting, key)
			done[key] = true
			return nil
		}
		conditions = func(r Conditions) error {
			for _, f := range r.Facts {
				if err := choose(facts[f]); err != nil {
					return err
				}
			}
			for _, p := range r.Promises {
				key := "promise:" + p
				if visiting[key] {
					return fmt.Errorf("循环承诺依赖 %s", p)
				}
				if done[key] {
					continue
				}
				visiting[key] = true
				if err := choose(promises[p]); err != nil {
					return err
				}
				if err := conditions(promiseByKey[p].Requires); err != nil {
					return err
				}
				effort += promiseByKey[p].Effort
				delete(visiting, key)
				done[key] = true
			}
			return nil
		}
		if err := conditions(ending.Requires); err != nil {
			return fmt.Errorf("结局 %s: %w", ending.Key, err)
		}
		if effort > c.Effort {
			return fmt.Errorf("结局 %s 的承诺精力不足", ending.Key)
		}
	}
	return nil
}
func ValidateContent(content contract.Content) error {
	c, err := ruledata.Singleton[Config](content, DefinitionKind)
	if err != nil {
		return err
	}
	if err := validate(c); err != nil {
		return err
	}
	keys := []string{"broken", "timeout", "leave"}
	for _, e := range c.Endings {
		keys = append(keys, e.Key)
	}
	for _, key := range keys {
		found := false
		for _, o := range content.Outcomes {
			found = found || o.Key == key && o.Enabled
		}
		if !found {
			return fmt.Errorf("缺少关系结局 %s", key)
		}
	}
	return nil
}
func Descriptor() contract.ModuleDescriptor {
	schema := ruledata.Schema[Config](map[string]string{"people": "人物", "key": "标识", "name": "名称", "trust": "信任门槛/初始信任", "listen_gain": "首次倾听的信任变化", "fact": "倾听获得的事实", "fact_label": "事实说明", "facts": "事实条件/结果", "promises": "承诺", "events": "关系事件", "requires": "前提条件", "choices": "互斥选择", "trust_changes": "履行后的信任变化", "promise": "建立的承诺", "effort": "可用精力/所需精力", "duration": "承诺期限（分钟）", "breach_changes": "违约后的信任变化", "endings": "成功结局", "max_breaches": "导致关系破裂的违约次数", "deadline": "故事期限（分钟）"})
	return contract.WithCampaignRuntime(contract.ModuleDescriptor{Key: EngineKey, Version: 1, Name: "关系叙事", Category: "relationship", Description: "通过一次性事件、人物选择与有期限的承诺改变关系。", Modes: []string{contract.RuntimeModeCampaign}, Actions: []string{"update_state", "domain_action"}, Verifiers: []string{"core.always", "core.conditions"}, Runtime: Module{}, AbandonAction: "relationship.leave", DefinitionLabels: map[string]string{DefinitionKind: "关系故事"}, DefinitionSchemas: map[string]map[string]any{DefinitionKind: schema}, DefinitionValidators: map[string]contract.DefinitionValidator{DefinitionKind: func(d contract.Definition) error {
		c, err := ruledata.Decode[Config](d.Content)
		if err != nil {
			return err
		}
		return validate(c)
	}}, ValidateContent: ValidateContent})
}
