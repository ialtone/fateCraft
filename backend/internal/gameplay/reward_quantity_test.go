package gameplay

import (
	"math"
	"testing"

	"github.com/ialtone/fatecraft/internal/storage"
)

func TestRewardCalculatorsRejectUnsafeQuantities(t *testing.T) {
	if err := validateFixedConfig(storage.JSONMap{"amount": float64(storage.MaxRewardQuantity) + 1}); err == nil {
		t.Fatal("unsafe fixed amount accepted")
	}
	if err := validateStateMultiplierConfig(storage.JSONMap{"state_path": "points", "multiplier_bps": 1, "max_amount": float64(storage.MaxRewardQuantity) + 1}); err == nil {
		t.Fatal("unsafe cap accepted")
	}
	for _, base := range []float64{math.Inf(1), math.NaN(), float64(storage.MaxRewardQuantity) + 1} {
		if _, err := calculateStateMultiplier(Evidence{State: storage.JSONMap{"points": base}}, storage.JSONMap{"state_path": "points", "multiplier_bps": 10000}); err == nil {
			t.Fatalf("unsafe base accepted: %v", base)
		}
	}
}
