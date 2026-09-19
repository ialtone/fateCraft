package roll

import (
	"os"

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
