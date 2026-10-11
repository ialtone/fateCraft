package dungeon

import (
	"fmt"
	"github.com/ialtone/fatecraft/internal/gameplay/components/expedition"
	"github.com/ialtone/fatecraft/internal/gameplay/components/ruledata"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
)

const EngineKey = "expedition.dungeon"
const DefinitionKind = "dungeon.expedition"

type Enemy struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	Node   string `json:"node"`
	Health int64  `json:"health"`
	Attack int64  `json:"attack"`
}
type Config struct {
	World    expedition.Config `json:"world"`
	Enemies  []Enemy           `json:"enemies"`
	Boss     string            `json:"boss"`
	Power    int64             `json:"power"`
	Defense  int64             `json:"defense"`
	Medicine int64             `json:"medicine"`
}

func enemyAt(c Config, node string) Enemy {
	for _, e := range c.Enemies {
		if e.Node == node {
			return e
		}
	}
	return Enemy{}
}
func validate(c Config) error {
	if err := expedition.Validate(c.World); err != nil {
		return err
	}
	if len(c.Enemies) < 2 || len(c.Enemies) > 16 || c.Power < 4 || c.Power > 40 || c.Defense < 0 || c.Defense > 20 || c.Medicine < 0 || c.Medicine > 10 {
		return fmt.Errorf("地牢敌人、战力或药物配置无效")
	}
	keys, nodes := map[string]bool{}, map[string]bool{}
	boss := Enemy{}
	for _, e := range c.Enemies {
		if !contract.ValidStableKey(e.Key) || keys[e.Key] || nodes[e.Node] || e.Name == "" || expedition.Find(c.World, e.Node).Key == "" || e.Node == c.World.Start || e.Health < 1 || e.Health > 300 || e.Attack < 1 || e.Attack > 100 {
			return fmt.Errorf("地牢遭遇无效 %s", e.Key)
		}
		keys[e.Key] = true
		nodes[e.Node] = true
		if e.Key == c.Boss {
			boss = e
		}
	}
	if boss.Key == "" || boss.Node != expedition.FindLoot(c.World, c.World.Goal).Node {
		return fmt.Errorf("终点遭遇必须守卫主要目标")
	}
	if len(expedition.Find(c.World, c.World.Start).Links) < 2 {
		return fmt.Errorf("入口需要至少两条探索路线")
	}
	rest := int64(0)
	for _, n := range c.World.Nodes {
		rest += n.Rest
	}
	if rest == 0 {
		return fmt.Errorf("地牢需要有限的休整地点")
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
	for _, key := range []string{"cleared", "retreated", "defeated", "timeout", "abandoned"} {
		found := false
		for _, o := range content.Outcomes {
			found = found || o.Key == key && o.Enabled
		}
		if !found {
			return fmt.Errorf("缺少地牢结局 %s", key)
		}
	}
	return nil
}
func Descriptor() contract.ModuleDescriptor {
	labels := map[string]string{"world": "远征地图与物资", "nodes": "地牢区域", "key": "标识", "name": "名称", "links": "往返通路", "minutes": "进入耗时（分钟）", "rest": "休整次数", "loot": "战利品", "node": "所在节点", "quantity": "数量", "weight": "单件重量", "start": "入口", "goal": "主要目标", "health": "初始生命", "supplies": "行军/战斗补给", "capacity": "负重上限", "deadline": "期限（分钟）", "enemies": "遭遇", "boss": "终点遭遇标识", "attack": "敌人反击威力", "power": "己方基础威力", "defense": "己方防御", "medicine": "初始治疗药物"}
	return contract.ModuleDescriptor{Key: EngineKey, Version: 1, Name: "地牢远征", Category: "dungeon", Description: "在有限补给和休整机会中选择路线、击败遭遇并携目标撤离。", Modes: []string{contract.RuntimeModeChallenge}, Actions: []string{"update_state", "domain_action", "complete_challenge"}, Verifiers: []string{"core.conditions"}, SettlementHandlers: []string{"core.challenge_completion"}, Runtime: Module{}, AbandonAction: "dungeon.abandon", DefinitionLabels: map[string]string{DefinitionKind: "地牢远征"}, DefinitionSchemas: map[string]map[string]any{DefinitionKind: ruledata.Schema[Config](labels)}, DefinitionValidators: map[string]contract.DefinitionValidator{DefinitionKind: func(d contract.Definition) error {
		c, err := ruledata.Decode[Config](d.Content)
		if err != nil {
			return err
		}
		return validate(c)
	}}, ValidateContent: ValidateContent}
}
