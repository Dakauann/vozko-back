package shared

func EstimateTokens(text string) int {
	n := len(text) / 4
	if n == 0 && len(text) > 0 {
		n = 1
	}
	return n
}
