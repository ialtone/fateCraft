package effectrules

// HealingPower is a percentage bonus to positive healing, capped at +100% / -90%.
// It never boosts damage, mana, lifespan or the character's health ceiling.
func HealingAmount(base, power int64) int64 {
	if base <= 0 {
		return base
	}
	power = max(int64(-90), min(int64(100), power))
	return base + base*power/100
}
