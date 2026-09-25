package aichat_usecase

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/balance"
	"vozko/domain/workspace/workspace_plan"
)

type fakeLedger struct {
	balance     int64
	err         error
	invalidated int
	reads       int
}

func (f *fakeLedger) HasSufficientBalance(_ string, amount int64) (bool, error) {
	return f.balance >= amount, f.err
}
func (f *fakeLedger) GetBalance(string) (int64, error) {
	f.reads++
	return f.balance, f.err
}
func (f *fakeLedger) Invalidate(string)          { f.invalidated++ }
func (f *fakeLedger) InvalidateDebounced(string) { f.invalidated++ }

type fakeSubscriptions struct {
	sub *workspace_plan.WorkspaceSubscription
	err error
}

func (f fakeSubscriptions) GetCurrentByWorkspaceID(string, time.Time) (*workspace_plan.WorkspaceSubscription, error) {
	return f.sub, f.err
}

var active = fakeSubscriptions{sub: &workspace_plan.WorkspaceSubscription{}}

func TestFundsGate(t *testing.T) {
	cases := []struct {
		name   string
		ledger *fakeLedger
		subs   fakeSubscriptions
		want   error
	}{
		{"funded and subscribed", &fakeLedger{balance: 1_000_000}, active, nil},
		{"exactly at the AI floor", &fakeLedger{balance: balance.MinAIFloorMicros}, active, nil},
		// The same floor every other AI spender uses; "> 0" let a workspace with a fraction of a cent start a paid call.
		{"below the AI floor", &fakeLedger{balance: balance.MinAIFloorMicros - 1}, active, ErrInsufficientBalance},
		{"negative balance", &fakeLedger{balance: -5}, active, ErrInsufficientBalance},
		{"no subscription", &fakeLedger{balance: 1_000_000}, fakeSubscriptions{}, ErrNoSubscription},
		{"subscription lookup fails closed", &fakeLedger{balance: 1_000_000}, fakeSubscriptions{err: errors.New("db")}, ErrNoSubscription},
		{"unreadable balance fails closed", &fakeLedger{balance: 1_000_000, err: errors.New("db down")}, active, ErrInsufficientBalance},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := NewFundsGate(tc.ledger, tc.subs).Check("ws"); !errors.Is(err, tc.want) {
				t.Fatalf("Check() = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestFundsGateReadsThroughTheSharedCache(t *testing.T) {
	ledger := &fakeLedger{balance: 1_000_000}
	gate := NewFundsGate(ledger, active)
	_ = gate.Check("ws")
	_ = gate.Check("ws")
	// The cache is one key per workspace shared by every replica and every agent reply. Writes keep it fresh
	// (CachedBalanceRepository invalidates on every debit and credit); a read path must never delete it.
	if ledger.invalidated != 0 || ledger.reads != 2 {
		t.Fatalf("invalidated = %d reads = %d, want cached reads only", ledger.invalidated, ledger.reads)
	}
}
