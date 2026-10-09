package calllist

import (
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/lead"
)

var itemNow = time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC)

func at(d time.Duration) *time.Time {
	t := itemNow.Add(d)
	return &t
}

func pendingItem() Item {
	return Item{ID: "item-1", ListID: "list-1", WorkspaceID: "ws-1", LeadID: "lead-1", Phone: "5511987654321", Position: 1, State: StatePending}
}

func reservedBy(user string, until *time.Time) Item {
	i := pendingItem()
	i.State, i.ReservedBy, i.ReservedUntil = StateReserved, user, until
	return i
}

func TestAnItemIsClaimableWhenPendingAndDueOrWhenItsReservationExpired(t *testing.T) {
	cases := []struct {
		name string
		item Item
		want bool
	}{
		{"pending", pendingItem(), true},
		{"a callback that is due", func() Item { i := pendingItem(); i.CallbackAt = at(-time.Minute); return i }(), true},
		{"a callback due right now", func() Item { i := pendingItem(); i.CallbackAt = at(0); return i }(), true},
		{"a callback for later", func() Item { i := pendingItem(); i.CallbackAt = at(time.Hour); return i }(), false},
		{"reserved and live", reservedBy("worker-1", at(time.Minute)), false},
		{"reserved past its deadline", reservedBy("worker-1", at(-time.Second)), true},
		{"reserved without a deadline", reservedBy("worker-1", nil), true},
		{"closed", func() Item { i := pendingItem(); i.State = StateClosed; return i }(), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.item.Claimable(itemNow); got != tc.want {
				t.Fatalf("Claimable = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestReservingAnItemHoldsItForTheReservationTime(t *testing.T) {
	i := pendingItem()
	if err := i.Reserve("worker-1", itemNow); err != nil {
		t.Fatal(err)
	}
	if i.State != StateReserved || i.ReservedBy != "worker-1" || !i.ReservedUntil.Equal(itemNow.Add(ReservationTTL)) {
		t.Fatalf("item = %+v", i)
	}
	if err := i.Reserve("worker-1", itemNow.Add(time.Minute)); err != nil {
		t.Fatalf("the holder reserving again = %v", err)
	}
	if err := i.Reserve("worker-2", itemNow.Add(time.Minute)); !errors.Is(err, ErrItemTaken) {
		t.Fatalf("another worker = %v, want ErrItemTaken", err)
	}
	if err := i.Reserve("worker-2", itemNow.Add(time.Minute+ReservationTTL+time.Second)); err != nil {
		t.Fatalf("after the reservation expired another worker takes it, got %v", err)
	}
	if i.ReservedBy != "worker-2" {
		t.Fatalf("reserved by %q", i.ReservedBy)
	}
	if err := i.Reserve("", itemNow); !errors.Is(err, ErrActorRequired) {
		t.Fatalf("nobody = %v", err)
	}
	later := pendingItem()
	later.CallbackAt = at(time.Hour)
	if err := later.Reserve("worker-1", itemNow); !errors.Is(err, ErrItemTaken) {
		t.Fatalf("a callback for later = %v", err)
	}
}

func TestOnlyTheReserverReleasesAnItemBackToTheQueue(t *testing.T) {
	i := reservedBy("worker-1", at(time.Minute))
	if err := i.Release("worker-2", itemNow); !errors.Is(err, ErrItemNotReserved) {
		t.Fatalf("another worker = %v", err)
	}
	if err := i.Release("worker-1", itemNow); err != nil {
		t.Fatal(err)
	}
	if i.State != StatePending || i.ReservedBy != "" || i.ReservedUntil != nil {
		t.Fatalf("item = %+v", i)
	}
	if err := i.Release("worker-1", itemNow); !errors.Is(err, ErrItemNotReserved) {
		t.Fatalf("releasing a pending item = %v", err)
	}
	expired := reservedBy("worker-1", at(-time.Minute))
	if err := expired.Release("worker-1", itemNow); err != nil {
		t.Fatalf("the reserver releasing an expired reservation = %v", err)
	}
}

func TestAnItemIsDialedOnlyByItsLiveReserverForItsLeadAndNumber(t *testing.T) {
	cases := []struct {
		name   string
		item   Item
		user   string
		leadID string
		number string
		want   error
	}{
		{"the reserver", reservedBy("worker-1", at(time.Minute)), "worker-1", "lead-1", "5511987654321", nil},
		{"the number without the ninth digit", reservedBy("worker-1", at(time.Minute)), "worker-1", "lead-1", "551187654321", nil},
		{"another worker", reservedBy("worker-1", at(time.Minute)), "worker-2", "lead-1", "5511987654321", ErrItemNotReserved},
		{"an expired reservation", reservedBy("worker-1", at(-time.Second)), "worker-1", "lead-1", "5511987654321", ErrItemNotReserved},
		{"a pending item", pendingItem(), "worker-1", "lead-1", "5511987654321", ErrItemNotReserved},
		{"another lead", reservedBy("worker-1", at(time.Minute)), "worker-1", "lead-2", "5511987654321", ErrItemMismatch},
		{"another number", reservedBy("worker-1", at(time.Minute)), "worker-1", "lead-1", "5511900000000", ErrItemMismatch},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.item.CheckDial(tc.user, tc.leadID, tc.number, itemNow); !errors.Is(err, tc.want) {
				t.Fatalf("CheckDial = %v, want %v", err, tc.want)
			}
		})
	}
}

func catalogue() *conversation.OutcomeCapture {
	return &conversation.OutcomeCapture{Outcomes: []conversation.Outcome{
		{Code: "interessado", Label: "Interessado", IsDurable: true},
		{Code: "sem_interesse", Label: "Sem interesse"},
	}}
}

func theCall() *CallFacts {
	return &CallFacts{ID: "call-1", WorkspaceID: "ws-1", LeadID: "lead-1", AgentID: "worker-1"}
}

func called(i Item) Item {
	i.LastCallID = "call-1"
	return i
}

func TestClosingAnItemRecordsTheWorkspaceOutcomeAgainstTheItemsOwnCall(t *testing.T) {
	cases := []struct {
		name    string
		item    Item
		closing Closing
		call    *CallFacts
		catalog *conversation.OutcomeCapture
		want    error
	}{
		{"the reserver after the call", called(reservedBy("worker-1", at(time.Minute))), Closing{By: "worker-1", Disposition: "Interessado "}, theCall(), catalogue(), nil},
		{"no call was made", reservedBy("worker-1", at(time.Minute)), Closing{By: "worker-1", Disposition: "interessado"}, nil, catalogue(), ErrItemNotCalled},
		{"the stamped call is not loaded", called(reservedBy("worker-1", at(time.Minute))), Closing{By: "worker-1", Disposition: "interessado"}, nil, catalogue(), ErrCallNotTheItems},
		{"a call of another lead", called(reservedBy("worker-1", at(time.Minute))), Closing{By: "worker-1", Disposition: "interessado"}, &CallFacts{ID: "call-1", WorkspaceID: "ws-1", LeadID: "lead-9", AgentID: "worker-1"}, catalogue(), ErrCallNotTheItems},
		{"a call of another workspace", called(reservedBy("worker-1", at(time.Minute))), Closing{By: "worker-1", Disposition: "interessado"}, &CallFacts{ID: "call-1", WorkspaceID: "ws-9", LeadID: "lead-1", AgentID: "worker-1"}, catalogue(), ErrCallNotTheItems},
		{"a call made by someone else", called(reservedBy("worker-1", at(time.Minute))), Closing{By: "worker-1", Disposition: "interessado"}, &CallFacts{ID: "call-1", WorkspaceID: "ws-1", LeadID: "lead-1", AgentID: "worker-2"}, catalogue(), ErrCallNotTheItems},
		{"another call than the stamped one", called(reservedBy("worker-1", at(time.Minute))), Closing{By: "worker-1", Disposition: "interessado"}, &CallFacts{ID: "call-2", WorkspaceID: "ws-1", LeadID: "lead-1", AgentID: "worker-1"}, catalogue(), ErrCallNotTheItems},
		{"the reserver of the call after the reservation expired", called(reservedBy("worker-1", at(-time.Hour))), Closing{By: "worker-1", Disposition: "interessado"}, theCall(), catalogue(), nil},
		{"the reserver of the call after the item went back to the queue", called(pendingItem()), Closing{By: "worker-1", Disposition: "interessado"}, theCall(), catalogue(), nil},
		{"someone else took the item after it expired", called(reservedBy("worker-2", at(time.Minute))), Closing{By: "worker-1", Disposition: "interessado"}, theCall(), catalogue(), ErrItemTaken},
		{"someone else holds an expired reservation", called(reservedBy("worker-2", at(-time.Minute))), Closing{By: "worker-1", Disposition: "interessado"}, theCall(), catalogue(), ErrItemTaken},
		{"an outcome outside the catalogue", called(reservedBy("worker-1", at(time.Minute))), Closing{By: "worker-1", Disposition: "vendido"}, theCall(), catalogue(), ErrDispositionUnknown},
		{"a reserved system outcome", called(reservedBy("worker-1", at(time.Minute))), Closing{By: "worker-1", Disposition: "_system_auto_close"}, theCall(), catalogue(), ErrDispositionReserved},
		{"no outcome", called(reservedBy("worker-1", at(time.Minute))), Closing{By: "worker-1", Disposition: " "}, theCall(), catalogue(), ErrDispositionRequired},
		{"a workspace without outcomes", called(reservedBy("worker-1", at(time.Minute))), Closing{By: "worker-1", Disposition: "interessado"}, theCall(), nil, ErrNoOutcomes},
		{"a workspace with an empty catalogue", called(reservedBy("worker-1", at(time.Minute))), Closing{By: "worker-1", Disposition: "interessado"}, theCall(), &conversation.OutcomeCapture{}, ErrNoOutcomes},
		{"a note too long", called(reservedBy("worker-1", at(time.Minute))), Closing{By: "worker-1", Disposition: "interessado", Note: strings.Repeat("a", MaxNoteLength+1)}, theCall(), catalogue(), ErrNoteTooLong},
		{"a callback time on an ordinary outcome", called(reservedBy("worker-1", at(time.Minute))), Closing{By: "worker-1", Disposition: "interessado", CallbackAt: at(time.Hour)}, theCall(), catalogue(), ErrCallbackTime},
		{"an item already closed", func() Item { i := called(pendingItem()); i.State = StateClosed; return i }(), Closing{By: "worker-1", Disposition: "interessado"}, theCall(), catalogue(), ErrItemClosed},
		{"nobody", called(reservedBy("worker-1", at(time.Minute))), Closing{Disposition: "interessado"}, theCall(), catalogue(), ErrActorRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			item := tc.item
			closed, err := item.Close(tc.closing, tc.call, tc.catalog, itemNow)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Close = %v, want %v", err, tc.want)
			}
			if tc.want != nil {
				if item.State != tc.item.State || item.Disposition != tc.item.Disposition {
					t.Fatalf("a refused close changed the item: %+v", item)
				}
				return
			}
			if !closed || item.State != StateClosed || item.Disposition != "interessado" || item.ClosedBy != "worker-1" ||
				item.ClosedAt == nil || !item.ClosedAt.Equal(itemNow) || item.ReservedBy != "" || item.ReservedUntil != nil {
				t.Fatalf("closed item = %+v", item)
			}
		})
	}
}

func TestACallbackReturnsTheItemToTheQueueForAFutureTime(t *testing.T) {
	item := called(reservedBy("worker-1", at(time.Minute)))
	closed, err := item.Close(Closing{By: "worker-1", Disposition: DispositionCallback, Note: " ligar depois do almoço ", CallbackAt: at(2 * time.Hour)}, theCall(), nil, itemNow)
	if err != nil {
		t.Fatal(err)
	}
	if closed || item.State != StatePending || item.Disposition != DispositionCallback || item.Note != "ligar depois do almoço" ||
		item.CallbackAt == nil || !item.CallbackAt.Equal(itemNow.Add(2*time.Hour)) || item.ReservedBy != "" || item.ClosedAt != nil {
		t.Fatalf("item = %+v", item)
	}
	if item.Claimable(itemNow) || !item.Claimable(itemNow.Add(2*time.Hour)) {
		t.Fatal("a callback is claimable only once it is due")
	}

	for name, when := range map[string]*time.Time{"no time": nil, "now": at(0), "the past": at(-time.Minute), "too far ahead": at(MaxCallbackAhead + time.Hour)} {
		t.Run(name, func(t *testing.T) {
			item := called(reservedBy("worker-1", at(time.Minute)))
			if _, err := item.Close(Closing{By: "worker-1", Disposition: DispositionCallback, CallbackAt: when}, theCall(), catalogue(), itemNow); !errors.Is(err, ErrCallbackTime) {
				t.Fatalf("Close = %v, want ErrCallbackTime", err)
			}
		})
	}
}

func TestARefusedLeadClosesItsItemWithTheReasonAndNoCaller(t *testing.T) {
	item := reservedBy("worker-1", at(time.Minute))
	if closed, err := item.Refuse(SkipReason(lead.DialRefusedBlocked), itemNow); err != nil || !closed {
		t.Fatalf("Refuse = %v, %v", closed, err)
	}
	if item.State != StateClosed || item.Disposition != DispositionRefused || item.Refusal != string(lead.DialRefusedBlocked) ||
		item.ClosedBy != "" || item.ClosedAt == nil || item.ReservedBy != "" {
		t.Fatalf("item = %+v", item)
	}
}

func TestTheStampedCallKeepsTheReservationAliveForTheCall(t *testing.T) {
	item := reservedBy("worker-1", at(time.Minute))
	mustStamp(t, &item, "call-1")
	if item.LastCallID != "call-1" || !item.ReservedUntil.Equal(itemNow.Add(ReservationTTL)) {
		t.Fatalf("item = %+v", item)
	}
	long := reservedBy("worker-1", at(ReservationTTL+time.Hour))
	mustStamp(t, &long, "call-2")
	if !long.ReservedUntil.Equal(itemNow.Add(ReservationTTL + time.Hour)) {
		t.Fatal("stamping must never shorten a reservation")
	}
	pending := pendingItem()
	mustStamp(t, &pending, "call-3")
	if pending.ReservedUntil != nil || pending.LastCallID != "call-3" {
		t.Fatalf("a pending item = %+v", pending)
	}
}

func mustStamp(t *testing.T, item *Item, callID string) {
	t.Helper()
	if err := item.Stamp("worker-1", callID, itemNow); err != nil {
		t.Fatalf("Stamp = %v", err)
	}
}
