package scheduled_message_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	sm "vozko/domain/scheduled_message"
	"vozko/domain/shared"
)

type scheduleFixture struct {
	repo    *fakeRepo
	windows *fakeWindows
	wake    *fakeWake
	clock   *fixedClock
	uc      sm.ScheduleUseCase
}

func newScheduleFixture(t *testing.T) *scheduleFixture {
	t.Helper()
	expires := fixedNow.Add(6 * time.Hour)
	f := &scheduleFixture{
		repo:    newFakeRepo(),
		windows: &fakeWindows{open: true, expiresAt: &expires},
		wake:    &fakeWake{},
		clock:   &fixedClock{now: fixedNow},
	}
	uc, err := NewScheduleUseCase(f.repo, f.windows, f.wake, f.clock)
	if err != nil {
		t.Fatalf("NewScheduleUseCase: %v", err)
	}
	f.uc = uc
	return f
}

func scheduleInput() sm.ScheduleInput {
	return sm.ScheduleInput{
		WorkspaceID:     "ws-1",
		EntryID:         "entry-1",
		EntryType:       string(shared.EntryTypeWhatsApp),
		CreatedByUserID: "user-1",
		Text:            "oi",
		ScheduledAt:     fixedNow.Add(2 * time.Hour),
	}
}

func TestScheduleStoresAndEnqueues(t *testing.T) {
	f := newScheduleFixture(t)

	result, err := f.uc.Execute(context.Background(), scheduleInput())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Message == nil || result.Message.Status != sm.StatusPending {
		t.Fatalf("result = %+v", result)
	}
	if result.AlreadyExisted {
		t.Error("a fresh schedule was reported as a replay")
	}
	// The expiry it was measured against is stored for forensics, and the
	// window travels back so the caller can render the outcome without asking
	// again.
	if result.Message.WindowExpiresAtAtCreation == nil {
		t.Error("the window expiry was not recorded")
	}
	if result.Window.LatestAllowedAt == nil {
		t.Error("the result did not carry the boundary the client needs")
	}
	if f.wake.count() != 1 {
		t.Errorf("fires enqueued = %d, want 1", f.wake.count())
	}
}

// The queue is an optimisation, not the delivery guarantee: the row is durable
// before the enqueue, so a broker outage costs latency and never the message.
func TestScheduleSucceedsWhenTheQueueIsDown(t *testing.T) {
	f := newScheduleFixture(t)
	f.wake.err = errors.New("broker unreachable")

	result, err := f.uc.Execute(context.Background(), scheduleInput())
	if err != nil {
		t.Fatalf("a dead broker must not fail the schedule: %v", err)
	}
	if result.Message.Status != sm.StatusPending {
		t.Errorf("status = %q, want pending so the sweep collects it", result.Message.Status)
	}
	if f.repo.get(result.Message.ID) == nil {
		t.Error("the message was not persisted")
	}
}

// A double-click, a retried timeout, a replayed request: all must produce ONE
// message to the customer.
func TestScheduleIsIdempotentPerKey(t *testing.T) {
	f := newScheduleFixture(t)

	in := scheduleInput()
	in.IdempotencyKey = "key-1"

	first, err := f.uc.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := f.uc.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("second: %v", err)
	}

	if second.Message.ID != first.Message.ID {
		t.Fatalf("a retried create produced a second message: %s vs %s", second.Message.ID, first.Message.ID)
	}
	if !second.AlreadyExisted {
		t.Error("the replay was not reported as one, so the caller would answer 201")
	}
	if f.wake.count() != 1 {
		t.Errorf("fires enqueued = %d, want 1: a replay must not double-schedule", f.wake.count())
	}
}

// Without a key the endpoint is honestly non-idempotent. Two keyless creates
// are two distinct intentions.
func TestScheduleWithoutAKeyCreatesEachTime(t *testing.T) {
	f := newScheduleFixture(t)

	first, _ := f.uc.Execute(context.Background(), scheduleInput())
	second, _ := f.uc.Execute(context.Background(), scheduleInput())

	if first.Message.ID == second.Message.ID {
		t.Fatal("two keyless creates collapsed into one")
	}
}

func TestScheduleRefusesAClosedWindow(t *testing.T) {
	f := newScheduleFixture(t)
	f.windows.set(false, nil)

	result, err := f.uc.Execute(context.Background(), scheduleInput())
	if !errors.Is(err, sm.ErrWindowClosed) {
		t.Fatalf("err = %v, want ErrWindowClosed", err)
	}
	if result == nil || result.Window.Open {
		t.Error("the refusal did not carry the window state the UI must explain")
	}
	if f.wake.count() != 0 {
		t.Error("a refused schedule still enqueued a fire")
	}
}

// The refusal has to name the boundary, or the operator's next attempt is a
// guess.
func TestScheduleRefusesPastTheWindowAndReportsTheBoundary(t *testing.T) {
	f := newScheduleFixture(t)

	in := scheduleInput()
	in.ScheduledAt = fixedNow.Add(8 * time.Hour) // window closes at +6h

	result, err := f.uc.Execute(context.Background(), in)
	if !errors.Is(err, sm.ErrScheduledAtPastWindow) {
		t.Fatalf("err = %v, want ErrScheduledAtPastWindow", err)
	}
	if result.Window.LatestAllowedAt == nil || !result.Window.LatestAllowedAt.Equal(fixedNow.Add(6*time.Hour)) {
		t.Errorf("latest allowed = %v, want the window's expiry", result.Window.LatestAllowedAt)
	}
}

func TestScheduleRefusesTooSoon(t *testing.T) {
	f := newScheduleFixture(t)

	in := scheduleInput()
	in.ScheduledAt = fixedNow.Add(10 * time.Second)

	if _, err := f.uc.Execute(context.Background(), in); !errors.Is(err, sm.ErrScheduledAtTooSoon) {
		t.Fatalf("err = %v, want ErrScheduledAtTooSoon", err)
	}
}

// A channel with no clock — Telegram in bot mode, a healthy linked device — is
// bounded only by the horizon, and must not be told it has a window.
func TestScheduleOnAClocklessChannelIsBoundedByTheHorizon(t *testing.T) {
	f := newScheduleFixture(t)
	f.windows.set(true, nil)

	in := scheduleInput()
	in.ScheduledAt = fixedNow.Add(20 * 24 * time.Hour)

	result, err := f.uc.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("a clockless channel refused a schedule inside the horizon: %v", err)
	}
	if result.Message.WindowExpiresAtAtCreation != nil {
		t.Error("a window expiry was invented for a channel that has none")
	}

	in.ScheduledAt = fixedNow.Add(sm.MaxScheduleHorizon + time.Hour)
	if _, err := f.uc.Execute(context.Background(), in); !errors.Is(err, sm.ErrScheduledAtTooFar) {
		t.Fatalf("err = %v, want ErrScheduledAtTooFar", err)
	}
}

func TestScheduleValidatesContent(t *testing.T) {
	f := newScheduleFixture(t)

	in := scheduleInput()
	in.Text = "   "

	if _, err := f.uc.Execute(context.Background(), in); !errors.Is(err, sm.ErrContentRequired) {
		t.Fatalf("err = %v, want ErrContentRequired", err)
	}
	if f.repo.get("") != nil {
		t.Error("an empty message was persisted")
	}
}

// Media alone is a message. The rule must match the live send path's, or the
// two disagree about what an empty message is.
func TestScheduleAcceptsMediaWithoutText(t *testing.T) {
	f := newScheduleFixture(t)

	in := scheduleInput()
	in.Text = ""
	in.MediaID = "med-1"
	in.MediaType = "image"

	if _, err := f.uc.Execute(context.Background(), in); err != nil {
		t.Fatalf("a media-only schedule was refused: %v", err)
	}
}

func TestNewScheduleUseCaseRefusesMissingDependencies(t *testing.T) {
	repo, windows, wake, clock := newFakeRepo(), &fakeWindows{}, &fakeWake{}, &fixedClock{}

	if _, err := NewScheduleUseCase(nil, windows, wake, clock); err == nil {
		t.Error("a nil repository was accepted")
	}
	if _, err := NewScheduleUseCase(repo, nil, wake, clock); err == nil {
		t.Error("a nil window reader was accepted")
	}
	if _, err := NewScheduleUseCase(repo, windows, nil, clock); err == nil {
		t.Error("a nil wake scheduler was accepted: every schedule would silently be up to a minute late")
	}
	if _, err := NewScheduleUseCase(repo, windows, wake, nil); err == nil {
		t.Error("a nil clock was accepted")
	}
}
