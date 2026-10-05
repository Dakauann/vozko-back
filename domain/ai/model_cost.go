package ai

import "math"

func (m ModelInfo) CostMicros(u Usage) int64 {
	return int64(math.Ceil(float64(u.PromptTokens)*m.PromptPrice + float64(u.CompletionTokens)*m.CompletionPrice))
}

func (m ModelInfo) HasKnownLimits() bool {
	return m.ContextLength > 0 && (m.PromptPrice > 0 || m.CompletionPrice > 0)
}
