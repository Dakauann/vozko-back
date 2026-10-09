package calllist_usecase

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"vozko/domain/calls/calllist"
	"vozko/domain/callsession"
)

func stampOf(user, itemID, callID string) callsession.CallListItemStamp {
	return callsession.CallListItemStamp{WorkspaceID: ws, UserID: user, ItemID: itemID, CallRecordID: callID}
}

func openItems(t *testing.T, h *harness) {
	t.Helper()
	for id, item := range h.store.items {
		if item.State != calllist.StatePending || item.ReservedBy != "" || item.Disposition != "" {
			t.Fatalf("item %s = %+v, want it pending and untouched", id, item)
		}
	}
	if h.store.lists["list-1"].ClosedCount != 0 {
		t.Fatalf("closed count = %d", h.store.lists["list-1"].ClosedCount)
	}
}

func TestNextSaysWhenItStoppedBeforeTheQueueEnded(t *testing.T) {
	h := newHarness()
	h.store.lists["list-1"] = activeList("list-1", worker)
	for n := 1; n <= maxRefusedPerNext+1; n++ {
		id := fmt.Sprintf("lead-%02d", n)
		blocked := callable(id, fmt.Sprintf("55119876500%02d", n))
		blocked.Blocked = true
		h.leads.leads[id] = blocked
		item := pendingItem(fmt.Sprintf("item-%02d", n), "list-1", id, n)
		item.Phone = blocked.Number
		h.store.put(item)
	}
	svc := h.service()
	got, err := svc.Next(context.Background(), actor(worker), "list-1")
	if err != nil || got.Item != nil || got.Refused != maxRefusedPerNext || !got.More {
		t.Fatalf("Next = %+v, %v, want it to stop at the refusal cap and ask for more", got, err)
	}
	got, err = svc.Next(context.Background(), actor(worker), "list-1")
	if err != nil || got.Item != nil || got.Refused != 1 || got.More {
		t.Fatalf("the second Next = %+v, %v, want the queue empty", got, err)
	}
}

func TestOnlyTheDialerStampsAnOpenItemAndTheRefusalIsNotRetryable(t *testing.T) {
	h, svc := reservedHarness(t)
	err := svc.StampLastCall(context.Background(), stampOf(colleague, "item-1", "call-9"))
	if !errors.Is(err, callsession.ErrCallListStampRefused) || !errors.Is(err, calllist.ErrItemTaken) {
		t.Fatalf("a colleague stamping = %v", err)
	}
	if item, _ := h.store.Item(context.Background(), ws, "item-1"); item.LastCallID != "" {
		t.Fatalf("a refused stamp landed: %+v", item)
	}
	h.store.items["item-1"].State = calllist.StateClosed
	if err := svc.StampLastCall(context.Background(), stampOf(worker, "item-1", "call-9")); !errors.Is(err, calllist.ErrItemClosed) ||
		!errors.Is(err, callsession.ErrCallListStampRefused) {
		t.Fatalf("stamping a closed item = %v", err)
	}
	if err := svc.StampLastCall(context.Background(), stampOf(worker, "item-9", "call-9")); !errors.Is(err, calllist.ErrItemNotFound) {
		t.Fatalf("stamping a missing item = %v", err)
	}
}

func TestCreatingAListChecksEachMemberOnce(t *testing.T) {
	h := newHarness()
	seedSnapshot(h, "list-1", callable("lead-a", "5511987650001"))
	h.store.claimErr = calllist.ErrBuildClaimLost
	svc := h.service()
	prepared, err := svc.Prepare(context.Background(), actor(manager), draft())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateFromSnapshot(context.Background(), actor(manager), prepared, 1); err != nil {
		t.Fatal(err)
	}
	entries := len(workRequirements(t))
	if h.counted.calls[worker] != entries || h.counted.calls[colleague] != entries {
		t.Fatalf("permission lookups = %v, want %d per member", h.counted.calls, entries)
	}
}

func TestCreatingAListNeedsADraftPreparedForTheSameActor(t *testing.T) {
	h := newHarness()
	svc := h.service()
	if _, err := svc.CreateFromSnapshot(context.Background(), actor(manager), Prepared{}, 1); !errors.Is(err, calllist.ErrForbidden) {
		t.Fatalf("an unprepared draft = %v", err)
	}
	prepared, err := svc.Prepare(context.Background(), actor(manager), draft())
	if err != nil {
		t.Fatal(err)
	}
	h.grants[colleague] = managerGrants()
	if _, err := svc.CreateFromSnapshot(context.Background(), actor(colleague), prepared, 1); !errors.Is(err, calllist.ErrForbidden) {
		t.Fatalf("a draft prepared by someone else = %v", err)
	}
	if len(h.store.lists) != 0 {
		t.Fatal("a refused create stored a list")
	}
}

func TestReassigningChecksOnlyTheNewMembers(t *testing.T) {
	h := workingHarness()
	h.grants["worker-3"] = workerGrants()
	members := []string{worker, colleague, "worker-3"}
	if _, err := h.service().Update(context.Background(), actor(manager), "list-1", calllist.Change{AssigneeIDs: &members}); err != nil {
		t.Fatal(err)
	}
	if h.counted.calls[worker] != 0 || h.counted.calls[colleague] != 0 || h.counted.calls["worker-3"] == 0 {
		t.Fatalf("permission lookups = %v, want only the new member checked", h.counted.calls)
	}
}

func TestClosingAfterTheListIsArchivedIsRefused(t *testing.T) {
	h, svc := reservedHarness(t)
	h.calls["call-1"] = callOf("call-1", worker, "lead-a")
	if err := svc.StampLastCall(context.Background(), stampOf(worker, "item-1", "call-1")); err != nil {
		t.Fatal(err)
	}
	h.store.lists["list-1"].Status = calllist.StatusArchived
	if _, err := svc.Close(context.Background(), actor(worker), "item-1", calllist.Closing{Disposition: "interessado"}); !errors.Is(err, calllist.ErrListNotActive) {
		t.Fatalf("Close = %v, want ErrListNotActive", err)
	}
}

func TestACallbackCannotBeClosedAgainWithTheCallThatAskedForIt(t *testing.T) {
	h, svc := reservedHarness(t)
	h.calls["call-1"] = callOf("call-1", worker, "lead-a")
	if err := svc.StampLastCall(context.Background(), stampOf(worker, "item-1", "call-1")); err != nil {
		t.Fatal(err)
	}
	when := now.Add(time.Hour)
	if _, err := svc.Close(context.Background(), actor(worker), "item-1", calllist.Closing{Disposition: calllist.DispositionCallback, CallbackAt: &when}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Close(context.Background(), actor(worker), "item-1", calllist.Closing{Disposition: "interessado"}); !errors.Is(err, calllist.ErrItemNotCalled) {
		t.Fatalf("closing the callback again = %v, want ErrItemNotCalled", err)
	}
}
