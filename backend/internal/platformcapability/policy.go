// Package platformcapability contains capabilities owned by fateCraft itself.
//
// These capabilities are part of the product contract, not of an individual
// challenge runtime.  Keeping their policy here prevents a content package
// from accidentally becoming the authorization source for a platform feature.
package platformcapability

import "github.com/ialtone/fatecraft/internal/accountasset"

const (
	// MessageRewriteKey is the stable public key used by both regenerate and
	// edit-and-regenerate.  It is intentionally not declared in a challenge
	// runtime JSON document.
	MessageRewriteKey = "message_rewrite"
	// MessageRewriteRewardKey is the stable platform reward consumed by both
	// regenerate and edit-and-regenerate.
	MessageRewriteRewardKey = accountasset.RewriteCredits
	// MessageRewriteCostQuantity is kept beside the policy, so storage and API
	// layers cannot silently disagree about the charge.
	MessageRewriteCostQuantity int64 = 1
)

// Capability is the public, player-facing description of a platform feature.
// Cost metadata is returned so clients can render the feature without
// duplicating product policy; the server remains authoritative for every
// authorization and balance check.
type Capability struct {
	Enabled       bool   `json:"enabled"`
	Label         string `json:"label"`
	Description   string `json:"description,omitempty"`
	CostRewardKey string `json:"cost_reward_key,omitempty"`
	CostQuantity  int64  `json:"cost_quantity,omitempty"`
}

// MessageRewritePolicy is the single platform policy shared by all gameplay
// modes and challenge packs.
func MessageRewritePolicy() Capability {
	return Capability{
		Enabled:       true,
		Label:         "剧情改写",
		Description:   "重新生成当前回复或改写一条玩家消息，并从该处继续推演；没有提交任何动作就失败或取消时会返还额度。",
		CostRewardKey: MessageRewriteRewardKey,
		CostQuantity:  MessageRewriteCostQuantity,
	}
}

// PublicCapabilities returns a fresh map for an API response.  Callers cannot
// mutate a package-level map and affect another request.
func PublicCapabilities() map[string]Capability {
	return map[string]Capability{MessageRewriteKey: MessageRewritePolicy()}
}

// IsReservedKey lets challenge validation reject platform keys without
// embedding a growing list of product capabilities in the gameplay package.
func IsReservedKey(key string) bool {
	switch key {
	case MessageRewriteKey:
		return true
	default:
		return false
	}
}
