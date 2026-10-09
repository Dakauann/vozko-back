package aichat_usecase

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/aichat"
	"vozko/domain/aiusage"
	"vozko/domain/balance"
)

type costThreads struct {
	threads map[string]*aichat.Thread
	created *aichat.Thread
}

func (c *costThreads) Create(t *aichat.Thread) error {
	t.ID = "th-new"
	c.created = t
	return nil
}
func (c *costThreads) GetByID(id string) (*aichat.Thread, error) {
	if t, ok := c.threads[id]; ok {
		return t, nil
	}
	return nil, aichat.ErrThreadNotFound
}
func (c *costThreads) ListByUser(aichat.ListThreadsInput) ([]*aichat.Thread, int64, error) {
	return nil, 0, nil
}
func (c *costThreads) Rename(string, string) error           { return nil }
func (c *costThreads) Touch(string, time.Time, string) error { return nil }
func (c *costThreads) Delete(string) error                   { return nil }

type ledger struct {
	totals    balance.ReferenceTotals
	err       error
	workspace string
	prefix    string
}

func (l *ledger) TotalsUnder(workspaceID, prefix string) (balance.ReferenceTotals, error) {
	l.workspace, l.prefix = workspaceID, prefix
	return l.totals, l.err
}

type usageLedger struct {
	totals    aiusage.Totals
	err       error
	workspace string
	prefix    string
}

func (u *usageLedger) TotalsUnder(workspaceID, prefix string) (aiusage.Totals, error) {
	u.workspace, u.prefix = workspaceID, prefix
	return u.totals, u.err
}

func costService(l *ledger) (*Service, *costThreads) {
	threads := &costThreads{threads: map[string]*aichat.Thread{
		"th-1":   {ID: "th-1", WorkspaceID: "ws-1", UserID: "u-1", CostTracked: true},
		"th-old": {ID: "th-old", WorkspaceID: "ws-1", UserID: "u-1"},
	}}
	svc := NewService(threads, nil, nil, nil)
	if l != nil {
		svc.SetChargeLedger(l)
	}
	return svc, threads
}

func TestANewThreadHasItsCostTracked(t *testing.T) {
	svc, threads := costService(&ledger{})
	if _, err := svc.CreateThread("ws-1", "u-1", "m", ""); err != nil {
		t.Fatal(err)
	}
	if !threads.created.CostTracked {
		t.Fatal("a thread created now names every charge, so its cost is tracked")
	}
}

func TestTheThreadCostAddsUpItsCharges(t *testing.T) {
	l := &ledger{totals: balance.ReferenceTotals{LedgerMicros: 80_000, BillingMicros: 420_000, Transactions: 4}}
	svc, _ := costService(l)
	cost, err := svc.ThreadCost("ws-1", "u-1", "th-1")
	if err != nil {
		t.Fatal(err)
	}
	if !cost.Available || cost.AmountMicros != 420_000 || cost.Currency != balance.BillingCurrency {
		t.Fatalf("cost = %+v", cost)
	}
	if l.workspace != "ws-1" || l.prefix != "aichat:th-1:" {
		t.Fatalf("the ledger must be read for the thread only, got %q %q", l.workspace, l.prefix)
	}
}

func TestTheThreadCostFailsClosed(t *testing.T) {
	svc, _ := costService(&ledger{totals: balance.ReferenceTotals{BillingMicros: 1}})
	if _, err := svc.ThreadCost("ws-1", "intruder", "th-1"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("someone else's thread must be refused, got %v", err)
	}
	if _, err := svc.ThreadCost("ws-2", "u-1", "th-1"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("a thread from another workspace must be refused, got %v", err)
	}
	if _, err := svc.ThreadCost("ws-1", "u-1", "missing"); !errors.Is(err, aichat.ErrThreadNotFound) {
		t.Fatalf("a missing thread must say so, got %v", err)
	}
	if cost, err := svc.ThreadCost("ws-1", "u-1", "th-old"); err != nil || cost.Available {
		t.Fatalf("an older thread cannot be priced exactly, got %+v %v", cost, err)
	}
	failing, _ := costService(&ledger{err: errors.New("db down")})
	if _, err := failing.ThreadCost("ws-1", "u-1", "th-1"); err == nil {
		t.Fatal("a ledger failure must not read as a cost")
	}
	unwired, _ := costService(nil)
	if cost, err := unwired.ThreadCost("ws-1", "u-1", "th-1"); err != nil || cost.Available {
		t.Fatalf("without a ledger there is no cost, got %+v %v", cost, err)
	}
}

func TestTheThreadCostCarriesTheTokensItsChargesUsed(t *testing.T) {
	svc, _ := costService(&ledger{totals: balance.ReferenceTotals{BillingMicros: 420_000, Transactions: 2, Debits: 2}})
	usage := &usageLedger{totals: aiusage.Totals{Calls: 2, Billed: 2, Tokens: aiusage.Tokens{Input: 5000, Output: 400, CacheRead: 3000}}}
	svc.SetUsageLedger(usage)
	cost, err := svc.ThreadCost("ws-1", "u-1", "th-1")
	if err != nil {
		t.Fatal(err)
	}
	if !cost.Usage.Available || cost.Usage.Tokens.CacheRead != 3000 || cost.Usage.Calls != 2 {
		t.Fatalf("usage = %+v", cost.Usage)
	}
	if usage.workspace != "ws-1" || usage.prefix != "aichat:th-1:" {
		t.Fatalf("the usage must be read for the thread only, got %q %q", usage.workspace, usage.prefix)
	}
}

func TestTheThreadUsageFailsClosed(t *testing.T) {
	svc, _ := costService(&ledger{totals: balance.ReferenceTotals{BillingMicros: 420_000, Transactions: 1, Debits: 1}})
	if cost, err := svc.ThreadCost("ws-1", "u-1", "th-1"); err != nil || cost.Usage.Available {
		t.Fatalf("without a usage ledger there are no tokens, got %+v %v", cost, err)
	}
	svc.SetUsageLedger(&usageLedger{err: errors.New("db down")})
	if _, err := svc.ThreadCost("ws-1", "u-1", "th-1"); err == nil {
		t.Fatal("a usage failure must not read as a token count")
	}
}
