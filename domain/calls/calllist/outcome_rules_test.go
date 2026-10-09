package calllist

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/lead"
)

func TestOnlyTheDialerStampsTheCallOnAnOpenItem(t *testing.T) {
	cases := []struct {
		name      string
		item      Item
		by        string
		want      error
		reservedT *time.Time
	}{
		{"the live reserver", reservedBy("worker-1", at(time.Minute)), "worker-1", nil, at(ReservationTTL)},
		{"the reserver after the reservation expired", reservedBy("worker-1", at(-time.Minute)), "worker-1", nil, at(ReservationTTL)},
		{"the dialer after releasing the item", pendingItem(), "worker-1", nil, nil},
		{"a colleague holds the item", reservedBy("worker-2", at(time.Minute)), "worker-1", ErrItemTaken, at(time.Minute)},
		{"a colleague holds an expired reservation", reservedBy("worker-2", at(-time.Minute)), "worker-1", ErrItemTaken, at(-time.Minute)},
		{"a closed item", func() Item { i := pendingItem(); i.State = StateClosed; return i }(), "worker-1", ErrItemClosed, nil},
		{"nobody", reservedBy("worker-1", at(time.Minute)), " ", ErrActorRequired, at(time.Minute)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			item := tc.item
			err := item.Stamp(tc.by, "call-9", itemNow)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Stamp = %v, want %v", err, tc.want)
			}
			if tc.want != nil && item.LastCallID != tc.item.LastCallID {
				t.Fatalf("a refused stamp rewrote the call: %+v", item)
			}
			if tc.want == nil && item.LastCallID != "call-9" {
				t.Fatalf("stamped item = %+v", item)
			}
			if (tc.reservedT == nil) != (item.ReservedUntil == nil) || (tc.reservedT != nil && !item.ReservedUntil.Equal(*tc.reservedT)) {
				t.Fatalf("reserved until %v, want %v", item.ReservedUntil, tc.reservedT)
			}
		})
	}
}

func callbackRecorded() Item {
	i := called(pendingItem())
	i.Disposition, i.CallbackAt = DispositionCallback, at(-time.Minute)
	return i
}

func TestACallbackIsClosedOnlyAfterANewCall(t *testing.T) {
	waiting := callbackRecorded()
	if _, err := waiting.Close(Closing{By: "worker-1", Disposition: "interessado"}, theCall(), catalogue(), itemNow); !errors.Is(err, ErrItemNotCalled) {
		t.Fatalf("closing a callback with the call that asked for it = %v, want ErrItemNotCalled", err)
	}

	held := callbackRecorded()
	if err := held.Reserve("worker-1", itemNow); err != nil {
		t.Fatal(err)
	}
	if _, err := held.Close(Closing{By: "worker-1", Disposition: "interessado"}, theCall(), catalogue(), itemNow); !errors.Is(err, ErrItemNotCalled) {
		t.Fatalf("the new reserver closing before calling = %v, want ErrItemNotCalled", err)
	}
	if err := held.Stamp("worker-1", "call-2", itemNow); err != nil {
		t.Fatal(err)
	}
	if held.Disposition != "" {
		t.Fatalf("a new call keeps the old callback outcome: %+v", held)
	}
	again := &CallFacts{ID: "call-2", WorkspaceID: "ws-1", LeadID: "lead-1", AgentID: "worker-1"}
	if closed, err := held.Close(Closing{By: "worker-1", Disposition: "interessado"}, again, catalogue(), itemNow); err != nil || !closed {
		t.Fatalf("closing after the new call = %v, %v", closed, err)
	}
}

func TestARefusalClosesTheItemOnlyForAFactAboutTheLead(t *testing.T) {
	cases := []struct {
		name   string
		reason SkipReason
		closes bool
	}{
		{"blocked", SkipReason(lead.DialRefusedBlocked), true},
		{"opted out", SkipReason(lead.DialRefusedOptedOut), true},
		{"gone", SkipGone, true},
		{"the number is no longer the lead's", SkipReason(lead.DialRefusedNumberNotHeld), true},
		{"an invalid number", SkipReason(lead.DialRefusedInvalidNumber), true},
		{"a dial purpose the server does not know", SkipReason(lead.DialRefusedUnknownPurpose), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.reason.Reversible() == tc.closes {
				t.Fatalf("Reversible = %v for %s", tc.reason.Reversible(), tc.reason)
			}
			item := reservedBy("worker-1", at(time.Minute))
			closed, err := item.Refuse(tc.reason, itemNow)
			if err != nil {
				t.Fatal(err)
			}
			if closed != tc.closes || item.Refusal != string(tc.reason) || item.ReservedBy != "" || item.ReservedUntil != nil {
				t.Fatalf("refused item = %+v, closed %v", item, closed)
			}
			if tc.closes && (item.State != StateClosed || item.Disposition != DispositionRefused || item.ClosedAt == nil) {
				t.Fatalf("closed item = %+v", item)
			}
			if !tc.closes && (item.State != StatePending || item.Disposition != "" || item.ClosedAt != nil ||
				item.CallbackAt == nil || !item.CallbackAt.Equal(itemNow.Add(RefusalRecheck))) {
				t.Fatalf("an item refused for a reversible reason = %+v, want it pending until the recheck", item)
			}
		})
	}
}

func TestAClosedItemCannotBeRefused(t *testing.T) {
	item := pendingItem()
	item.State, item.Disposition = StateClosed, "interessado"
	if _, err := item.Refuse(SkipReason(lead.DialRefusedBlocked), itemNow); !errors.Is(err, ErrItemClosed) {
		t.Fatalf("Refuse = %v, want ErrItemClosed", err)
	}
	if item.Disposition != "interessado" {
		t.Fatalf("a refused refusal changed the item: %+v", item)
	}
}

func TestReservingAnItemClearsTheRefusalItWaitedOn(t *testing.T) {
	item := pendingItem()
	if _, err := item.Refuse(SkipReason(lead.DialRefusedUnknownPurpose), itemNow); err != nil {
		t.Fatal(err)
	}
	if err := item.Reserve("worker-1", itemNow.Add(RefusalRecheck)); err != nil {
		t.Fatal(err)
	}
	if item.Refusal != "" {
		t.Fatalf("a reserved item still reads as refused: %+v", item)
	}
}

func TestReadmittingAnItemAppliesTheCallListDialRule(t *testing.T) {
	blocked := phoneLead()
	blocked.Blocked = true
	cases := []struct {
		name    string
		lead    *lead.Lead
		phone   string
		context lead.DialContext
		want    SkipReason
	}{
		{"callable", phoneLead(), "5511987654321", lead.DialContext{Purpose: lead.DialCallList}, ""},
		{"gone", nil, "5511987654321", lead.DialContext{Purpose: lead.DialCallList}, SkipGone},
		{"blocked", blocked, "5511987654321", lead.DialContext{Purpose: lead.DialCallList}, SkipReason(lead.DialRefusedBlocked)},
		{"a number the lead no longer holds", phoneLead(), "5511900000000", lead.DialContext{Purpose: lead.DialCallList}, SkipReason(lead.DialRefusedNumberNotHeld)},
		{"no purpose", phoneLead(), "5511987654321", lead.DialContext{}, SkipReason(lead.DialRefusedUnknownPurpose)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Readmit(tc.lead, tc.phone, nil, tc.context); got != tc.want {
				t.Fatalf("Readmit = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAListTakesOutcomesOnlyWhileActiveOrPaused(t *testing.T) {
	for status, want := range map[Status]error{
		StatusActive: nil, StatusPaused: nil, StatusArchived: ErrListNotActive, StatusBuilding: ErrListNotActive, StatusFailed: ErrListNotActive,
	} {
		t.Run(string(status), func(t *testing.T) {
			l := List{Status: status}
			if err := l.AcceptsOutcomes(); !errors.Is(err, want) {
				t.Fatalf("AcceptsOutcomes = %v, want %v", err, want)
			}
		})
	}
}

func TestAChangeWithNoFieldChangesNothing(t *testing.T) {
	l := List{Status: StatusActive, Name: "Lista", AssigneeIDs: []string{"worker-1"}}
	if err := l.Change(Change{By: "manager-1"}, listNow); !errors.Is(err, ErrNothingToChange) {
		t.Fatalf("an empty change = %v", err)
	}
}

func TestAnItemRowNamesTheLeadOnlyByARealName(t *testing.T) {
	cases := map[string]ItemView{
		"Maria Souza": {LeadName: "Maria Souza", LeadNumber: "5511987654321"},
		"":            {LeadName: "5511987654321", LeadNumber: "5511987654321"},
	}
	for want, view := range cases {
		if got := view.LeadRealName(); got != want {
			t.Fatalf("LeadRealName(%+v) = %q, want %q", view, got, want)
		}
	}
}
