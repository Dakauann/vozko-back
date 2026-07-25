package whatsapp_campaign_entry

import "testing"

// Dispatches ("disparos") is the BILLED-send count: Total minus the buckets a
// workspace is never charged for (PENDING, FAILED, NOT_ELIGIBLE_POSSIBLE_SPAM).
// It is defined subtractively, not as SENT+DELIVERED+READ.
func TestStatusCountsDispatches(t *testing.T) {
	sc := StatusCounts{
		Total:                   100,
		Pending:                 10,
		Sent:                    30,
		Delivered:               25,
		Read:                    15,
		Failed:                  12,
		NotEligiblePossibleSpam: 8,
	}

	if got, want := sc.Dispatches(), int64(70); got != want { // 100 - 10 - 12 - 8
		t.Errorf("Dispatches() = %d, want %d", got, want)
	}
	if got, want := sc.Processed(), int64(90); got != want { // 30 + 25 + 15 + 12 + 8
		t.Errorf("Processed() = %d, want %d", got, want)
	}
}

// The defining property of the subtractive form: a billed entry that is NOT in
// the SENT/DELIVERED/READ delivery buckets (here, Total exceeds the named
// buckets by 40) still counts as a dispatch. An additive SENT+DELIVERED+READ
// would wrongly report 40 here.
func TestStatusCountsDispatchesCountsBilledRegardlessOfDeliveryStatus(t *testing.T) {
	sc := StatusCounts{
		Total:                   100, // 40 billed entries sit outside the named delivery buckets
		Pending:                 10,
		Sent:                    40,
		Failed:                  5,
		NotEligiblePossibleSpam: 5,
	}

	if got, want := sc.Dispatches(), int64(80); got != want { // 100 - 10 - 5 - 5
		t.Errorf("Dispatches() = %d, want %d", got, want)
	}
}

func TestStatusCountsDispatchesExcludesSpamFailedAndPending(t *testing.T) {
	// Total is exactly the sum of the non-billed buckets → nothing billed.
	sc := StatusCounts{Total: 21, Pending: 5, Failed: 7, NotEligiblePossibleSpam: 9}

	if got := sc.Dispatches(); got != 0 {
		t.Errorf("Dispatches() = %d, want 0 when everything is pending/failed/spam", got)
	}
}

// Defensive: inconsistent counts (excluded buckets exceed Total) must clamp to 0
// rather than report a negative dispatch count.
func TestStatusCountsDispatchesClampsNegative(t *testing.T) {
	sc := StatusCounts{Total: 5, Pending: 4, Failed: 4, NotEligiblePossibleSpam: 4}
	if got := sc.Dispatches(); got != 0 {
		t.Errorf("Dispatches() = %d, want 0 (clamped)", got)
	}
}

func TestStatusCountsDispatchesZeroValue(t *testing.T) {
	if got := (StatusCounts{}).Dispatches(); got != 0 {
		t.Errorf("Dispatches() = %d, want 0 for empty counts", got)
	}
}
