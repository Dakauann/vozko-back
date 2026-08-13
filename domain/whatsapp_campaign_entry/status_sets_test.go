package whatsapp_campaign_entry

import "testing"

// The three status sets are one list plus two derivations. These tests exist to
// fail the day someone adds a seventh status without deciding which side of the
// billing line it falls on — the alternative is a status that is Valid(), is
// counted in Total, and is silently absent from both Dispatches() and every
// export that asks for "what we sent".

func TestAllStatusesIsTheSetValidAccepts(t *testing.T) {
	for _, s := range AllStatuses() {
		if !s.Valid() {
			t.Errorf("AllStatuses contains %q but Valid() rejects it", s)
		}
	}
	if SendStatus("SOMETHING_ELSE").Valid() {
		t.Error("Valid() accepted a status that is not in AllStatuses")
	}
	if SendStatus("").Valid() {
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

// The additive spelling of "enviados, entregues e lidos" is what an operator
// asks for; the subtractive one is what stays correct. This pins that they mean
// the same thing today, so the export's default preset and the summary's
// headline count the same entries.
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
