package roll

import (
	"testing"

	"github.com/ialtone/fatecraft/internal/protocol"
	"github.com/ialtone/fatecraft/internal/storage"
)

// testConfig 构造与 data/roll_config.yaml 关键数值一致的测试配置。
func testConfig() *Config {
	return &Config{
		ContextModifiers: ContextModifiers{
			Opponent: map[string]int{
				"境界差距_每级": -10,
				"对手受伤":    15,
				"对手疲惫":    10,
				"属性克制":    20,
				"属性被克":    -20,
			},
		},
		HistoryAdjust: HistoryAdjustment{
			ConsecutiveFails:   map[int]int{2: 5, 3: 10, 4: 15},
			SameActionRetry:    RetryBonus{Bonus: 5},
			LuckyStreakPenalty: StreakRule{Threshold: 5, Penalty: -5},
		},
	}
}

func rolls(results ...bool) []storage.RollHistoryEntry {
	// 输入按"最新在前"排列，与 HistoryStore.Recent 的返回约定一致。
	rows := make([]storage.RollHistoryEntry, 0, len(results))
	for _, ok := range results {
		rows = append(rows, storage.RollHistoryEntry{RollType: "探索判定", Success: ok})
	}
	return rows
}

func TestHistoryAdjustment(t *testing.T) {
	cfg := testConfig()

	cases := []struct {
		name        string
		recent      []storage.RollHistoryEntry
		rollType    string
		rewardValue int
		want        int
	}{
		{"无历史", nil, "探索判定", 0, 0},
		{"连败2次+同类重试", rolls(false, false), "探索判定", 0, 5 + 5},
		{"连败3次+同类重试", rolls(false, false, false), "探索判定", 0, 10 + 5},
		{"连败2次但换了判定类型", rolls(false, false), "战斗判定", 0, 5},
		{"上次成功无调整", rolls(true, false, false), "探索判定", 0, 0},
		{"连胜5次+高收益惩罚", rolls(true, true, true, true, true), "探索判定", 100000, -5},
		{"连胜5次但低收益不罚", rolls(true, true, true, true, true), "探索判定", 999, 0},
		{"连胜4次不足阈值", rolls(true, true, true, true), "探索判定", 100000, 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := historyAdjustment(c.recent, c.rollType, c.rewardValue, cfg)
			if got != c.want {
				t.Fatalf("historyAdjustment = %d, want %d", got, c.want)
			}
		})
	}
}

func TestCalculateOpponentBonus(t *testing.T) {
	cfg := testConfig()

	cases := []struct {
		name     string
		opponent *protocol.RollOpponent
		want     int
	}{
		{"对手高2个境界", &protocol.RollOpponent{RealmGap: 2}, -20},
		{"己方高1个境界", &protocol.RollOpponent{RealmGap: -1}, 10},
		{"对手受伤", &protocol.RollOpponent{Statuses: []string{"对手受伤"}}, 15},
		{"受伤+疲惫", &protocol.RollOpponent{Statuses: []string{"对手受伤", "对手疲惫"}}, 25},
		{"属性克制", &protocol.RollOpponent{Element: "属性克制"}, 20},
		{"疲惫+高1境界组合", &protocol.RollOpponent{RealmGap: 1, Statuses: []string{"对手疲惫"}}, 0},
		{"词表外词条忽略", &protocol.RollOpponent{Statuses: []string{"未受伤"}, Element: "五行相生"}, 0},
		{"空对手", &protocol.RollOpponent{}, 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := calculateOpponentBonus(c.opponent, cfg)
			if got != c.want {
				t.Fatalf("calculateOpponentBonus = %d, want %d", got, c.want)
			}
		})
	}
}

func TestCalculateContextBonusExactMatch(t *testing.T) {
	cfg := testConfig()
	cfg.ContextModifiers.Advantages = map[string]int{"得到指点": 20, "充分准备": 10}
	cfg.ContextModifiers.Disadvantages = map[string]int{"受伤状态": -20}
	cfg.ContextModifiers.Environment = map[string]int{"夜晚": -5}

	cases := []struct {
		name string
		ctx  *protocol.RollContext
		want int
	}{
		{"精确命中求和", &protocol.RollContext{
			Advantages:    []string{"得到指点", "充分准备"},
			Disadvantages: []string{"受伤状态"},
			Environment:   []string{"夜晚"},
		}, 20 + 10 - 20 - 5},
		// 旧实现的三类问题在精确查表下不再发生：
		{"同义改写不命中(漏报变为显式)", &protocol.RollContext{Advantages: []string{"获得高人指点"}}, 0},
		{"否定句不误报", &protocol.RollContext{Disadvantages: []string{"并未受伤状态"}}, 0},
		{"子串不歧义", &protocol.RollContext{Advantages: []string{"指点"}}, 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := &protocol.RollRequest{Context: c.ctx}
			got := calculateContextBonus(req, cfg)
			if got != c.want {
				t.Fatalf("calculateContextBonus = %d, want %d", got, c.want)
			}
		})
	}
}

func TestTraitConditionsUseAllContextTokens(t *testing.T) {
	// 条件词条现在来自全部情境枚举：夜行者的"夜晚"条件应由 environment 提供。
	cfg := testConfig()
	cfg.TraitPatterns = TraitPatterns{
		Traits: []TraitPattern{
			{Keywords: []string{"夜行者"}, AppliesTo: []string{"exploration"}, Conditions: []string{"夜晚"}, Bonus: 20},
		},
	}
	state := map[string]any{"traits": []any{"夜行者"}}

	req := &protocol.RollRequest{
		Category: "exploration",
		Context:  &protocol.RollContext{Environment: []string{"夜晚"}},
	}
	if got := MatchTraits(state, req.Category, collectContextTokens(req), cfg); got != 20 {
		t.Fatalf("夜晚条件经 environment 传入应命中, got %d", got)
	}

	// 条件不满足时不加成
	reqNoNight := &protocol.RollRequest{Category: "exploration", Context: &protocol.RollContext{}}
	if got := MatchTraits(state, reqNoNight.Category, collectContextTokens(reqNoNight), cfg); got != 0 {
		t.Fatalf("无夜晚词条不应命中, got %d", got)
	}
}

func TestVocabSplitsOpponentKeys(t *testing.T) {
	cfg := testConfig()
	v := cfg.Vocab()
	if len(v.OpponentStatuses) != 2 || v.OpponentStatuses[0] != "对手受伤" || v.OpponentStatuses[1] != "对手疲惫" {
		t.Fatalf("OpponentStatuses 划分错误: %v", v.OpponentStatuses)
	}
	if len(v.OpponentElements) != 2 || v.OpponentElements[0] != "属性克制" || v.OpponentElements[1] != "属性被克" {
		t.Fatalf("OpponentElements 划分错误: %v", v.OpponentElements)
	}
}

func TestApplyRewardCapTakesTightestCap(t *testing.T) {
	// 与 data/roll_config.yaml 相同的 5 档封顶（threshold 升序）。
	cfg := &Config{RewardBalance: RewardBalance{Caps: []RewardCap{
		{Threshold: 1000, MaxRate: 90},
		{Threshold: 10000, MaxRate: 75},
		{Threshold: 100000, MaxRate: 50},
		{Threshold: 1000000, MaxRate: 25},
		{Threshold: 10000000, MaxRate: 10},
	}}}

	cases := []struct {
		name        string
		currentRate int
		rewardValue int
		want        int
	}{
		{"低收益不封顶", 80, 500, 80},
		{"小额收益封90", 95, 1500, 90},
		{"重大收益必须封到50而非首个命中的75", 80, 150000, 50},
		{"改命收益封10", 80, 10000000, 10},
		{"已低于上限不动", 20, 10000000, 10}, // 20>10 仍封到 10
		{"低于全部上限保持原值", 5, 10000000, 5},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := applyRewardCap(c.currentRate, c.rewardValue, cfg); got != c.want {
				t.Fatalf("applyRewardCap(%d, %d) = %d, want %d", c.currentRate, c.rewardValue, got, c.want)
			}
		})
	}
}

func TestHistoryAdjustmentConsecutiveFailsCapAtMaxTier(t *testing.T) {
	// 连败超过配置最大档（4）时应封顶到最大档补偿（15），而不是归零。
	cfg := testConfig()
	got := historyAdjustment(rolls(false, false, false, false, false), "战斗判定", 0, cfg)
	// 连败5次：档位补偿封顶 15；上次同类失败（rolls 全部同类型"探索判定"）——
	// 注意 rolls() 的类型是"探索判定"，此处用"战斗判定"避免叠加重试加成。
	if got != 15 {
		t.Fatalf("连败5次补偿 = %d, want 15（封顶到最大档）", got)
	}
}
