package aichat

import (
	"vozko/domain/aiusage"
	"vozko/domain/balance"
)

func ChargeReference(threadID string) string {
	return "aichat:" + threadID
}

type ThreadUsage struct {
	Available bool
	Calls     int
	Tokens    aiusage.Tokens
}

type ThreadCost struct {
	Available    bool
	AmountMicros int64
	Currency     string
	Usage        ThreadUsage
}

func usageOf(charges balance.ReferenceTotals, usage *aiusage.Totals) ThreadUsage {
	if usage == nil || !usage.Covers(charges.Debits) {
		return ThreadUsage{}
	}
	return ThreadUsage{Available: true, Calls: usage.Calls, Tokens: usage.Tokens}
}

func CostOf(thread *Thread, charges balance.ReferenceTotals, usage *aiusage.Totals) ThreadCost {
	cost := ThreadCost{Currency: balance.BillingCurrency}
	if thread == nil || !thread.CostTracked {
		return cost
	}
	cost.Usage = usageOf(charges, usage)
	if !charges.MissingRate {
		cost.Available = true
		cost.AmountMicros = charges.BillingMicros
	}
	return cost
}
