package platformcapability

import "testing"

func TestMessageRewritePolicyIsPlatformOwned(t *testing.T) {
	policy := MessageRewritePolicy()
	if !policy.Enabled || policy.CostRewardKey != MessageRewriteRewardKey || policy.CostQuantity != MessageRewriteCostQuantity {
		t.Fatalf("unexpected message rewrite policy: %+v", policy)
	}
	if policy.Label == "" || policy.Description == "" {
		t.Fatal("platform policy must provide player-facing copy")
	}
}

func TestPublicCapabilitiesReturnsIndependentMap(t *testing.T) {
	left := PublicCapabilities()
	left[MessageRewriteKey] = Capability{}
	right := PublicCapabilities()
	if !right[MessageRewriteKey].Enabled {
		t.Fatal("public capability map must not share mutable state")
	}
}
