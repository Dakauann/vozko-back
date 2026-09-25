package aichat_usecase

import (
	"time"

	"vozko/domain/balance"
	balance_usecase "vozko/usecases/balance"
)

type FundsGate struct {
	spend balance_usecase.SpendGuard
	subs  subscriptionReader
}

func NewFundsGate(ledger balance.CachedBalanceChecker, subs subscriptionReader) *FundsGate {
	return &FundsGate{spend: balance_usecase.NewRequiredSpendGuard(ledger, "ai chat"), subs: subs}
}

func (g *FundsGate) Check(workspaceID string) error {
	sub, err := g.subs.GetCurrentByWorkspaceID(workspaceID, time.Now().UTC())
	if err != nil || sub == nil {
		return ErrNoSubscription
	}
	if !g.spend.Allow(workspaceID) {
		return ErrInsufficientBalance
	}
	return nil
}
