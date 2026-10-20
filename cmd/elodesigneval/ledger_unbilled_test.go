package main

import "testing"

func TestTheLedgerCountsCallsTheProviderCouldNotBillSoTheCostCapIsKnownToUndercount(t *testing.T) {
	ledger := newReferenceLedger(&usageLedger{})
	ledger.IncBillingSkipped("no_id_before_timeout")
	ledger.IncBillingSkipped("recovery_failed")
	if got := ledger.Unbilled(); got != 2 {
		t.Fatalf("Unbilled() = %d, want 2", got)
	}
}
