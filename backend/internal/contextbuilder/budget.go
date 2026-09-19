package contextbuilder

// 预算计算常量与辅助函数。

// FormatReserve covers message framing and provider protocol overhead.
const FormatReserve = 256

// WorldBudgetRatio 是世界书可用的 prompt 预算占比（doc 09：20%~25%）。
const WorldBudgetRatio = 0.22

// CampaignProjectionBudgetRatio reserves at most half of the prompt for the
// authoritative Campaign read model. Unlike world information this data is
// required, but it still needs a ceiling so recent turns can remain visible.
const CampaignProjectionBudgetRatio = 0.50

// CampaignProjectionBudgetMax prevents a very large model window from turning
// an accidentally unbounded gameplay projection into an equally large prompt.
const CampaignProjectionBudgetMax = 32 * 1024

// PromptBudget 返回可用于 prompt 的 token 预算。
func PromptBudget(maxContextTokens, reservedResponseTokens int) int {
	return maxContextTokens - reservedResponseTokens - FormatReserve
}

// WorldBudget 返回世界书可用的 token 预算。
func WorldBudget(promptBudget int) int {
	if promptBudget <= 0 {
		return 0
	}
	return int(float64(promptBudget) * WorldBudgetRatio)
}

// CampaignProjectionBudget returns the preferred token ceiling for a Campaign
// projection. Build also clamps it to the space left by other required data.
func CampaignProjectionBudget(promptBudget int) int {
	if promptBudget <= 0 {
		return 0
	}
	budget := int(float64(promptBudget) * CampaignProjectionBudgetRatio)
	if budget > CampaignProjectionBudgetMax {
		return CampaignProjectionBudgetMax
	}
	return budget
}
