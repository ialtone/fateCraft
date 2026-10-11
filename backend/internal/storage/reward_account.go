package storage

import (
	"github.com/ialtone/fatecraft/internal/accountasset"
)

const PointsKey = accountasset.Points
const MessageRewriteCreditKey = accountasset.RewriteCredits
const MaxRewardQuantity int64 = accountasset.MaxQuantity

type RewardAsset struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	Unit string `json:"unit"`
	Icon string `json:"icon"`
}

func ValidRewardKey(key string) bool { return accountasset.ValidKey(key) }

func RewardAssetFor(key string) RewardAsset {
	switch key {
	case PointsKey:
		return RewardAsset{key, "积分", "分", "sparkles"}
	case MessageRewriteCreditKey:
		return RewardAsset{key, "剧情改写次数", "次", "refresh"}
	default:
		return RewardAsset{}
	}
}
