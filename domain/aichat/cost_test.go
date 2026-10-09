package aichat

import (
	"testing"

	"vozko/domain/aiusage"
	"vozko/domain/balance"
)

func TestChargeReferenceNamesTheThread(t *testing.T) {
	if got := ChargeReference("th-1"); got != "aichat:th-1" {
		t.Fatalf("ChargeReference() = %q", got)
	}
}

func TestTheCostOfATrackedThreadIsWhatItsChargesAddUpTo(t *testing.T) {
	thread := &Thread{ID: "th-1", CostTracked: true}
	cost := CostOf(thread, balance.ReferenceTotals{BillingMicros: 420_000, Transactions: 3}, nil)
	if !cost.Available || cost.AmountMicros != 420_000 || cost.Currency != balance.BillingCurrency {
		t.Fatalf("CostOf() = %+v", cost)
	}
	if empty := CostOf(thread, balance.ReferenceTotals{}, nil); !empty.Available || empty.AmountMicros != 0 {
		t.Fatalf("a tracked thread with no charges costs zero, got %+v", empty)
	}
}

func TestTheCostIsUnavailableWhenItCannotBeExact(t *testing.T) {
	cases := map[string]struct {
		thread *Thread
		totals balance.ReferenceTotals
	}{
		"a thread from before charges named their thread": {&Thread{ID: "th-1"}, balance.ReferenceTotals{BillingMicros: 100}},
		"a charge without its exchange rate":              {&Thread{ID: "th-1", CostTracked: true}, balance.ReferenceTotals{BillingMicros: 100, Transactions: 1, MissingRate: true}},
		"no thread":                                       {nil, balance.ReferenceTotals{}},
	}
	for name, tc := range cases {
		cost := CostOf(tc.thread, tc.totals, nil)
		if cost.Available || cost.AmountMicros != 0 {
			t.Fatalf("%s: the cost must be unavailable, got %+v", name, cost)
		}
	}
}

func TestTheUsageOfAThreadIsShownWhenEveryChargeHasItsTokens(t *testing.T) {
	thread := &Thread{ID: "th-1", CostTracked: true}
	usage := aiusage.Totals{Calls: 2, Billed: 3, Tokens: aiusage.Tokens{Input: 3000, Output: 300, CacheRead: 2300, CacheWrite: 50, Reasoning: 20}}
	cost := CostOf(thread, balance.ReferenceTotals{BillingMicros: 900, Transactions: 3, Debits: 3}, &usage)
	if !cost.Usage.Available || cost.Usage.Calls != 2 || cost.Usage.Tokens != usage.Tokens {
		t.Fatalf("usage = %+v", cost.Usage)
	}
}

func TestTheUsageIsUnavailableWhenItCannotBeExact(t *testing.T) {
	tracked := &Thread{ID: "th-1", CostTracked: true}
	partial := aiusage.Totals{Calls: 2, Billed: 2, Tokens: aiusage.Tokens{Input: 3000}}
	cases := map[string]struct {
		thread *Thread
		usage  *aiusage.Totals
	}{
		"a charge without its usage row":                  {tracked, &partial},
		"usage that was not read":                         {tracked, nil},
		"a thread from before charges named their thread": {&Thread{ID: "th-1"}, &partial},
	}
	for name, tc := range cases {
		cost := CostOf(tc.thread, balance.ReferenceTotals{BillingMicros: 900, Transactions: 3, Debits: 3}, tc.usage)
		if cost.Usage != (ThreadUsage{}) {
			t.Fatalf("%s: the usage must be unavailable and empty, got %+v", name, cost.Usage)
		}
	}
}
