package balance_usecase

import (
	"log"
	"strings"

	"vozko/domain/balance"
)

// AIFloorGuard is the one question every AI path asks before it spends:
// may this workspace pay for a model call right now?
//
// It exists as a shared type because the check was copied into four use cases,
// each with its own floor constant and its own log line, and a fifth copy was
// about to be written for seeded conversations. The decision is the same
// everywhere, so it is made in one place.
//
// Fail-closed on purpose. A balance that cannot be READ is treated as too low,
// because the alternative is spending money we cannot prove the customer has
// every time the cache or the database blinks. That is the stance the audience
// engine has always taken and the one every copy of this check agreed on.
type AIFloorGuard struct {
	balance balance.CachedBalanceChecker
	// label names the caller in the log line, so an operator reading
	// "balance below floor" knows which feature went quiet.
	label string
}

// NewAIFloorGuard builds the guard. A nil checker yields a guard that allows:
// a deployment without balance tracking is not a deployment where every AI
// feature is dead.
func NewAIFloorGuard(checker balance.CachedBalanceChecker, label string) AIFloorGuard {
	return AIFloorGuard{balance: checker, label: strings.TrimSpace(label)}
}

// Allow reports whether this workspace may spend on a model call.
//
// A bool rather than an error: every caller does the same thing with a refusal
// — skip the call and carry on — and the reason is already in the log. Callers
// that owe their own domain error wrap this and return theirs.
func (g AIFloorGuard) Allow(workspaceID string) bool {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		// A call without a workspace is a call nobody pays for. The AI adapter
		// logs that as a revenue leak; refusing here is cheaper than finding it
		// there.
		log.Printf("[balance] %s: refusing an AI call with no workspace id", g.label)
		return false
	}
	if g.balance == nil {
		return true
	}
	bal, err := g.balance.GetBalance(workspaceID)
	if err != nil {
		log.Printf("[balance] %s: balance check for workspace %s failed, refusing (fail-closed): %v",
			g.label, workspaceID, err)
		return false
	}
	if bal < balance.MinAIFloorMicros {
		log.Printf("[balance] %s: workspace %s balance (%d micros) is below the floor (%d micros), refusing",
			g.label, workspaceID, bal, balance.MinAIFloorMicros)
		return false
	}
	return true
}
