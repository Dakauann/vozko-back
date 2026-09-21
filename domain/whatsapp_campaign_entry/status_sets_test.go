package whatsapp_campaign_entry

import "testing"

func TestAllStatusesIsTheSetValidAccepts(t *testing.T) {
	for _, s := range AllStatuses() {
		if !ValidStatus(s) {
			t.Errorf("AllStatuses contains %q but Valid() rejects it", s)
		}
	}
	if ValidStatus(SendStatus("SOMETHING_ELSE")) {
		t.Error("Valid() accepted a status that is not in AllStatuses")
	}
	if ValidStatus(SendStatus("")) {
		t.Error("Valid() accepted the empty status")
	}
}

func TestDispatchedStatusesPartitionsAllStatuses(t *testing.T) {
	dispatched := make(map[SendStatus]bool)
	for _, s := range DispatchedStatuses() {
		dispatched[s] = true
	}
	nonDispatch := make(map[SendStatus]bool)
	for _, s := range NonDispatchStatuses() {
		nonDispatch[s] = true
	}

	for _, s := range AllStatuses() {
		inOne := dispatched[s]
		inOther := nonDispatch[s]
		if inOne == inOther {
			t.Errorf("status %q is in %v dispatched and %v non-dispatch; every status must be in exactly one", s, inOne, inOther)
		}
	}

	if len(dispatched)+len(nonDispatch) != len(AllStatuses()) {
		t.Errorf("sets cover %d statuses, AllStatuses has %d", len(dispatched)+len(nonDispatch), len(AllStatuses()))
	}
}

func TestDispatchedStatusesMatchesDispatchesArithmetic(t *testing.T) {
	counts := StatusCounts{
		Total:                   100,
		Pending:                 10,
		Sent:                    20,
		Delivered:               30,
		Read:                    35,
		Failed:                  4,
		NotEligiblePossibleSpam: 1,
	}

	perStatus := map[SendStatus]int64{
		SendStatusPending:                 counts.Pending,
		SendStatusSent:                    counts.Sent,
		SendStatusDelivered:               counts.Delivered,
		SendStatusRead:                    counts.Read,
		SendStatusFailed:                  counts.Failed,
		SendStatusNotEligiblePossibleSpam: counts.NotEligiblePossibleSpam,
	}

	var additive int64
	for _, s := range DispatchedStatuses() {
		additive += perStatus[s]
	}

	if additive != counts.Dispatches() {
		t.Errorf("sum over DispatchedStatuses = %d, Dispatches() = %d", additive, counts.Dispatches())
	}
}

func TestStatusStringsRendersForSQL(t *testing.T) {
	got := StatusStrings(NonDispatchStatuses())
	want := []string{"PENDING", "FAILED", "NOT_ELIGIBLE_POSSIBLE_SPAM"}

	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
