package balance_usecase

import (
	"log"
	"strings"

	"vozko/domain/balance"
)

type SpendGuard struct {
	balance        balance.CachedBalanceChecker
	label          string
	requireChecker bool
}

func NewSpendGuard(checker balance.CachedBalanceChecker, label string) SpendGuard {
	return SpendGuard{balance: checker, label: strings.TrimSpace(label)}
}

func NewRequiredSpendGuard(checker balance.CachedBalanceChecker, label string) SpendGuard {
	return SpendGuard{balance: checker, label: strings.TrimSpace(label), requireChecker: true}
}

func (g SpendGuard) read(workspaceID string) (micros int64, checked bool, ok bool) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
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
