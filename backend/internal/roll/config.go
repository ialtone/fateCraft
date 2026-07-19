package roll

import (
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config 判定系统配置。
type Config struct {
	Categories       map[string]CategoryRule `yaml:"categories"`
	ContextModifiers ContextModifiers        `yaml:"context_modifiers"`
	RewardBalance    RewardBalance           `yaml:"reward_balance"`
	TraitPatterns    TraitPatterns           `yaml:"trait_patterns"`
	HistoryAdjust    HistoryAdjustment       `yaml:"history_adjustment"`
	OutcomeLevels    OutcomeLevels           `yaml:"outcome_levels"`
	DifficultyModes  map[string]int          `yaml:"difficulty_modes"`
}

type CategoryRule struct {
	Name              string             `yaml:"name"`
	DefaultAttributes []string           `yaml:"default_attributes"`
	DefaultWeights    map[string]float64 `yaml:"default_weights"`
	BaseSuccessRate   int                `yaml:"base_success_rate"`
}

type ContextModifiers struct {
	Advantages    map[string]int `yaml:"advantages"`
	Disadvantages map[string]int `yaml:"disadvantages"`
	Environment   map[string]int `yaml:"environment"`
	Opponent      map[string]int `yaml:"opponent"`
}

type RewardBalance struct {
	Caps []RewardCap `yaml:"caps"`
}

type RewardCap struct {
	Threshold   int    `yaml:"threshold"`
	MaxRate     int    `yaml:"max_rate"`
	Description string `yaml:"description"`
}

type TraitPatterns struct {
	Talents []TraitPattern `yaml:"talents"`
	Traits  []TraitPattern `yaml:"traits"`
}

type TraitPattern struct {
	Keywords   []string `yaml:"keywords"`
	AppliesTo  []string `yaml:"applies_to"`
	Conditions []string `yaml:"conditions,omitempty"`
	Bonus      int      `yaml:"bonus,omitempty"`
	Penalty    int      `yaml:"penalty,omitempty"`
}

type HistoryAdjustment struct {
	ConsecutiveFails   map[int]int `yaml:"consecutive_fails"`
	SameActionRetry    RetryBonus  `yaml:"same_action_retry"`
	LuckyStreakPenalty StreakRule  `yaml:"lucky_streak_penalty"`
}

type RetryBonus struct {
	Bonus int `yaml:"bonus"`
}

type StreakRule struct {
	Threshold int `yaml:"threshold"`
	Penalty   int `yaml:"penalty"`
}

type OutcomeLevels struct {
	CriticalSuccess OutcomeLevel `yaml:"critical_success"`
	CriticalFailure OutcomeLevel `yaml:"critical_failure"`
}

type OutcomeLevel struct {
	Probability  float64 `yaml:"probability"`
	MinThreshold int     `yaml:"min_threshold"`
}

// LoadConfig 从 YAML 文件加载判定配置。
// 每次调用都重新读取文件（无全局单例），配置由组合根持有并注入 Service。
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Vocab 是情境修正的枚举词表，来源即 context_modifiers 的键。
// 启动时注入 narrative_response.json 的 enum，约束模型只能输出表内词条，
// 使后端的精确查表计分与模型输出保持闭环（词表单一来源：roll_config.yaml）。
type Vocab struct {
	Advantages       []string
	Disadvantages    []string
	Environment      []string
	OpponentStatuses []string // 对手状态（对手受伤/对手疲惫…）
	OpponentElements []string // 克制关系（属性克制/属性被克），按"属性"前缀从 opponent 键中划分
}

// Vocab 返回按字典序排序的词表（排序保证注入 schema 的结果确定，便于缓存与比对）。
func (c *Config) Vocab() Vocab {
	v := Vocab{
		Advantages:    sortedKeys(c.ContextModifiers.Advantages),
		Disadvantages: sortedKeys(c.ContextModifiers.Disadvantages),
		Environment:   sortedKeys(c.ContextModifiers.Environment),
	}
	for _, key := range sortedKeys(c.ContextModifiers.Opponent) {
		switch {
		case key == "境界差距_每级":
			// 乘算系数，不是词条
		case strings.HasPrefix(key, "属性"):
			v.OpponentElements = append(v.OpponentElements, key)
		default:
			v.OpponentStatuses = append(v.OpponentStatuses, key)
		}
	}
	return v
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
