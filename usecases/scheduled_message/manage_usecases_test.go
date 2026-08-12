package scheduled_message_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	sm "vozko/domain/scheduled_message"
	"vozko/domain/shared"
)

type manageFixture struct {
	repo       *fakeRepo
	windows    *fakeWindows
	wake       *fakeWake
	clock      *fixedClock
	cancel     sm.CancelUseCase
	reschedule sm.RescheduleUseCase
	list       sm.ListUseCase
}

func newManageFixture(t *testing.T) *manageFixture {
	t.Helper()
	expires := fixedNow.Add(6 * time.Hour)
	f := &manageFixture{
		repo:    newFakeRepo(),
		windows: &fakeWindows{open: true, expiresAt: &expires},
		wake:    &fakeWake{},
		clock:   &fixedClock{now: fixedNow},
	}

	var err error
	if f.cancel, err = NewCancelUseCase(f.repo); err != nil {
		t.Fatalf("NewCancelUseCase: %v", err)
	}
	if f.reschedule, err = NewRescheduleUseCase(f.repo, f.windows, f.wake, f.clock); err != nil {
		t.Fatalf("NewRescheduleUseCase: %v", err)
	}
	if f.list, err = NewListUseCase(f.repo, f.windows, f.clock); err != nil {
		t.Fatalf("NewListUseCase: %v", err)
	}
	return f
}

func (f *manageFixture) pending(id, workspaceID string) *sm.ScheduledMessage {
	return f.repo.put(&sm.ScheduledMessage{
		ID:              id,
		WorkspaceID:     workspaceID,
		EntryID:         "entry-1",
		EntryType:       shared.EntryTypeWhatsApp,
		CreatedByUserID: "user-1",
		Text:            "oi",
		ScheduledAt:     fixedNow.Add(2 * time.Hour),
		Status:          sm.StatusPending,
	})
}

func TestCancelPendingMessage(t *testing.T) {
	f := newManageFixture(t)
	f.pending("sched-1", "ws-1")

	if err := f.cancel.Execute(context.Background(), "ws-1", "sched-1"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := f.repo.get("sched-1").Status; got != sm.StatusCanceled {
		t.Errorf("status = %q, want canceled", got)
	}
}

// Cancelling something already sent must not succeed quietly: the operator
// needs to know the customer has it.
func TestCancelRefusesATerminalMessage(t *testing.T) {
	f := newManageFixture(t)
	m := f.pending("sched-1", "ws-1")
	m.Status = sm.StatusSent

	if err := f.cancel.Execute(context.Background(), "ws-1", "sched-1"); !errors.Is(err, sm.ErrNotPending) {
		t.Fatalf("err = %v, want ErrNotPending", err)
	}
}

// Tenant isolation. Answering ErrNotFound rather than a distinct "forbidden"
// avoids confirming that an id the caller cannot see exists.
func TestCancelRefusesAnotherWorkspacesMessage(t *testing.T) {
	f := newManageFixture(t)
	f.pending("sched-1", "ws-other")

	if err := f.cancel.Execute(context.Background(), "ws-1", "sched-1"); !errors.Is(err, sm.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if got := f.repo.get("sched-1").Status; got != sm.StatusPending {
		t.Errorf("another workspace's message was modified")
	}
}

func TestRescheduleMovesTheTimeAndReEnqueues(t *testing.T) {
	f := newManageFixture(t)
	f.pending("sched-1", "ws-1")

	newTime := fixedNow.Add(4 * time.Hour)
	result, err := f.reschedule.Execute(context.Background(), sm.RescheduleInput{
		ID: "sched-1", WorkspaceID: "ws-1", ScheduledAt: newTime,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.Message.ScheduledAt.Equal(newTime) {
		t.Errorf("scheduled at = %v, want %v", result.Message.ScheduledAt, newTime)
	}
	if !f.repo.get("sched-1").ScheduledAt.Equal(newTime) {
		t.Error("the new time was not persisted")
	}
	// A stale fire from the original time is harmless: the claim admits one
	// caller and the row is not yet due.
	if f.wake.count() != 1 {
		t.Errorf("fires enqueued = %d, want 1", f.wake.count())
	}
}

func TestRescheduleValidatesAgainstTheLiveWindow(t *testing.T) {
	f := newManageFixture(t)
	f.pending("sched-1", "ws-1")

	result, err := f.reschedule.Execute(context.Background(), sm.RescheduleInput{
		ID: "sched-1", WorkspaceID: "ws-1", ScheduledAt: fixedNow.Add(8 * time.Hour),
	})
	if !errors.Is(err, sm.ErrScheduledAtPastWindow) {
		t.Fatalf("err = %v, want ErrScheduledAtPastWindow", err)
	}
	if result.Window.LatestAllowedAt == nil {
		t.Error("the refusal did not name the boundary")
	}
	if f.wake.count() != 0 {
		t.Error("a refused reschedule still enqueued a fire")
	}
}

// The window can only have grown since creation, so a reschedule validated
// against the LIVE window is never stricter — and it is what lets an operator
// push a message further out after the customer writes again.
func TestRescheduleUsesTheWidenedWindow(t *testing.T) {
	f := newManageFixture(t)
	f.pending("sched-1", "ws-1")

	widened := fixedNow.Add(30 * time.Hour)
	f.windows.set(true, &widened)

	newTime := fixedNow.Add(28 * time.Hour)
	if _, err := f.reschedule.Execute(context.Background(), sm.RescheduleInput{
		ID: "sched-1", WorkspaceID: "ws-1", ScheduledAt: newTime,
	}); err != nil {
		t.Fatalf("a reschedule inside the widened window was refused: %v", err)
	}
	stored := f.repo.get("sched-1")
	if stored.WindowExpiresAtAtCreation == nil || !stored.WindowExpiresAtAtCreation.Equal(widened) {
		t.Errorf("the recorded expiry was not refreshed: %v", stored.WindowExpiresAtAtCreation)
	}
}

func TestRescheduleRefusesATerminalMessage(t *testing.T) {
	f := newManageFixture(t)
	m := f.pending("sched-1", "ws-1")
	m.Status = sm.StatusSent

	_, err := f.reschedule.Execute(context.Background(), sm.RescheduleInput{
		ID: "sched-1", WorkspaceID: "ws-1", ScheduledAt: fixedNow.Add(3 * time.Hour),
	})
	if !errors.Is(err, sm.ErrNotPending) {
		t.Fatalf("err = %v, want ErrNotPending", err)
	}
}

// The composer needs the window with the list, or it makes two requests that
// can disagree by the width of a round trip.
func TestListForEntryCarriesTheLiveWindow(t *testing.T) {
	f := newManageFixture(t)
	f.pending("sched-1", "ws-1")

	result, err := f.list.ForEntry(context.Background(), "entry-1", string(shared.EntryTypeWhatsApp), nil)
	if err != nil {
		t.Fatalf("ForEntry: %v", err)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(result.Messages))
	}
	if !result.Window.Open || result.Window.LatestAllowedAt == nil {
		t.Errorf("window = %+v, want the live state the composer needs", result.Window)
	}
}

func TestListForEntryFiltersByStatus(t *testing.T) {
	f := newManageFixture(t)
	f.pending("sched-1", "ws-1")
	sent := f.pending("sched-2", "ws-1")
	sent.Status = sm.StatusSent

	result, err := f.list.ForEntry(context.Background(), "entry-1", string(shared.EntryTypeWhatsApp),
		[]sm.Status{sm.StatusPending})
	if err != nil {
		t.Fatalf("ForEntry: %v", err)
	}
	if len(result.Messages) != 1 || result.Messages[0].ID != "sched-1" {
		t.Fatalf("messages = %+v, want only the pending one", result.Messages)
	}
}
