package scheduled_message_usecase

import (
	"context"
	"testing"
	"time"

	sm "vozko/domain/scheduled_message"
	"vozko/domain/shared"
)

type sweepFixture struct {
	repo     *fakeRepo
	dispatch *dispatchFixture
	clock    *fixedClock
	sweep    sm.SweepJob
}

func newSweepFixture(t *testing.T) *sweepFixture {
	t.Helper()
	d := newDispatchFixture(t)
	f := &sweepFixture{repo: d.repo, dispatch: d, clock: d.clock}

	sweep, err := NewSweepJob(d.repo, d.uc, d.clock)
	if err != nil {
		t.Fatalf("NewSweepJob: %v", err)
	}
	f.sweep = sweep
	return f
}

func (f *sweepFixture) message(id string, status sm.Status, scheduledAt time.Time, claimedAt *time.Time) *sm.ScheduledMessage {
	return f.repo.put(&sm.ScheduledMessage{
		ID:              id,
		WorkspaceID:     "ws-1",
		EntryID:         "entry-1",
		EntryType:       shared.EntryTypeWhatsApp,
		CreatedByUserID: "user-1",
		Text:            "oi",
		ScheduledAt:     scheduledAt,
		Status:          status,
		ClaimedAt:       claimedAt,
	})
}

// The sweep is what makes the feature correct without a broker. This is the
// case the queue would normally cover.
func TestSweepDeliversDueMessages(t *testing.T) {
	f := newSweepFixture(t)
	f.message("due", sm.StatusPending, fixedNow.Add(-time.Minute), nil)
	f.message("not-yet", sm.StatusPending, fixedNow.Add(time.Hour), nil)

	if err := f.sweep.Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got := f.dispatch.send.count(); got != 1 {
		t.Fatalf("sends = %d, want only the due message", got)
	}
	if got := f.repo.get("due").Status; got != sm.StatusSent {
		t.Errorf("due status = %q, want sent", got)
	}
	if got := f.repo.get("not-yet").Status; got != sm.StatusPending {
		t.Errorf("a future message was dispatched early (status %q)", got)
	}
}

// The sweep overlapping the queue consumer is the NORMAL steady state, not an
// edge case: both are meant to be running.
func TestSweepAndQueueTogetherSendOnce(t *testing.T) {
	f := newSweepFixture(t)
	f.message("due", sm.StatusPending, fixedNow.Add(-time.Minute), nil)

	if err := f.dispatch.uc.Execute(context.Background(), "due"); err != nil {
		t.Fatalf("queue dispatch: %v", err)
	}
	if err := f.sweep.Execute(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	if got := f.dispatch.send.count(); got != 1 {
		t.Fatalf("the customer received %d messages, want exactly 1", got)
	}
}

// A dispatcher that died mid-send leaves the row in `sending` forever. It is
// retired with its OWN reason, never retried: we cannot tell a send that never
// left from one that arrived.
func TestSweepRetiresStuckClaimsWithoutResending(t *testing.T) {
	f := newSweepFixture(t)
	claimed := fixedNow.Add(-10 * time.Minute)
	f.message("stuck", sm.StatusSending, fixedNow.Add(-time.Hour), &claimed)

	if err := f.sweep.Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got := f.dispatch.send.count(); got != 0 {
		t.Fatalf("a stuck claim was re-sent %d time(s); a duplicate to the customer is unrecoverable", got)
	}
	stored := f.repo.get("stuck")
	if stored.Status != sm.StatusFailed {
		t.Fatalf("status = %q, want failed", stored.Status)
	}
	if stored.FailureReason == nil || *stored.FailureReason != sm.ReasonDispatchInterrupted {
		t.Errorf("reason = %v, want dispatch_interrupted so the UI can say delivery is unconfirmed", stored.FailureReason)
	}
}

// A claim younger than the lease belongs to a dispatcher that is still working.
// Retiring it would report a message as failed while it is on its way.
func TestSweepLeavesFreshClaimsAlone(t *testing.T) {
	f := newSweepFixture(t)
	claimed := fixedNow.Add(-30 * time.Second)
	f.message("in-flight", sm.StatusSending, fixedNow.Add(-time.Minute), &claimed)

	if err := f.sweep.Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := f.repo.get("in-flight").Status; got != sm.StatusSending {
		t.Errorf("status = %q, want a live dispatch left alone", got)
	}
}

// A long-overdue message that CAN still go out should go out. There is no
// separate "expiry" pass precisely because this one already covers it: the due
// pass claims every pending row whose time has passed, however long ago.
func TestSweepStillDeliversALongOverdueMessage(t *testing.T) {
	f := newSweepFixture(t)
	f.message("ancient", sm.StatusPending, fixedNow.Add(-48*time.Hour), nil)

	if err := f.sweep.Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := f.repo.get("ancient").Status; got != sm.StatusSent {
		t.Errorf("status = %q, want the overdue message delivered", got)
	}
}

// And when it cannot go out, the same pass retires it with a reason. Either
// way no row is left looking imminent forever.
func TestSweepRetiresALongOverdueMessageItCannotDeliver(t *testing.T) {
	f := newSweepFixture(t)
	f.message("ancient", sm.StatusPending, fixedNow.Add(-48*time.Hour), nil)
	f.dispatch.windows.set(false, nil)

	if err := f.sweep.Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	stored := f.repo.get("ancient")
	if stored.Status != sm.StatusFailed {
		t.Fatalf("status = %q, want failed", stored.Status)
	}
	if stored.FailureReason == nil || *stored.FailureReason != sm.ReasonWindowClosed {
		t.Errorf("reason = %v, want the specific truth (window_closed), not a generic expiry", stored.FailureReason)
	}
}

// One bad message must not strand the rest of a claimed batch: they have all
// left pending already and would otherwise sit until the stuck pass retires them.
func TestSweepContinuesAfterOneFailure(t *testing.T) {
	f := newSweepFixture(t)
	f.message("a", sm.StatusPending, fixedNow.Add(-time.Minute), nil)
	f.message("b", sm.StatusPending, fixedNow.Add(-time.Minute), nil)
	f.message("c", sm.StatusPending, fixedNow.Add(-time.Minute), nil)

	if err := f.sweep.Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, id := range []string{"a", "b", "c"} {
		if got := f.repo.get(id).Status; got == sm.StatusSending {
			t.Errorf("%s was left claimed and unfinished", id)
		}
	}
}

func TestNewSweepJobRefusesMissingDependencies(t *testing.T) {
	d := newDispatchFixture(t)

	if _, err := NewSweepJob(nil, d.uc, d.clock); err == nil {
		t.Error("a nil repository was accepted")
	}
	if _, err := NewSweepJob(d.repo, nil, d.clock); err == nil {
		t.Error("a nil dispatcher was accepted")
	}
	if _, err := NewSweepJob(d.repo, d.uc, nil); err == nil {
		t.Error("a nil clock was accepted")
	}
}

func TestPurgeRemovesOnlyTerminalMessages(t *testing.T) {
	repo := newFakeRepo()
	clock := &fixedClock{now: fixedNow}
	job, err := NewPurgeJob(repo, clock)
	if err != nil {
		t.Fatalf("NewPurgeJob: %v", err)
	}

	old := fixedNow.Add(-terminalRetention - time.Hour)
	repo.put(&sm.ScheduledMessage{ID: "old-sent", Status: sm.StatusSent, UpdatedAt: old})
	repo.put(&sm.ScheduledMessage{ID: "old-pending", Status: sm.StatusPending, UpdatedAt: old})
	repo.put(&sm.ScheduledMessage{ID: "recent-sent", Status: sm.StatusSent, UpdatedAt: fixedNow})

	if err := job.Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if repo.get("old-sent") != nil {
		t.Error("an old delivered message survived the purge")
	}
	if repo.get("recent-sent") == nil {
		t.Error("a recent message was purged")
	}
	// An undelivered message is not litter, however old.
	if repo.get("old-pending") == nil {
		t.Error("a PENDING message was purged: the customer would never receive it and nobody would know")
	}
}
