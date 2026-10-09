package calllist

import (
	"reflect"
	"testing"
	"time"

	"vozko/domain/calls/cdr"
)

func listIn(status Status) *List {
	return &List{ID: "list-1", WorkspaceID: "ws-1", Status: status, AssigneeIDs: []string{"worker-1"}}
}

func TestAnItemIsClosableOnlyByTheMemberWhoseCallTheServerStamped(t *testing.T) {
	callback := callbackRecorded()
	closedItem := called(pendingItem())
	closedItem.State = StateClosed
	otherList := called(reservedBy("worker-1", at(time.Minute)))
	otherList.ListID = "list-9"
	cases := []struct {
		name string
		list *List
		item Item
		by   string
		call *CallFacts
		want bool
	}{
		{"the live reserver after the call", listIn(StatusActive), called(reservedBy("worker-1", at(time.Minute))), "worker-1", theCall(), true},
		{"the caller after the reservation expired", listIn(StatusActive), called(reservedBy("worker-1", at(-time.Hour))), "worker-1", theCall(), true},
		{"the caller after the item went back to the queue", listIn(StatusActive), called(pendingItem()), "worker-1", theCall(), true},
		{"a paused list still takes outcomes", listIn(StatusPaused), called(reservedBy("worker-1", at(time.Minute))), "worker-1", theCall(), true},
		{"no call was stamped", listIn(StatusActive), reservedBy("worker-1", at(time.Minute)), "worker-1", nil, false},
		{"the stamped call is gone", listIn(StatusActive), called(reservedBy("worker-1", at(time.Minute))), "worker-1", nil, false},
		{"the call was made by a colleague", listIn(StatusActive), called(reservedBy("worker-1", at(time.Minute))), "worker-1", &CallFacts{ID: "call-1", WorkspaceID: "ws-1", LeadID: "lead-1", AgentID: "worker-2"}, false},
		{"the call was to another lead", listIn(StatusActive), called(reservedBy("worker-1", at(time.Minute))), "worker-1", &CallFacts{ID: "call-1", WorkspaceID: "ws-1", LeadID: "lead-9", AgentID: "worker-1"}, false},
		{"a callback waits for a new call", listIn(StatusActive), callback, "worker-1", theCall(), false},
		{"a colleague took the item", listIn(StatusActive), called(reservedBy("worker-2", at(time.Minute))), "worker-1", theCall(), false},
		{"an item already closed", listIn(StatusActive), closedItem, "worker-1", theCall(), false},
		{"an archived list", listIn(StatusArchived), called(reservedBy("worker-1", at(time.Minute))), "worker-1", theCall(), false},
		{"a list still building", listIn(StatusBuilding), called(reservedBy("worker-1", at(time.Minute))), "worker-1", theCall(), false},
		{"an item of another list", listIn(StatusActive), otherList, "worker-1", theCall(), false},
		{"no list", nil, called(reservedBy("worker-1", at(time.Minute))), "worker-1", theCall(), false},
		{"nobody", listIn(StatusActive), called(reservedBy("worker-1", at(time.Minute))), " ", theCall(), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.list.Closable(tc.item, tc.by, tc.call, itemNow); got != tc.want {
				t.Fatalf("Closable = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTheClosableVerdictAgreesWithClose(t *testing.T) {
	items := []Item{
		called(reservedBy("worker-1", at(time.Minute))),
		called(reservedBy("worker-2", at(time.Minute))),
		called(pendingItem()),
		reservedBy("worker-1", at(time.Minute)),
		callbackRecorded(),
	}
	for _, item := range items {
		verdict := item.ClosableBy("worker-1", theCall(), itemNow)
		working := item
		_, err := working.Close(Closing{By: "worker-1", Disposition: "interessado"}, theCall(), catalogue(), itemNow)
		if (verdict == nil) != (err == nil) {
			t.Fatalf("item %+v: verdict %v, close %v", item, verdict, err)
		}
	}
}

func TestAListOffersAManagerOnlyTheStatusesItCanMoveTo(t *testing.T) {
	cases := []struct {
		status Status
		want   []Status
	}{
		{StatusActive, []Status{StatusPaused, StatusArchived}},
		{StatusPaused, []Status{StatusActive, StatusArchived}},
		{StatusArchived, []Status{StatusActive}},
		{StatusBuilding, []Status{}},
		{StatusFailed, []Status{}},
	}
	for _, tc := range cases {
		t.Run(string(tc.status), func(t *testing.T) {
			l := listIn(tc.status)
			moves := l.StatusMoves()
			if !reflect.DeepEqual(moves, tc.want) {
				t.Fatalf("StatusMoves = %v, want %v", moves, tc.want)
			}
			for _, to := range moves {
				moved := listIn(tc.status)
				target := to
				if err := moved.Change(Change{Status: &target}, itemNow); err != nil {
					t.Fatalf("offered move to %s is refused: %v", to, err)
				}
			}
		})
	}
}

func TestAListTellsWhetherItTakesOutcomesAndWhereAManagerCanMoveIt(t *testing.T) {
	cases := []struct {
		name string
		list *List
		want ListVerdict
	}{
		{"an active list", listIn(StatusActive), ListVerdict{AcceptsOutcomes: true, StatusMoves: []Status{StatusPaused, StatusArchived}}},
		{"a paused list", listIn(StatusPaused), ListVerdict{AcceptsOutcomes: true, StatusMoves: []Status{StatusActive, StatusArchived}}},
		{"an archived list", listIn(StatusArchived), ListVerdict{StatusMoves: []Status{StatusActive}}},
		{"a list still building", listIn(StatusBuilding), ListVerdict{StatusMoves: []Status{}}},
		{"a failed list", listIn(StatusFailed), ListVerdict{StatusMoves: []Status{}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.list.Verdict(true); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Verdict = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestAViewerWhoDoesNotManageListsIsOfferedNoStatusMove(t *testing.T) {
	for _, status := range []Status{StatusActive, StatusPaused, StatusArchived, StatusBuilding, StatusFailed} {
		t.Run(string(status), func(t *testing.T) {
			managed := listIn(status).Verdict(true)
			got := listIn(status).Verdict(false)
			if got.StatusMoves == nil || len(got.StatusMoves) != 0 {
				t.Fatalf("StatusMoves = %#v, want an empty list", got.StatusMoves)
			}
			if got.AcceptsOutcomes != managed.AcceptsOutcomes {
				t.Fatalf("Verdict = %+v, the rest must match %+v", got, managed)
			}
		})
	}
}

func TestTheFactsOfACallNameItsWorkspaceLeadAndAgent(t *testing.T) {
	if FactsOf(nil) != nil {
		t.Fatal("no call has facts")
	}
	leadID, agentID := "lead-1", "worker-1"
	got := FactsOf(&cdr.Call{ID: "call-1", WorkspaceID: "ws-1", LeadID: &leadID, AgentID: &agentID})
	if !reflect.DeepEqual(got, theCall()) {
		t.Fatalf("FactsOf = %+v, want %+v", got, theCall())
	}
	bare := FactsOf(&cdr.Call{ID: "call-2", WorkspaceID: "ws-1"})
	if bare.LeadID != "" || bare.AgentID != "" {
		t.Fatalf("a call without lead and agent = %+v", bare)
	}
}

func TestTheProgressOfAListMovesWithEachItemChange(t *testing.T) {
	stamped := called(reservedBy("worker-1", at(time.Minute)))
	callback := callbackRecorded()
	closedItem := called(pendingItem())
	closedItem.State, closedItem.Disposition = StateClosed, "interessado"
	refused := pendingItem()
	refused.State, refused.Disposition = StateClosed, DispositionRefused
	cases := []struct {
		name   string
		before Item
		after  Item
		want   Progress
	}{
		{"nothing changed", pendingItem(), pendingItem(), Progress{}},
		{"the first call", reservedBy("worker-1", at(time.Minute)), stamped, Progress{Called: 1}},
		{"another call to an item already called", stamped, func() Item { i := stamped; i.LastCallID = "call-2"; return i }(), Progress{}},
		{"a callback recorded", stamped, callback, Progress{Callbacks: 1}},
		{"a new call consumes the callback", callback, func() Item { i := callback; i.Disposition = ""; i.LastCallID = "call-2"; return i }(), Progress{Callbacks: -1}},
		{"an outcome closes a called item", stamped, closedItem, Progress{Closed: 1}},
		{"a refusal closes an item never called", pendingItem(), refused, Progress{Closed: 1}},
		{"a refusal closes a waiting callback", callback, func() Item { i := callback; i.State, i.Disposition = StateClosed, DispositionRefused; return i }(), Progress{Closed: 1, Callbacks: -1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ProgressChange(tc.before, tc.after)
			if got != tc.want {
				t.Fatalf("ProgressChange = %+v, want %+v", got, tc.want)
			}
			if got.None() != (tc.want == Progress{}) {
				t.Fatalf("None = %v for %+v", got.None(), got)
			}
		})
	}
}
