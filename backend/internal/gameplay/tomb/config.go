package tomb

import (
	"fmt"
	"github.com/ialtone/fatecraft/internal/gameplay/components/expedition"
	"github.com/ialtone/fatecraft/internal/gameplay/components/ruledata"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
)

const EngineKey = "expedition.tomb"
const DefinitionKind = "tomb.expedition"

type Clue struct {
	Key      string   `json:"key"`
	Name     string   `json:"name"`
	Node     string   `json:"node"`
	Requires []string `json:"requires"`
}
type Trap struct {
	Key    string   `json:"key"`
	Name   string   `json:"name"`
	Node   string   `json:"node"`
	Clues  []string `json:"clues"`
	Tools  int64    `json:"tools"`
	Risk   int64    `json:"risk"`
	Damage int64    `json:"damage"`
}
type Config struct {
	World expedition.Config `json:"world"`
	Clues []Clue            `json:"clues"`
	Traps []Trap            `json:"traps"`
	Light int64             `json:"light"`
	Tools int64             `json:"tools"`
}

func trapAt(c Config, node string) Trap {
	for _, p := range c.Traps {
		if p.Node == node {
			return p
		}
	}
	return Trap{}
}
func validate(c Config) error {
	if err := expedition.Validate(c.World); err != nil {
		return err
	}
	if len(c.Clues) < 2 || len(c.Clues) > 16 || len(c.Traps) < 1 || len(c.Traps) > 8 || c.Light < 30 || c.Light > 1440 || c.Tools < 0 || c.Tools > 10 {
		return fmt.Errorf("墓室线索、机关、照明或工具配置无效")
	}
	clues := map[string]Clue{}
	for _, clue := range c.Clues {
		if !contract.ValidStableKey(clue.Key) || clues[clue.Key].Key != "" || clue.Name == "" || expedition.Find(c.World, clue.Node).Key == "" || len(clue.Requires) > 8 {
			return fmt.Errorf("线索定义无效 %s", clue.Key)
		}
		clues[clue.Key] = clue
	}
	for _, clue := range c.Clues {
		for _, key := range clue.Requires {
			if key == clue.Key || clues[key].Key == "" {
				return fmt.Errorf("线索 %s 的前提无效", clue.Key)
			}
		}
	}
	traps, nodes := map[string]bool{}, map[string]bool{}
	for _, p := range c.Traps {
		if !contract.ValidStableKey(p.Key) || traps[p.Key] || nodes[p.Node] || p.Name == "" || p.Node == c.World.Start || expedition.Find(c.World, p.Node).Key == "" || len(p.Clues) < 1 || len(p.Clues) > 8 || p.Tools < 1 || p.Tools > 10 || p.Risk < 1 || p.Risk > 100 || p.Damage < 1 || p.Damage > 200 {
			return fmt.Errorf("机关定义无效 %s", p.Key)
		}
		traps[p.Key] = true
		nodes[p.Node] = true
		for _, key := range p.Clues {
			if clues[key].Key == "" {
				return fmt.Errorf("机关引用了未知线索 %s", key)
			}
		}
	}
	// Prove a clue-based route. Optional risky bypasses must not hide cycles or
	// force the author to rely on luck to make the target reachable.
	reached, known := map[string]bool{c.World.Start: true}, map[string]bool{}
	for range len(c.World.Nodes) + len(c.Clues) + 1 {
		for _, clue := range c.Clues {
			ok := reached[clue.Node]
			for _, key := range clue.Requires {
				ok = ok && known[key]
			}
			if ok {
				known[clue.Key] = true
			}
		}
		for _, n := range c.World.Nodes {
			if reached[n.Key] {
				for _, next := range n.Links {
					p := trapAt(c, next)
					ok := true
					for _, key := range p.Clues {
						ok = ok && known[key]
					}
					if ok {
						reached[next] = true
					}
				}
			}
		}
	}
	if len(known) != len(c.Clues) || !reached[expedition.FindLoot(c.World, c.World.Goal).Node] {
		return fmt.Errorf("线索存在循环、机关前提或目标无法到达")
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
	for _, key := range []string{"recovered", "retreated", "perished", "darkness", "timeout", "abandoned"} {
		found := false
		for _, o := range content.Outcomes {
			found = found || o.Key == key && o.Enabled
		}
		if !found {
			return fmt.Errorf("缺少墓室结局 %s", key)
		}
	}
	return nil
}
func Descriptor() contract.ModuleDescriptor {
	labels := map[string]string{"world": "远征地图与物资", "nodes": "墓室", "key": "标识", "name": "名称/已知线索", "links": "往返通路", "minutes": "进入耗时（分钟）", "rest": "休整次数", "loot": "遗物", "node": "所在墓室", "quantity": "数量", "weight": "单件重量", "start": "入口", "goal": "目标遗物", "health": "初始体力", "supplies": "行军补给", "capacity": "负重上限", "deadline": "期限（分钟）", "clues": "线索", "requires": "前置线索", "traps": "机关", "tools": "工具数量/绕行消耗", "risk": "绕行受伤概率（%）", "damage": "绕行伤害", "light": "照明时长（分钟）"}
	return contract.ModuleDescriptor{Key: EngineKey, Version: 1, Name: "古墓探险", Category: "tomb", Description: "循线索解开机关，管理照明与负重，把遗物带回入口。", Modes: []string{contract.RuntimeModeChallenge}, Actions: []string{"update_state", "domain_action", "complete_challenge"}, Verifiers: []string{"core.conditions"}, SettlementHandlers: []string{"core.challenge_completion"}, Runtime: Module{}, AbandonAction: "tomb.abandon", DefinitionLabels: map[string]string{DefinitionKind: "墓室远征"}, DefinitionSchemas: map[string]map[string]any{DefinitionKind: ruledata.Schema[Config](labels)}, DefinitionValidators: map[string]contract.DefinitionValidator{DefinitionKind: func(d contract.Definition) error {
		c, err := ruledata.Decode[Config](d.Content)
		if err != nil {
			return err
		}
		return validate(c)
	}}, ValidateContent: ValidateContent}
}
