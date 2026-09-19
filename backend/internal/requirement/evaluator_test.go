package requirement

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type fakeBalances map[string]int64

func (f fakeBalances) HasBalance(_ context.Context, _ uuid.UUID, key string, quantity int64) (bool, error) {
	return f[key] >= quantity, nil
}

func TestRewardRequirementCanGateAnotherFeature(t *testing.T) {
	rule, err := Decode(map[string]any{
		"type": "reward_balance", "reward_key": "midnight-case.case-token",
		"operator": "gte", "quantity": 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	playerID := uuid.New()
	if err := (&Evaluator{rewards: fakeBalances{}}).Check(context.Background(), playerID, rule); !errors.Is(err, ErrRequirementsNotMet) {
		t.Fatalf("empty balance error = %v, want ErrRequirementsNotMet", err)
	}
	if err := (&Evaluator{rewards: fakeBalances{"midnight-case.case-token": 1}}).
		Check(context.Background(), playerID, rule); err != nil {
		t.Fatalf("owned token did not unlock feature: %v", err)
	}
}

func TestDecodeRejectsOverlyDeepRequirementTree(t *testing.T) {
	rule := map[string]any{"type": "reward_balance", "reward_key": "test.token", "quantity": 1}
	for range 6 {
		rule = map[string]any{"type": "all", "conditions": []any{rule}}
	}
	if _, err := Decode(rule); err == nil {
		t.Fatal("overly deep requirement tree was accepted")
	}
}
