package survival

import (
	"fmt"
	"slices"

	"github.com/ialtone/fatecraft/internal/gameplay/components/ruledata"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
)

const EngineKey = "survival.exploration"
const DefinitionKind = "survival.scenario"

type Resource struct {
	Key            string `json:"key"`
	Name           string `json:"name"`
	HungerRecovery int64  `json:"hunger_recovery"`
	ThirstRecovery int64  `json:"thirst_recovery"`
}
type Location struct {
	Key           string           `json:"key"`
	Name          string           `json:"name"`
	Connections   []string         `json:"connections"`
	Resources     map[string]int64 `json:"resources"`
	TravelMinutes int64            `json:"travel_minutes"`
	GatherMinutes int64            `json:"gather_minutes"`
}
type Recipe struct {
	Key      string           `json:"key"`
	Name     string           `json:"name"`
	Inputs   map[string]int64 `json:"inputs"`
	Output   string           `json:"output"`
	Quantity int64            `json:"quantity"`
	Minutes  int64            `json:"minutes"`
}
type Config struct {
	StartLocation      string           `json:"start_location"`
	RescueLocation     string           `json:"rescue_location"`
	RescueItem         string           `json:"rescue_item"`
	RescueAfterMinutes int64            `json:"rescue_after_minutes"`
	DeadlineMinutes    int64            `json:"deadline_minutes"`
	HungerPerHour      int64            `json:"hunger_per_hour"`
	ThirstPerHour      int64            `json:"thirst_per_hour"`
	WeatherRiskPercent int64            `json:"weather_risk_percent"`
	WeatherDamage      int64            `json:"weather_damage"`
	InitialStock       map[string]int64 `json:"initial_stock"`
	Resources          []Resource       `json:"resources"`
	Locations          []Location       `json:"locations"`
	Recipes            []Recipe         `json:"recipes"`
}

func validateConfig(config Config) error {
	if len(config.Locations) < 2 || len(config.Locations) > 32 || len(config.Resources) < 3 || len(config.Resources) > 32 || len(config.Recipes) < 1 || len(config.Recipes) > 32 {
		return fmt.Errorf("地点、资源或配方数量不在支持范围内")
	}
	if config.DeadlineMinutes < 240 || config.DeadlineMinutes > 60*24*30 || config.RescueAfterMinutes < 0 || config.RescueAfterMinutes >= config.DeadlineMinutes || config.HungerPerHour < 1 || config.HungerPerHour > 20 || config.ThirstPerHour < 1 || config.ThirstPerHour > 20 || config.WeatherRiskPercent < 0 || config.WeatherRiskPercent > 100 || config.WeatherDamage < 0 || config.WeatherDamage > 50 {
		return fmt.Errorf("时间、生存消耗或环境风险参数无效")
	}
	resources := map[string]bool{}
	for _, resource := range config.Resources {
		if !contract.ValidStableKey(resource.Key) || resource.Name == "" || resources[resource.Key] || resource.HungerRecovery < 0 || resource.HungerRecovery > 100 || resource.ThirstRecovery < 0 || resource.ThirstRecovery > 100 {
			return fmt.Errorf("资源定义无效: %s", resource.Key)
		}
		resources[resource.Key] = true
	}
	stockValid := func(stock map[string]int64) error {
		if err := ruledata.ValidateStock(stock); err != nil {
			return err
		}
		for key := range stock {
			if !resources[key] {
				return fmt.Errorf("引用未知资源 %s", key)
			}
		}
		return nil
	}
	if err := stockValid(config.InitialStock); err != nil {
		return err
	}
	locations := map[string]Location{}
	available := map[string]bool{}
	for key, quantity := range config.InitialStock {
		if quantity > 0 {
			available[key] = true
		}
	}
	for _, location := range config.Locations {
		if !contract.ValidStableKey(location.Key) || location.Name == "" || locations[location.Key].Key != "" || location.TravelMinutes < 1 || location.TravelMinutes > 1440 || location.GatherMinutes < 1 || location.GatherMinutes > 240 || len(location.Connections) > 8 {
			return fmt.Errorf("地点定义无效: %s", location.Key)
		}
		if err := stockValid(location.Resources); err != nil {
			return err
		}
		locations[location.Key] = location
	}
	if locations[config.StartLocation].Key == "" || locations[config.RescueLocation].Key == "" || !resources[config.RescueItem] {
		return fmt.Errorf("初始地点、救援地点或求援装置不存在")
	}
	for _, location := range config.Locations {
		seen := map[string]bool{}
		for _, next := range location.Connections {
			if next == location.Key || seen[next] || locations[next].Key == "" || !slices.Contains(locations[next].Connections, location.Key) {
				return fmt.Errorf("地点 %s 的连接必须存在、唯一且可返回", location.Key)
			}
			seen[next] = true
		}
	}
	visited := map[string]bool{config.StartLocation: true}
	queue := []string{config.StartLocation}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for key, quantity := range locations[current].Resources {
			if quantity > 0 {
				available[key] = true
			}
		}
		for _, next := range locations[current].Connections {
			if !visited[next] {
				visited[next] = true
				queue = append(queue, next)
			}
		}
	}
	if !visited[config.RescueLocation] {
		return fmt.Errorf("救援地点不可达")
	}
	recipes := map[string]bool{}
	for _, recipe := range config.Recipes {
		if !contract.ValidStableKey(recipe.Key) || recipe.Name == "" || recipes[recipe.Key] || !resources[recipe.Output] || recipe.Quantity < 1 || recipe.Quantity > 100 || recipe.Minutes < 1 || recipe.Minutes > 1440 || len(recipe.Inputs) == 0 {
			return fmt.Errorf("配方无效: %s", recipe.Key)
		}
		if err := stockValid(recipe.Inputs); err != nil {
			return err
		}
		for key, quantity := range recipe.Inputs {
			if quantity < 1 || key == recipe.Output {
				return fmt.Errorf("配方 %s 必须消耗正数的其他材料", recipe.Key)
			}
		}
		recipes[recipe.Key] = true
	}
	rescueCraftable := false
	for range config.Recipes {
		for _, recipe := range config.Recipes {
			possible := true
			for key := range recipe.Inputs {
				possible = possible && available[key]
			}
			if possible {
				available[recipe.Output] = true
				if recipe.Output == config.RescueItem {
					rescueCraftable = true
				}
			}
		}
	}
	if !rescueCraftable {
		return fmt.Errorf("求援装置缺少可达的制作来源")
	}
	return validateRescueMaterials(config, visited)
}

func ValidateContent(content contract.Content) error {
	config, err := ruledata.Singleton[Config](content, DefinitionKind)
	if err != nil {
		return err
	}
	if err := validateConfig(config); err != nil {
		return err
	}
	for _, key := range []string{"rescued", "perished", "timeout", "abandoned"} {
		found := false
		for _, outcome := range content.Outcomes {
			if outcome.Key == key && outcome.Enabled {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("缺少已启用结局 %s", key)
		}
	}
	return nil
}

func Descriptor() contract.ModuleDescriptor {
	return contract.ModuleDescriptor{DefinitionSchemas: map[string]map[string]any{DefinitionKind: ruledata.Schema[Config](map[string]string{"key": "标识", "name": "名称", "start_location": "初始地点", "rescue_location": "救援地点", "rescue_item": "求援装置", "rescue_after_minutes": "救援开始时间（分钟）", "deadline_minutes": "生存期限（分钟）", "hunger_per_hour": "每小时饥饿增长", "thirst_per_hour": "每小时口渴增长", "weather_risk_percent": "恶劣天气概率（%）", "weather_damage": "天气伤害", "initial_stock": "初始库存", "resources": "资源", "locations": "地点", "recipes": "制作配方", "hunger_recovery": "饥饿恢复", "thirst_recovery": "口渴恢复", "connections": "相邻地点", "travel_minutes": "进入耗时（分钟）", "gather_minutes": "采集耗时（分钟）", "inputs": "消耗材料", "output": "产出物品", "quantity": "产出数量", "minutes": "耗时（分钟）"})}, DefinitionLabels: map[string]string{DefinitionKind: "生存场景"}, Key: EngineKey, Version: 1, Name: "生存探索", Category: "survival", Description: "以有限补给、制作、环境风险和救援窗口组成生存流程。", Modes: []string{contract.RuntimeModeChallenge}, Actions: []string{"update_state", "domain_action", "complete_challenge"}, Verifiers: []string{"core.conditions"}, SettlementHandlers: []string{"core.challenge_completion"}, Runtime: Module{}, AbandonAction: "survival.abandon", DefinitionValidators: map[string]contract.DefinitionValidator{DefinitionKind: func(definition contract.Definition) error {
		config, err := ruledata.Decode[Config](definition.Content)
		if err != nil {
			return err
		}
		return validateConfig(config)
	}}, ValidateContent: ValidateContent}
}
