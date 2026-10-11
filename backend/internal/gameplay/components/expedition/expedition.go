// Package expedition owns bounded maps, supplies, injury and carried loot.
// Clue dependencies and combat belong to the modules using this component.
package expedition

import (
	"fmt"
	"slices"

	"github.com/ialtone/fatecraft/internal/gameplay/contract"
)

type Node struct {
	Key     string   `json:"key"`
	Name    string   `json:"name"`
	Links   []string `json:"links"`
	Minutes int64    `json:"minutes"`
	Rest    int64    `json:"rest"`
}
type Loot struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Node     string `json:"node"`
	Quantity int64  `json:"quantity"`
	Weight   int64  `json:"weight"`
}
type Config struct {
	Nodes    []Node `json:"nodes"`
	Loot     []Loot `json:"loot"`
	Start    string `json:"start"`
	Goal     string `json:"goal"`
	Health   int64  `json:"health"`
	Supplies int64  `json:"supplies"`
	Capacity int64  `json:"capacity"`
	Deadline int64  `json:"deadline"`
}
type State struct {
	Position string           `json:"position"`
	Previous string           `json:"previous"`
	Health   int64            `json:"health"`
	Supplies int64            `json:"supplies"`
	Visited  []string         `json:"visited"`
	Known    []string         `json:"known"`
	Taken    []string         `json:"taken"`
	Rested   map[string]int64 `json:"rested"`
	Carried  map[string]int64 `json:"carried"`
	Secured  map[string]int64 `json:"secured"`
	Lost     map[string]int64 `json:"lost"`
}

func Find(c Config, key string) Node {
	for _, n := range c.Nodes {
		if n.Key == key {
			return n
		}
	}
	return Node{}
}
func FindLoot(c Config, key string) Loot {
	for _, l := range c.Loot {
		if l.Key == key {
			return l
		}
	}
	return Loot{}
}
func Validate(c Config) error {
	if len(c.Nodes) < 4 || len(c.Nodes) > 24 || len(c.Loot) < 1 || len(c.Loot) > 32 || c.Health < 20 || c.Health > 200 || c.Supplies < 1 || c.Supplies > 100 || c.Capacity < 1 || c.Capacity > 100 || c.Deadline < 60 || c.Deadline > 1440 {
		return fmt.Errorf("远征地图、补给、负重或期限无效")
	}
	seen := map[string]bool{}
	for _, n := range c.Nodes {
		if !contract.ValidStableKey(n.Key) || seen[n.Key] || n.Name == "" || len(n.Links) < 1 || len(n.Links) > 6 || n.Minutes < 1 || n.Minutes > 60 || n.Rest < 0 || n.Rest > 3 {
			return fmt.Errorf("远征节点无效 %s", n.Key)
		}
		seen[n.Key] = true
	}
	if !seen[c.Start] {
		return fmt.Errorf("入口不存在")
	}
	for _, n := range c.Nodes {
		links := map[string]bool{}
		for _, key := range n.Links {
			if !seen[key] || key == n.Key || links[key] || !slices.Contains(Find(c, key).Links, n.Key) {
				return fmt.Errorf("节点 %s 的连接必须存在、唯一且可返回", n.Key)
			}
			links[key] = true
		}
	}
	reached := map[string]bool{c.Start: true}
	for range len(c.Nodes) {
		for _, n := range c.Nodes {
			if reached[n.Key] {
				for _, key := range n.Links {
					reached[key] = true
				}
			}
		}
	}
	if len(reached) != len(c.Nodes) {
		return fmt.Errorf("存在无法到达的远征节点")
	}
	loot := map[string]bool{}
	for _, l := range c.Loot {
		if !contract.ValidStableKey(l.Key) || loot[l.Key] || l.Name == "" || !seen[l.Node] || l.Quantity < 1 || l.Quantity > 20 || l.Weight < 1 || l.Weight > 100 || l.Quantity*l.Weight > c.Capacity {
			return fmt.Errorf("战利品数量、重量或节点无效 %s", l.Key)
		}
		loot[l.Key] = true
	}
	if !loot[c.Goal] || FindLoot(c, c.Goal).Node == c.Start {
		return fmt.Errorf("远征目标必须来自入口之外的节点")
	}
	return nil
}
func Initialize(c Config) State {
	n := Find(c, c.Start)
	return State{Position: c.Start, Previous: c.Start, Health: c.Health, Supplies: c.Supplies, Visited: []string{c.Start}, Known: append([]string{c.Start}, n.Links...), Taken: []string{}, Rested: map[string]int64{}, Carried: map[string]int64{}, Secured: map[string]int64{}, Lost: map[string]int64{}}
}
func Weight(c Config, s State) int64 {
	total := int64(0)
	for key, n := range s.Carried {
		total += FindLoot(c, key).Weight * n
	}
	return total
}
func ValidateState(c Config, s State) error {
	if Find(c, s.Position).Key == "" || Find(c, s.Previous).Key == "" || s.Health < 0 || s.Health > c.Health || s.Supplies < 0 || s.Supplies > c.Supplies || Weight(c, s) > c.Capacity {
		return fmt.Errorf("远征位置、伤势或补给状态无效")
	}
	for _, keys := range [][]string{s.Known, s.Visited} {
		seen := map[string]bool{}
		for _, key := range keys {
			if Find(c, key).Key == "" || seen[key] {
				return fmt.Errorf("重复或未知地点记录")
			}
			seen[key] = true
		}
	}
	if !slices.Contains(s.Visited, s.Position) || !slices.Contains(s.Visited, c.Start) {
		return fmt.Errorf("当前位置没有探索来源")
	}
	for _, key := range s.Visited {
		if !slices.Contains(s.Known, key) {
			return fmt.Errorf("探索地点不在已知范围")
		}
	}
	for key, n := range s.Rested {
		if Find(c, key).Key == "" || n < 0 || n > Find(c, key).Rest || !slices.Contains(s.Visited, key) {
			return fmt.Errorf("休整记录无效")
		}
	}
	taken := map[string]bool{}
	for _, key := range s.Taken {
		l := FindLoot(c, key)
		if l.Key == "" || taken[key] || !slices.Contains(s.Visited, l.Node) {
			return fmt.Errorf("战利品领取来源无效")
		}
		taken[key] = true
	}
	for _, stock := range []map[string]int64{s.Carried, s.Secured, s.Lost} {
		for key, n := range stock {
			if !taken[key] || n < 1 {
				return fmt.Errorf("无来源的远征战利品")
			}
		}
	}
	for key := range taken {
		if s.Carried[key]+s.Secured[key]+s.Lost[key] != FindLoot(c, key).Quantity {
			return fmt.Errorf("战利品数量不守恒")
		}
	}
	return nil
}
func CanMove(c Config, s State, key string) bool {
	return s.Supplies > 0 && slices.Contains(Find(c, s.Position).Links, key)
}
func Move(c Config, s *State, key string) error {
	if !CanMove(c, *s, key) {
		return contract.Reject("expedition.route", "路线不可达或行军补给不足")
	}
	s.Supplies--
	s.Previous = s.Position
	s.Position = key
	if !slices.Contains(s.Visited, key) {
		s.Visited = append(s.Visited, key)
	}
	for _, k := range append([]string{key}, Find(c, key).Links...) {
		if !slices.Contains(s.Known, k) {
			s.Known = append(s.Known, k)
		}
	}
	return nil
}
func CanTake(c Config, s State, key string) bool {
	l := FindLoot(c, key)
	return l.Key != "" && l.Node == s.Position && !slices.Contains(s.Taken, key) && Weight(c, s)+l.Weight*l.Quantity <= c.Capacity
}
func Take(c Config, s *State, key string) error {
	if !CanTake(c, *s, key) {
		return contract.Reject("expedition.loot", "战利品不在此处、已领取或超出负重")
	}
	l := FindLoot(c, key)
	s.Taken = append(s.Taken, key)
	s.Carried[key] = l.Quantity
	return nil
}
func Drop(s *State, key string) error {
	if s.Carried[key] < 1 {
		return contract.Reject("expedition.drop", "没有携带该物品")
	}
	s.Lost[key] += s.Carried[key]
	delete(s.Carried, key)
	return nil
}
func Damage(s *State, n int64) { s.Health = max(0, s.Health-n) }
func CanRest(c Config, s State) bool {
	return s.Supplies > 0 && s.Health < c.Health && s.Rested[s.Position] < Find(c, s.Position).Rest
}
func Rest(c Config, s *State) error {
	if !CanRest(c, *s) {
		return contract.Reject("expedition.rest", "此处无法休整，或补给/休整次数不足")
	}
	s.Supplies--
	s.Rested[s.Position]++
	s.Health = min(c.Health, s.Health+20)
	return nil
}
func Lose(s *State, all bool) {
	for key, n := range s.Carried {
		lost := (n + 1) / 2
		if all {
			lost = n
		}
		s.Lost[key] += lost
		s.Carried[key] -= lost
		if s.Carried[key] == 0 {
			delete(s.Carried, key)
		}
	}
}
func Extract(s *State) {
	for key, n := range s.Carried {
		s.Secured[key] += n
		delete(s.Carried, key)
	}
}
func Items(c Config, stock map[string]int64) []any {
	result := []any{}
	for _, l := range c.Loot {
		if stock[l.Key] > 0 {
			result = append(result, map[string]any{"key": l.Key, "name": l.Name, "quantity": stock[l.Key], "weight": l.Weight, "description": fmt.Sprintf("单件负重 %d", l.Weight)})
		}
	}
	return result
}
func Project(c Config, s State) map[string]any {
	nodes := []any{}
	for _, n := range c.Nodes {
		if slices.Contains(s.Known, n.Key) {
			description := "尚未进入"
			if slices.Contains(s.Visited, n.Key) {
				description = "已探索"
			}
			if n.Key == s.Position {
				description = "当前位置"
			} else if slices.Contains(Find(c, s.Position).Links, n.Key) {
				description += "，相邻区域"
			}
			nodes = append(nodes, map[string]any{"key": n.Key, "name": n.Name, "visited": slices.Contains(s.Visited, n.Key), "adjacent": slices.Contains(Find(c, s.Position).Links, n.Key), "description": description})
		}
	}
	return map[string]any{"location": Find(c, s.Position).Name, "health": s.Health, "supplies": s.Supplies, "weight": Weight(c, s), "capacity": c.Capacity, "nodes": nodes, "carried": Items(c, s.Carried), "secured": Items(c, s.Secured), "lost": Items(c, s.Lost), "goal": FindLoot(c, c.Goal).Name + "：携带目标并返回入口确认撤离"}
}
