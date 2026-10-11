package survival

import (
	"fmt"
	"slices"
	"sort"

	"github.com/ialtone/fatecraft/internal/gameplay/components/ruledata"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/gameplay/kernel"
)

type State struct {
	Location   string                      `json:"location"`
	Health     int64                       `json:"health"`
	Stamina    int64                       `json:"stamina"`
	Hunger     int64                       `json:"hunger"`
	Thirst     int64                       `json:"thirst"`
	Stock      map[string]int64            `json:"stock"`
	Remaining  map[string]map[string]int64 `json:"remaining"`
	Discovered []string                    `json:"discovered"`
	Crafted    map[string]int64            `json:"crafted"`
	Weather    string                      `json:"weather"`
}

type Module struct{}

func (Module) NarrativeFactPaths() []string {
	return []string{"health", "stamina", "hunger", "thirst", "location", "game_time", "outcome_key"}
}

func (Module) Actions() []contract.RuntimeAction {
	key := func(name, label string) contract.RuntimeParameter {
		return contract.RuntimeParameter{Key: name, Label: label, Type: "string", Required: true}
	}
	hours := contract.RuntimeParameter{Key: "hours", Label: "小时数", Type: "integer", Required: true, Minimum: 1, Maximum: 8}
	actions := []contract.RuntimeAction{
		{Key: "survival.observe", Label: "观察周围", Description: "辨认相邻地点并消耗十分钟"},
		{Key: "survival.travel", Label: "前往地点", Description: "前往相邻且已发现的地点", Parameters: []contract.RuntimeParameter{key("destination", "目的地")}},
		{Key: "survival.gather", Label: "采集资源", Description: "采集当前地点一份有限资源", Parameters: []contract.RuntimeParameter{key("resource", "资源")}},
		{Key: "survival.craft", Label: "制作物品", Description: "消耗真实材料制作配方物品", Parameters: []contract.RuntimeParameter{key("recipe", "配方")}},
		{Key: "survival.consume", Label: "使用补给", Description: "消耗一份食物或饮水恢复生存指标", Parameters: []contract.RuntimeParameter{key("resource", "补给")}},
		{Key: "survival.rest", Label: "休息", Description: "花费时间恢复体力与少量伤势，期间仍会饥渴", Parameters: []contract.RuntimeParameter{hours}},
		{Key: "survival.wait", Label: "等待", Description: "等待救援窗口，期间仍会消耗补给", Parameters: []contract.RuntimeParameter{hours}},
		{Key: "survival.rescue", Label: "发出求援信号", Description: "在救援窗口消耗已制作装置并撤离"},
		{Key: "survival.abandon", Label: "放弃求援", Description: "结束本次生存挑战"},
	}
	for index := range actions {
		actions[index].Actors = []string{"player", "model"}
		if actions[index].Key == "survival.abandon" || actions[index].Key == "survival.rescue" {
			actions[index].Actors = []string{"player"}
			actions[index].Ending = true
		}
		actions[index].Writes = []string{"location", "health", "stamina", "hunger", "thirst", "stock", "remaining", "discovered", "crafted", "weather"}
		actions[index].Events = []string{"survival.acted", "survival.weather", "survival.ended"}
	}
	return actions
}

func (Module) Initialize(content contract.Content, _ contract.InitialInput) (contract.RunState, error) {
	config, err := ruledata.Singleton[Config](content, DefinitionKind)
	if err != nil {
		return contract.RunState{}, err
	}
	if err := validateConfig(config); err != nil {
		return contract.RunState{}, err
	}
	state := State{Location: config.StartLocation, Health: 100, Stamina: 100, Hunger: 0, Thirst: 0, Stock: config.InitialStock, Remaining: map[string]map[string]int64{}, Discovered: []string{config.StartLocation}, Crafted: map[string]int64{}, Weather: "clear"}
	for _, location := range config.Locations {
		state.Remaining[location.Key] = location.Resources
	}
	data, err := ruledata.Object(state)
	return contract.RunState{SchemaVersion: 1, Status: contract.RunActive, Data: data}, err
}

func (Module) ValidateState(content contract.Content, run contract.RunState) error {
	config, err := ruledata.Singleton[Config](content, DefinitionKind)
	if err != nil {
		return err
	}
	state, err := ruledata.Decode[State](run.Data)
	if err != nil {
		return err
	}
	if run.SchemaVersion != 1 || state.Health < 0 || state.Health > 100 || state.Stamina < 0 || state.Stamina > 100 || state.Hunger < 0 || state.Hunger > 100 || state.Thirst < 0 || state.Thirst > 100 || (state.Weather != "clear" && state.Weather != "storm") {
		return fmt.Errorf("生存指标或状态版本无效")
	}
	if err := ruledata.ValidateStock(state.Stock); err != nil {
		return err
	}
	if err := ruledata.ValidateStock(state.Crafted); err != nil {
		return err
	}
	locations := map[string]Location{}
	for _, location := range config.Locations {
		locations[location.Key] = location
	}
	if locations[state.Location].Key == "" || !slices.Contains(state.Discovered, state.Location) || len(state.Remaining) != len(config.Locations) || len(state.Discovered) > len(config.Locations) {
		return fmt.Errorf("地点状态无效")
	}
	seen := map[string]bool{}
	for _, key := range state.Discovered {
		if locations[key].Key == "" || seen[key] {
			return fmt.Errorf("发现地点无效")
		}
		seen[key] = true
	}
	resources := map[string]bool{}
	for _, resource := range config.Resources {
		resources[resource.Key] = true
	}
	for key := range state.Stock {
		if !resources[key] {
			return fmt.Errorf("持有未知资源")
		}
	}
	for key := range state.Crafted {
		if !resources[key] {
			return fmt.Errorf("未知制作记录")
		}
	}
	for key, stock := range state.Remaining {
		location, ok := locations[key]
		if !ok {
			return fmt.Errorf("未知资源地点")
		}
		if err := ruledata.ValidateStock(stock); err != nil {
			return err
		}
		for resource, quantity := range stock {
			if _, exists := location.Resources[resource]; !exists || quantity > location.Resources[resource] {
				return fmt.Errorf("地点资源超过初始上限")
			}
		}
	}
	if run.Status == contract.RunActive && (state.Health == 0 || run.GameTime >= config.DeadlineMinutes) {
		return fmt.Errorf("已耗尽生存条件但未结束")
	}
	if run.OutcomeKey == "rescued" && (state.Crafted[config.RescueItem] < 1 || state.Location != config.RescueLocation || run.GameTime < config.RescueAfterMinutes || state.Health == 0) {
		return fmt.Errorf("救援事实不完整")
	}
	return nil
}

func (m Module) Preview(content contract.Content, run contract.RunState, actor string) ([]contract.ActionPreview, error) {
	config, err := ruledata.Singleton[Config](content, DefinitionKind)
	if err != nil {
		return nil, err
	}
	state, err := ruledata.Decode[State](run.Data)
	if err != nil {
		return nil, err
	}
	location := findLocation(config, state.Location)
	labels := map[string]string{"stamina": "体力"}
	for _, resource := range config.Resources {
		labels[resource.Key] = resource.Name
	}
	previews := []contract.ActionPreview{}
	for _, action := range m.Actions() {
		if !slices.Contains(action.Actors, actor) {
			continue
		}
		preview := contract.ActionPreview{Action: action, Available: true, Risk: "行动耗时会增加饥渴；恶劣天气可能造成伤害", CostLabels: labels}
		options := []string{}
		switch action.Key {
		case "survival.observe":
			preview.Minutes = 10
		case "survival.travel":
			for _, key := range location.Connections {
				if slices.Contains(state.Discovered, key) {
					options = append(options, key)
					destination := findLocation(config, key)
					preview.Plans = append(preview.Plans, contract.ActionPlan{Label: destination.Name, Arguments: map[string]any{"destination": key}, Minutes: destination.TravelMinutes, Costs: map[string]int64{"stamina": 5}})
				}
			}
			preview.Available = len(options) > 0 && state.Stamina >= 5
			preview.Reason = "没有已发现的相邻地点，或体力不足"
		case "survival.gather":
			for key, quantity := range state.Remaining[state.Location] {
				if quantity > 0 {
					options = append(options, key)
					preview.Plans = append(preview.Plans, contract.ActionPlan{Label: labels[key], Arguments: map[string]any{"resource": key}, Minutes: location.GatherMinutes, Costs: map[string]int64{"stamina": 10}})
				}
			}
			preview.Available = len(options) > 0 && state.Stamina >= 10
			preview.Reason = "当前位置资源已耗尽，或体力不足"
			preview.Minutes = location.GatherMinutes
		case "survival.craft":
			for _, recipe := range config.Recipes {
				if ruledata.CanPay(state.Stock, recipe.Inputs) {
					options = append(options, recipe.Key)
					costs := map[string]int64{"stamina": 5}
					for key, quantity := range recipe.Inputs {
						costs[key] = quantity
					}
					preview.Plans = append(preview.Plans, contract.ActionPlan{Label: recipe.Name, Arguments: map[string]any{"recipe": recipe.Key}, Minutes: recipe.Minutes, Costs: costs})
				}
			}
			preview.Available = len(options) > 0 && state.Stamina >= 5
			preview.Reason = "材料或体力不足"
		case "survival.consume":
			for _, resource := range config.Resources {
				if state.Stock[resource.Key] > 0 && (resource.HungerRecovery > 0 && state.Hunger > 0 || resource.ThirstRecovery > 0 && state.Thirst > 0) {
					options = append(options, resource.Key)
					preview.Plans = append(preview.Plans, contract.ActionPlan{Label: resource.Name, Arguments: map[string]any{"resource": resource.Key}, Minutes: 10, Costs: map[string]int64{resource.Key: 1}})
				}
			}
			preview.Available = len(options) > 0
			preview.Reason = "没有需要使用的有效补给"
			preview.Minutes = 10
		case "survival.rest":
			preview.Minutes = 60
			preview.Available = state.Stamina < 100 || state.Health < 100
			preview.Reason = "当前无需恢复"
		case "survival.wait":
			preview.Minutes = 60
		case "survival.rescue":
			preview.Available = state.Location == config.RescueLocation && state.Stock[config.RescueItem] > 0 && state.Crafted[config.RescueItem] > 0 && run.GameTime >= config.RescueAfterMinutes
			preview.Reason = "需要携带亲手制作的求援装置，在救援窗口到达救援地点"
			preview.Risk = ""
			preview.Costs = map[string]int64{config.RescueItem: 1}
		case "survival.abandon":
			preview.Risk = "本次挑战将结束"
		}
		if action.Key == "survival.rest" || action.Key == "survival.wait" {
			for hours := int64(1); hours <= 8; hours++ {
				preview.Plans = append(preview.Plans, contract.ActionPlan{Label: fmt.Sprintf("%d 小时", hours), Arguments: map[string]any{"hours": hours}, Minutes: hours * 60})
			}
		}
		if len(action.Parameters) > 0 && action.Parameters[0].Type == "string" {
			sort.Strings(options)
			preview.Action.Parameters[0].Options = options
			preview.Action.Parameters[0].OptionLabels = map[string]string{}
			for _, plan := range preview.Plans {
				value, _ := plan.Arguments[action.Parameters[0].Key].(string)
				preview.Action.Parameters[0].OptionLabels[value] = plan.Label
			}
			sort.Slice(preview.Plans, func(i, j int) bool {
				return fmt.Sprint(preview.Plans[i].Arguments[action.Parameters[0].Key]) < fmt.Sprint(preview.Plans[j].Arguments[action.Parameters[0].Key])
			})
		}
		if preview.Available {
			preview.Reason = ""
		}
		previews = append(previews, preview)
	}
	return previews, nil
}

func (Module) Execute(content contract.Content, run contract.RunState, request contract.RuntimeRequest) (contract.Transition, error) {
	config, err := ruledata.Singleton[Config](content, DefinitionKind)
	if err != nil {
		return contract.Transition{}, err
	}
	state, err := ruledata.Decode[State](run.Data)
	if err != nil {
		return contract.Transition{}, err
	}
	minutes := int64(0)
	started := run.GameTime
	var finish func() error
	argument := func(key string) string { value, _ := request.Arguments[key].(string); return value }
	switch request.ActionKey {
	case "survival.observe":
		minutes = 10
		finish = func() error {
			for _, key := range findLocation(config, state.Location).Connections {
				if !slices.Contains(state.Discovered, key) {
					state.Discovered = append(state.Discovered, key)
				}
			}
			return nil
		}
	case "survival.travel":
		minutes = findLocation(config, argument("destination")).TravelMinutes
		finish = func() error { state.Location = argument("destination"); return nil }
		state.Stamina -= 5
	case "survival.gather":
		resource := argument("resource")
		state.Remaining[state.Location][resource]--
		finish = func() error { return ruledata.Add(state.Stock, resource, 1) }
		minutes = findLocation(config, state.Location).GatherMinutes
		state.Stamina -= 10
	case "survival.craft":
		for _, recipe := range config.Recipes {
			if recipe.Key == argument("recipe") {
				if err := ruledata.Pay(state.Stock, recipe.Inputs); err != nil {
					return contract.Transition{}, err
				}
				finish = func() error {
					if err := ruledata.Add(state.Stock, recipe.Output, recipe.Quantity); err != nil {
						return err
					}
					return ruledata.Add(state.Crafted, recipe.Output, recipe.Quantity)
				}
				minutes = recipe.Minutes
				state.Stamina -= 5
				break
			}
		}
	case "survival.consume":
		resourceKey := argument("resource")
		if err := ruledata.Pay(state.Stock, map[string]int64{resourceKey: 1}); err != nil {
			return contract.Transition{}, err
		}
		for _, resource := range config.Resources {
			if resource.Key == resourceKey {
				state.Hunger = max(0, state.Hunger-resource.HungerRecovery)
				state.Thirst = max(0, state.Thirst-resource.ThirstRecovery)
			}
		}
		minutes = 10
	case "survival.rest", "survival.wait":
		hours, _ := kernel.Integer(request.Arguments["hours"])
		minutes = hours * 60
	case "survival.rescue":
		if err := ruledata.Pay(state.Stock, map[string]int64{config.RescueItem: 1}); err != nil {
			return contract.Transition{}, err
		}
		run.Status, run.OutcomeKey = contract.RunCompleted, "rescued"
	case "survival.abandon":
		run.Status, run.OutcomeKey = contract.RunFailed, "abandoned"
	default:
		return contract.Transition{}, contract.Reject("action.unknown", "未知生存动作")
	}
	events := []contract.RuntimeEvent{{Kind: "survival.acted", Visibility: contract.VisibilityPlayer, Data: map[string]any{"action": request.ActionKey, "minutes": minutes}}}
	if minutes > 0 {
		// Resolve each crossed hour in order. Later rest cannot revive someone
		// who died earlier in the same action, nor can new thirst charge old hours.
		end := min(run.GameTime+minutes, config.DeadlineMinutes)
		for tick := (run.GameTime/60 + 1) * 60; tick <= end; tick += 60 {
			state.Hunger = min(100, state.Hunger+config.HungerPerHour)
			state.Thirst = min(100, state.Thirst+config.ThirstPerHour)
			if state.Hunger == 100 || state.Thirst == 100 {
				state.Health = max(0, state.Health-15)
			}
			if state.Health == 0 {
				end = tick
				break
			}
			if request.ActionKey == "survival.rest" {
				state.Stamina = min(100, state.Stamina+15)
				if state.Hunger < 100 && state.Thirst < 100 {
					state.Health = min(100, state.Health+3)
				}
			}
		}
		if state.Health > 0 && end < config.DeadlineMinutes {
			weather, err := request.Random.Intn("survival.weather", 100)
			if err != nil {
				return contract.Transition{}, err
			}
			state.Weather = "clear"
			if weather < config.WeatherRiskPercent {
				state.Weather = "storm"
				state.Health = max(0, state.Health-config.WeatherDamage)
				events = append(events, contract.RuntimeEvent{Kind: "survival.weather", Visibility: contract.VisibilityPlayer, Data: map[string]any{"weather": state.Weather, "damage": config.WeatherDamage}})
			}
		}
		run.GameTime = end
		if state.Health == 0 {
			run.Status, run.OutcomeKey = contract.RunFailed, "perished"
		} else if run.GameTime >= config.DeadlineMinutes {
			run.Status, run.OutcomeKey = contract.RunFailed, "timeout"
		}
	}
	if run.Status != contract.RunActive {
		events = append(events, contract.RuntimeEvent{Kind: "survival.ended", Visibility: contract.VisibilityPlayer, Data: map[string]any{"outcome_key": run.OutcomeKey}})
	}
	if run.Status == contract.RunActive && finish != nil {
		if err := finish(); err != nil {
			return contract.Transition{}, err
		}
	}
	minutes = run.GameTime - started
	events[0].Data["minutes"] = minutes
	run.Data, err = ruledata.Object(state)
	if err != nil {
		return contract.Transition{}, err
	}
	return contract.Transition{State: run, Events: events, Result: map[string]any{"action": request.ActionKey, "minutes": minutes, "outcome_key": run.OutcomeKey}}, nil
}

func (Module) Project(content contract.Content, run contract.RunState, _ string) (map[string]any, error) {
	config, err := ruledata.Singleton[Config](content, DefinitionKind)
	if err != nil {
		return nil, err
	}
	state, err := ruledata.Decode[State](run.Data)
	if err != nil {
		return nil, err
	}
	inventory := []any{}
	for _, resource := range config.Resources {
		if state.Stock[resource.Key] > 0 {
			inventory = append(inventory, map[string]any{"key": resource.Key, "name": resource.Name, "quantity": state.Stock[resource.Key]})
		}
	}
	locations := []any{}
	for _, location := range config.Locations {
		if slices.Contains(state.Discovered, location.Key) {
			locations = append(locations, map[string]any{"key": location.Key, "name": location.Name})
		}
	}
	return map[string]any{"health": state.Health, "stamina": state.Stamina, "hunger": state.Hunger, "thirst": state.Thirst, "location": findLocation(config, state.Location).Name, "locations": locations, "inventory": inventory, "weather": state.Weather, "game_time": run.GameTime, "rescue_after_minutes": config.RescueAfterMinutes, "deadline_minutes": config.DeadlineMinutes, "goal": "制作求援装置，携带到救援地点并等待救援窗口", "outcome_key": run.OutcomeKey}, nil
}

func findLocation(config Config, key string) Location {
	for _, location := range config.Locations {
		if location.Key == key {
			return location
		}
	}
	return Location{}
}
