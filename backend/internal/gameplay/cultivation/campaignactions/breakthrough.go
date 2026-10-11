package campaignactions

import (
	"fmt"
	"math"

	"github.com/ialtone/fatecraft/internal/storage"
)

// Check against the current realm's lifespan before rolling or granting the
// next realm's lifespan. AgeDays has whole days; GameTime retains the remainder.
func breakthroughTimeReady(c *storage.CampaignCharacter, gameTime, minutes int64) error {
	if c == nil || c.Health <= 0 {
		return fmt.Errorf("角色当前不能突破")
	}
	if minutes < 1 || gameTime < 0 || gameTime > math.MaxInt64-minutes {
		return fmt.Errorf("突破时间跨度无效")
	}
	days := (gameTime+minutes)/1440 - gameTime/1440
	if c.AgeDays < 0 || c.LifespanDays <= c.AgeDays || days >= c.LifespanDays-c.AgeDays {
		return fmt.Errorf("剩余寿元不足以完成此次突破")
	}
	return nil
}
