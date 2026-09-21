package balance_usecase

import (
	"log"
	"strings"

	"vozko/domain/balance"
)

// SpendGuard is the one question every path asks before it spends a workspace's
// money: may this workspace pay for what is about to happen?
//
// It exists as a shared type because the check was copied into four use cases,
// each with its own floor constant and its own log line. It was then copied a
// fifth time anyway, into the WhatsApp conversational AI path, which is the
// highest-volume spender in the product and the one place the policy most
// needed to be the same as everywhere else. That copy is gone; it delegates
// here now.
//
// Two questions, one balance read each, because they are genuinely different:
//
//   - Floor: "is this workspace solvent enough to start a model call", asked
//     when the price is not knowable in advance. Model calls are billed by
//     tokens after the fact, so a floor is the only pre-check available.
//   - CanAfford: "can this workspace cover this exact amount", asked when the
//     price IS known, such as a WhatsApp message with a per-message rate.
//
// Fail-closed on purpose. A balance that cannot be READ is treated as too low,
// because the alternative is spending money we cannot prove the customer has
// every time the cache or the database blinks. That is the stance the audience
// engine has always taken and the one every copy of this check agreed on.
type SpendGuard struct {
	balance balance.CachedBalanceChecker
	// label names the caller in the log line, so an operator reading
	// "balance below floor" knows which feature went quiet.
	label string
	// requireChecker makes a missing checker a refusal instead of a pass.
	//
	// The two stances are both right, for different callers, and the difference
	// is whether a nil checker is a deployment choice or a wiring bug. Making
	// it a field rather than leaving each caller to remember is the point: this
	// exact distinction was lost once already, when the WhatsApp AI path was
	// moved onto the shared guard and its explicit fail-closed on a nil checker
	// turned into the guard's permissive default. Nothing failed. It would have
	// answered customers for free until somebody noticed the bill.
	requireChecker bool
}

// NewSpendGuard builds a guard that ALLOWS when there is no checker at all: a
// deployment without balance tracking is not a deployment where every paid
// feature is dead.
//
// Use it where billing is genuinely optional. Where a missing checker means the
// container failed to wire it, use NewRequiredSpendGuard.
func NewSpendGuard(checker balance.CachedBalanceChecker, label string) SpendGuard {
	return SpendGuard{balance: checker, label: strings.TrimSpace(label)}
}

// NewRequiredSpendGuard builds a guard that REFUSES when there is no checker.
//
// For paths where the checker is always wired and its absence is a bug, not a
// configuration: spending money because a dependency went missing is the one
// outcome that cannot be undone afterwards.
func NewRequiredSpendGuard(checker balance.CachedBalanceChecker, label string) SpendGuard {
	return SpendGuard{balance: checker, label: strings.TrimSpace(label), requireChecker: true}
}

// read returns the workspace balance and whether the guard may proceed at all.
//
// Both questions share it so that "no workspace id", "no checker" and "the read
// failed" are answered identically by both, rather than by two switch
// statements that can drift.
func (g SpendGuard) read(workspaceID string) (micros int64, checked bool, ok bool) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		// A call without a workspace is a call nobody pays for. The AI adapter
		// logs that as a revenue leak; refusing here is cheaper than finding it
		// there.
		log.Printf("[balance] %s: refusing a paid action with no workspace id", g.label)
		return 0, false, false
	}
	if g.balance == nil {
		if g.requireChecker {
			log.Printf("CRITICAL: [balance] %s: no balance checker wired, refusing to spend for workspace %s (fail-closed)",
				g.label, workspaceID)
			return 0, false, false
		}
		return 0, false, true
	}
	bal, err := g.balance.GetBalance(workspaceID)
	if err != nil {
		log.Printf("[balance] %s: balance check for workspace %s failed, refusing (fail-closed): %v",
			g.label, workspaceID, err)
		return 0, false, false
	}
	return bal, true, true
}

// Allow reports whether this workspace is above the floor for a model call.
//
// A bool rather than an error: every caller does the same thing with a refusal,
// skip the call and carry on, and the reason is already in the log. Callers
// that owe their own domain error wrap this and return theirs.
func (g SpendGuard) Allow(workspaceID string) bool {
	bal, checked, ok := g.read(workspaceID)
	if !ok {
		return false
	}
	if !checked {
		return true
	}
	if bal < balance.MinAIFloorMicros {
		log.Printf("[balance] %s: workspace %s balance (%d micros) is below the floor (%d micros), refusing",
			g.label, workspaceID, bal, balance.MinAIFloorMicros)
		return false
	}
	return true
}

// CanAfford reports whether this workspace can cover an exact amount.
//
// Deliberately NOT the floor. A workspace can sit below the AI floor and still
// afford a message that costs a fraction of a cent, and refusing that would
// take the inbox away from someone who can pay for the thing they are actually
// doing.
//
// A non-positive amount is free and always allowed, which is what makes "this
// plan does not charge for service messages" express itself as no gate at all
// rather than as a special case at every call site.
func (g SpendGuard) CanAfford(workspaceID string, amountMicros int64) bool {
	if amountMicros <= 0 {
		return true
	}
	bal, checked, ok := g.read(workspaceID)
	if !ok {
		return false
	}
	if !checked {
		return true
	}
	if bal < amountMicros {
		log.Printf("[balance] %s: workspace %s balance (%d micros) cannot cover %d micros, refusing",
			g.label, workspaceID, bal, amountMicros)
		return false
	}
	return true
}
