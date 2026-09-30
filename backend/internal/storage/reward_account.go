package storage

import (
	"github.com/ialtone/fatecraft/internal/platformcapability"
)

const PointsKey = "fatecraft.points"
const MessageRewriteCreditKey = platformcapability.MessageRewriteRewardKey
const MaxRewardQuantity int64 = 9007199254740991

type RewardAsset struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	Unit string `json:"unit"`
	Icon string `json:"icon"`
}

func ValidRewardKey(key string) bool { return key == PointsKey || key == MessageRewriteCreditKey }

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
