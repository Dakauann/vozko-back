package agentloop

import "vozko/domain/ai"

func UntilDone(c Config, model ai.ModelInfo, uncatalogedTokenBudget int) Config {
	c.IterateUntilDone = true
	c.MaxIterations = 0
	c.MaxHistoryMsgs = 0
	if model.HasKnownLimits() {
		c.ModelLimits = model
		c.SessionTokenBudget = 0
		return c
	}
	c.ModelLimits = ai.ModelInfo{}
	c.SessionTokenBudget = uncatalogedTokenBudget
	return c
}
