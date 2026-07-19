package contextbuilder

// 预算计算常量与辅助函数。

// FormatReserve 是为 JSON 信封等格式开销预留的固定 token 数。
const FormatReserve = 256

// WorldBudgetRatio 是世界书可用的 prompt 预算占比（doc 09：20%~25%）。
const WorldBudgetRatio = 0.22

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
