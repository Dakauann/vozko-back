package calllist_usecase

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"vozko/domain/calls/calllist"
)

func calledHarness(t *testing.T) (*harness, *Service) {
	t.Helper()
	h, svc := reservedHarness(t)
	h.calls["call-1"] = callOf("call-1", worker, "lead-a")
	if err := svc.StampLastCall(context.Background(), stampOf(worker, "item-1", "call-1")); err != nil {
		t.Fatal(err)
	}
	return h, svc
}

func TestNextTellsTheMemberThatTheContactSheAlreadyCalledCanBeClosedAfterAReload(t *testing.T) {
	_, svc := calledHarness(t)
	again, err := svc.Next(context.Background(), actor(worker), "list-1")
	if err != nil {
		t.Fatal(err)
	}
	if again.Item == nil || again.Item.ID != "item-1" || !again.Closable {
		t.Fatalf("the held contact after a reload = %+v closable %v", again.Item, again.Closable)
	}
}

func TestAFreshContactFromNextIsNotClosableBeforeTheCall(t *testing.T) {
	h := workingHarness()
	got, err := h.service().Next(context.Background(), actor(worker), "list-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Item == nil || got.Closable {
		t.Fatalf("a contact never called = %+v closable %v", got.Item, got.Closable)
	}
}

func TestNextKeepsServingWhenTheStampedCallCannotBeReadButSaysItIsNotClosable(t *testing.T) {
	h, svc := calledHarness(t)
	delete(h.calls, "call-1")
	got, err := svc.Next(context.Background(), actor(worker), "list-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Item == nil || got.Item.ID != "item-1" || got.Closable {
		t.Fatalf("a contact whose call is gone = %+v closable %v", got.Item, got.Closable)
	}
}

func TestEachItemRowTellsTheViewerWhetherSheCanCloseIt(t *testing.T) {
	h, _ := calledHarness(t)
	h.store.items["item-2"].LastCallID = "call-2"
	h.calls["call-2"] = callOf("call-2", colleague, "lead-b")
	cases := []struct {
		viewer string
		want   map[string]bool
	}{
		{worker, map[string]bool{"item-1": true, "item-2": false}},
		{colleague, map[string]bool{"item-1": false, "item-2": true}},
	}
	for _, tc := range cases {
		t.Run(tc.viewer, func(t *testing.T) {
			rows, err := h.service().Items(context.Background(), actor(tc.viewer), calllist.ItemQuery{ListID: "list-1"})
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]bool{}
			for _, row := range rows.Items {
				got[row.ID] = row.Closable
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("closable = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAnItemOfAnArchivedListIsNeverClosable(t *testing.T) {
	h, _ := calledHarness(t)
	h.store.lists["list-1"].Status = calllist.StatusArchived
	rows, err := h.service().Items(context.Background(), actor(worker), calllist.ItemQuery{ListID: "list-1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows.Items {
		if row.Closable {
			t.Fatalf("item %s of an archived list is closable", row.ID)
		}
	}
}

func TestReleasingAContactAlreadyCalledStillLetsTheCallerCloseIt(t *testing.T) {
	_, svc := calledHarness(t)
	released, err := svc.Release(context.Background(), actor(worker), "item-1")
	if err != nil {
		t.Fatal(err)
	}
	if released.Item.State != calllist.StatePending || !released.Closable {
		t.Fatalf("released = %+v closable %v", released.Item, released.Closable)
	}
}

func TestAClosedOrCallbackItemIsNotClosableAgain(t *testing.T) {
	_, svc := calledHarness(t)
	when := now.Add(2 * time.Hour)
	callback, err := svc.Close(context.Background(), actor(worker), "item-1", calllist.Closing{Disposition: calllist.DispositionCallback, CallbackAt: &when})
	if err != nil {
		t.Fatal(err)
	}
	if callback.Item.State != calllist.StatePending || callback.Closable {
		t.Fatalf("callback = %+v closable %v", callback.Item, callback.Closable)
	}

	_, other := calledHarness(t)
	closed, err := other.Close(context.Background(), actor(worker), "item-1", calllist.Closing{Disposition: "interessado"})
	if err != nil {
		t.Fatal(err)
	}
	if closed.Item.State != calllist.StateClosed || closed.Closable {
		t.Fatalf("closed = %+v closable %v", closed.Item, closed.Closable)
	}
}

func TestListsCarryTheirVerdicts(t *testing.T) {
	h := workingHarness()
	svc := h.service()
	want := calllist.ListVerdict{AcceptsOutcomes: true, StatusMoves: []calllist.Status{calllist.StatusPaused, calllist.StatusArchived}}

	page, err := svc.Lists(context.Background(), actor(manager), calllist.ListQuery{})
	if err != nil || len(page.Lists) != 1 || !reflect.DeepEqual(page.Lists[0].Verdict, want) {
		t.Fatalf("lists = %+v, %v", page, err)
	}
	one, err := svc.View(context.Background(), actor(manager), "list-1")
	if err != nil || !reflect.DeepEqual(one.Verdict, want) {
		t.Fatalf("get = %+v, %v", one, err)
	}
	asWorker := want
	asWorker.StatusMoves = []calllist.Status{}
	worked, err := svc.View(context.Background(), actor(worker), "list-1")
	if err != nil || !reflect.DeepEqual(worked.Verdict, asWorker) {
		t.Fatalf("get as a member who does not manage lists = %+v, %v", worked, err)
	}
	assigned, err := svc.Lists(context.Background(), actor(worker), calllist.ListQuery{})
	if err != nil || len(assigned.Lists) != 1 || !reflect.DeepEqual(assigned.Lists[0].Verdict, asWorker) {
		t.Fatalf("lists as a member who does not manage lists = %+v, %v", assigned, err)
	}
	paused := calllist.StatusPaused
	moved, err := svc.Update(context.Background(), actor(manager), "list-1", calllist.Change{Status: &paused})
	wantPaused := calllist.ListVerdict{AcceptsOutcomes: true, StatusMoves: []calllist.Status{calllist.StatusActive, calllist.StatusArchived}}
	if err != nil || !reflect.DeepEqual(moved.Verdict, wantPaused) {
		t.Fatalf("paused = %+v, %v", moved, err)
	}
}

func TestNextCarriesTheListVerdict(t *testing.T) {
	h := workingHarness()
	got, err := h.service().Next(context.Background(), actor(worker), "list-1")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Verdict.AcceptsOutcomes || got.Verdict.StatusMoves == nil || len(got.Verdict.StatusMoves) != 0 {
		t.Fatalf("verdict for a member who does not manage lists = %+v", got.Verdict)
	}

	h.grants[worker]["call_lists:manage"] = true
	h.grants[worker]["leads:read"] = true
	managed, err := h.service().Next(context.Background(), actor(worker), "list-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(managed.Verdict.StatusMoves) != 2 {
		t.Fatalf("verdict for a member who manages lists = %+v", managed.Verdict)
	}
}

func TestAViewerWhoCannotWorkCallListsNeverSeesAClosableRow(t *testing.T) {
	h, _ := calledHarness(t)
	h.grants[worker] = map[string]bool{"call_lists:read": true}
	rows, err := h.service().Items(context.Background(), actor(worker), calllist.ItemQuery{ListID: "list-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows.Items) == 0 {
		t.Fatal("no rows")
	}
	for _, row := range rows.Items {
		if row.Closable {
			t.Fatalf("item %s is closable for a member who lost the dialer", row.ID)
		}
	}
}

func TestThePendingTabIsCutAtTheInstantOfItsFirstPage(t *testing.T) {
	h := workingHarness()
	svc := h.service()
	first, err := svc.Items(context.Background(), actor(worker), calllist.ItemQuery{ListID: "list-1", State: calllist.StatePending})
	if err != nil {
		t.Fatal(err)
	}
	if asked := h.store.itemQueries[len(h.store.itemQueries)-1]; !asked.AsOf.Equal(now) {
		t.Fatalf("the first page was cut at %v, want %v", asked.AsOf, now)
	}
	if first.AsOf == nil || !first.AsOf.Equal(now) {
		t.Fatalf("the first page answers asOf %v", first.AsOf)
	}

	h.clock = now.Add(time.Hour)
	pinned := now.Add(-time.Minute)
	if _, err := svc.Items(context.Background(), actor(worker), calllist.ItemQuery{ListID: "list-1", State: calllist.StatePending, AfterPosition: 1, AsOf: pinned}); err != nil {
		t.Fatal(err)
	}
	if asked := h.store.itemQueries[len(h.store.itemQueries)-1]; !asked.AsOf.Equal(pinned) {
		t.Fatalf("a later page was cut at %v, want the pinned %v", asked.AsOf, pinned)
	}

	reads := len(h.store.itemQueries)
	if _, err := svc.Items(context.Background(), actor(worker), calllist.ItemQuery{ListID: "list-1", State: calllist.StatePending, AfterPosition: 1}); !errors.Is(err, calllist.ErrItemCursorInvalid) {
		t.Fatalf("a later pending page without asOf = %v", err)
	}
	if len(h.store.itemQueries) != reads {
		t.Fatal("a refused page reached the store")
	}

	if _, err := svc.Items(context.Background(), actor(worker), calllist.ItemQuery{ListID: "list-1", State: calllist.StateClosed, AfterPosition: 1}); err != nil {
		t.Fatalf("a later closed page needs no asOf: %v", err)
	}
}

func TestACreatedListCarriesItsVerdict(t *testing.T) {
	h := newHarness()
	h.store.lists["list-1"] = activeList("list-1", worker)
	svc := h.service()
	prepared, err := svc.Prepare(context.Background(), actor(manager), draft())
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.CreateFromSnapshot(context.Background(), actor(manager), prepared, 2)
	if err != nil {
		t.Fatal(err)
	}
	want := calllist.ListVerdict{AcceptsOutcomes: true, StatusMoves: []calllist.Status{calllist.StatusPaused, calllist.StatusArchived}}
	if got.List == nil || got.ID != "list-1" || !reflect.DeepEqual(got.Verdict, want) {
		t.Fatalf("created = %+v, want the verdict %+v", got, want)
	}
}
