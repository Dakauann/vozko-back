package whatsapp_campaign_entry

import "testing"

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

	if got, want := sc.Dispatches(), int64(70); got != want {
		t.Errorf("Dispatches() = %d, want %d", got, want)
	}
	if got, want := sc.Processed(), int64(90); got != want {
		t.Errorf("Processed() = %d, want %d", got, want)
	}
}

func TestStatusCountsDispatchesCountsBilledRegardlessOfDeliveryStatus(t *testing.T) {
	sc := StatusCounts{
		Total:                   100,
		Pending:                 10,
		Sent:                    40,
		Failed:                  5,
		NotEligiblePossibleSpam: 5,
	}

	if got, want := sc.Dispatches(), int64(80); got != want {
		t.Errorf("Dispatches() = %d, want %d", got, want)
	}
}

func TestStatusCountsDispatchesExcludesSpamFailedAndPending(t *testing.T) {
	sc := StatusCounts{Total: 21, Pending: 5, Failed: 7, NotEligiblePossibleSpam: 9}

	if got := sc.Dispatches(); got != 0 {
		t.Errorf("Dispatches() = %d, want 0 when everything is pending/failed/spam", got)
	}
}

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
