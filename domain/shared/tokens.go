package shared

// EstimateTokens approximates the LLM token count of a text as len/4, never
// returning 0 for a non-empty input.
//
// len() counts BYTES, so accented pt-BR text over-counts slightly. That is
// deliberate and must stay: the estimate is the safe direction for chunking,
// and it is baked into every stored RAG chunk's token_count. A consumer that
// wants a different bias (the comment-analysis budgeter needs a MORE
// conservative number) applies its own factor on top rather than changing
// this function: one estimator, several policies, no fork.
func EstimateTokens(text string) int {
	n := len(text) / 4
	if n == 0 && len(text) > 0 {
		n = 1
	}
	return n
}
