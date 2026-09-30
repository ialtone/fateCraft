package requirement

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/storage"
)

var ErrRequirementsNotMet = errors.New("前置条件未满足")

const (
	maxDepth      = 4
	maxConditions = 32
)

type Rule struct {
	Type       string `json:"type"`
	RewardKey  string `json:"reward_key,omitempty"`
	Operator   string `json:"operator,omitempty"`
	Quantity   int64  `json:"quantity,omitempty"`
	Conditions []Rule `json:"conditions,omitempty"`
}

type Evaluator struct {
	rewards rewardBalanceReader
}

type rewardBalanceReader interface {
	HasBalance(context.Context, uuid.UUID, string, int64) (bool, error)
}

func NewEvaluator(rewards *storage.RewardRepository) *Evaluator {
	return &Evaluator{rewards: rewards}
}

func Decode(raw any) (Rule, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return Rule{}, err
	}
	var rule Rule
	if err := json.Unmarshal(data, &rule); err != nil {
		return Rule{}, err
	}
	if err := validate(rule, 0, new(int)); err != nil {
		return Rule{}, err
	}
	return rule, nil
}

func (e *Evaluator) Check(ctx context.Context, playerID uuid.UUID, rule Rule) error {
	ok, err := e.evaluate(ctx, playerID, rule)
	if err != nil {
		return err
	}
	if !ok {
		return ErrRequirementsNotMet
	}
	return nil
}

func (e *Evaluator) evaluate(ctx context.Context, playerID uuid.UUID, rule Rule) (bool, error) {
	switch rule.Type {
	case "reward_balance":
		return e.rewards.HasBalance(ctx, playerID, rule.RewardKey, rule.Quantity)
	case "all":
		for _, child := range rule.Conditions {
			ok, err := e.evaluate(ctx, playerID, child)
			if err != nil || !ok {
				return ok, err
			}
		}
		return true, nil
	case "any":
		for _, child := range rule.Conditions {
			ok, err := e.evaluate(ctx, playerID, child)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	default:
		return false, fmt.Errorf("不支持的前置条件类型 %q", rule.Type)
	}
}

func validate(rule Rule, depth int, count *int) error {
	(*count)++
	if depth > maxDepth || *count > maxConditions {
		return fmt.Errorf("前置条件过于复杂")
	}
	switch rule.Type {
	case "reward_balance":
		if !storage.ValidRewardKey(rule.RewardKey) || rule.Quantity <= 0 || rule.Quantity > storage.MaxRewardQuantity {
			return fmt.Errorf("奖励余额条件缺少 reward_key 或 quantity")
		}
		if rule.Operator != "" && rule.Operator != "gte" {
			return fmt.Errorf("奖励余额条件只支持 gte")
		}
		if len(rule.Conditions) != 0 {
			return fmt.Errorf("奖励余额条件不能包含子条件")
		}
	case "all", "any":
		if len(rule.Conditions) == 0 {
			return fmt.Errorf("%s 条件不能为空", rule.Type)
		}
		for _, child := range rule.Conditions {
			if err := validate(child, depth+1, count); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("未知前置条件类型 %q", rule.Type)
	}
	return nil
}
