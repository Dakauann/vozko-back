package scheduled_message_usecase

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"vozko/domain/conversation"
	sm "vozko/domain/scheduled_message"
	"vozko/domain/shared"
)

var fixedNow = time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)

type dispatchFixture struct {
	repo        *fakeRepo
	windows     *fakeWindows
	send        *fakeSend
	broadcaster *fakeBroadcaster
	clock       *fixedClock
	uc          sm.DispatchUseCase
}

func newDispatchFixture(t *testing.T) *dispatchFixture {
	t.Helper()
	f := &dispatchFixture{
		repo:        newFakeRepo(),
		windows:     &fakeWindows{open: true},
		send:        &fakeSend{},
		broadcaster: &fakeBroadcaster{},
		clock:       &fixedClock{now: fixedNow},
	}
	uc, err := NewDispatchUseCase(f.repo, f.windows, f.send, f.broadcaster, f.clock)
	if err != nil {
		t.Fatalf("NewDispatchUseCase: %v", err)
	}
	f.uc = uc
	return f
}

func (f *dispatchFixture) pending(id string) *sm.ScheduledMessage {
	return f.repo.put(&sm.ScheduledMessage{
		ID:              id,
		WorkspaceID:     "ws-1",
		EntryID:         "entry-1",
		EntryType:       shared.EntryTypeWhatsApp,
		CreatedByUserID: "user-1",
		Text:            "oi",
		ScheduledAt:     fixedNow,
		Status:          sm.StatusPending,
	})
}

// THE test. Twenty dispatchers race on one message; exactly one send may reach
// the customer. If the claim ever stops being a single conditional write —
// here or in SQL — this is what fails.
func TestConcurrentDispatchSendsExactlyOnce(t *testing.T) {
	f := newDispatchFixture(t)
	f.pending("sched-1")

	const racers = 20
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, racers)

	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = f.uc.Execute(context.Background(), "sched-1")
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("dispatcher %d returned %v; losing the claim is normal, not an error", i, err)
		}
	}
	if got := f.send.count(); got != 1 {
		t.Fatalf("the customer received %d messages, want exactly 1", got)
	}
	if f.repo.claimAttempts != racers {
		t.Errorf("claim attempts = %d, want %d: every dispatcher must try", f.repo.claimAttempts, racers)
	}
	if got := f.repo.get("sched-1").Status; got != sm.StatusSent {
		t.Errorf("status = %q, want sent", got)
	}
}

// A queue redelivery after a successful send must not send again. This is the
// ordinary consequence of at-least-once delivery, not an edge case.
func TestDispatchingAnAlreadySentMessageDoesNothing(t *testing.T) {
	f := newDispatchFixture(t)
	f.pending("sched-1")

	if err := f.uc.Execute(context.Background(), "sched-1"); err != nil {
		t.Fatalf("first dispatch: %v", err)
	}
	if err := f.uc.Execute(context.Background(), "sched-1"); err != nil {
		t.Fatalf("redelivery must be a no-op, got %v", err)
	}
	if got := f.send.count(); got != 1 {
		t.Fatalf("sends = %d, want 1", got)
	}
}

// Cancel racing a fire: whichever conditional write lands first wins, and the
// loser does nothing. There is no interleaving where both succeed.
func TestDispatchingACancelledMessageDoesNothing(t *testing.T) {
	f := newDispatchFixture(t)
	f.pending("sched-1")

	if err := f.repo.Cancel("sched-1"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if err := f.uc.Execute(context.Background(), "sched-1"); err != nil {
		t.Fatalf("dispatching a cancelled message: %v", err)
	}
	if got := f.send.count(); got != 0 {
		t.Fatalf("a cancelled message was sent %d time(s)", got)
	}
	if got := f.repo.get("sched-1").Status; got != sm.StatusCanceled {
		t.Errorf("status = %q, want canceled", got)
	}
}

// A fire for an id that no longer exists is expected — a purge could have
// removed it — and must not error the consumer into a retry loop.
func TestDispatchingAnUnknownMessageDoesNothing(t *testing.T) {
	f := newDispatchFixture(t)
	if err := f.uc.Execute(context.Background(), "does-not-exist"); err != nil {
		t.Fatalf("unknown id: %v", err)
	}
	if f.send.count() != 0 {
		t.Error("an unknown id produced a send")
	}
}

// A window that closed between scheduling and firing is a real outcome — the
// contact can block the bot, a session can die — and must surface as its own
// reason, not as a generic provider failure.
func TestDispatchFailsWithWindowClosedWhenTheWindowShut(t *testing.T) {
	f := newDispatchFixture(t)
	f.pending("sched-1")
	f.windows.set(false, nil)

	if err := f.uc.Execute(context.Background(), "sched-1"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if f.send.count() != 0 {
		t.Error("a message went out on a closed window")
	}

	stored := f.repo.get("sched-1")
	if stored.Status != sm.StatusFailed {
		t.Fatalf("status = %q, want failed", stored.Status)
	}
	if stored.FailureReason == nil || *stored.FailureReason != sm.ReasonWindowClosed {
		t.Errorf("reason = %v, want window_closed", stored.FailureReason)
	}
}

func TestDispatchClassifiesSendErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want sm.FailureReason
	}{
		{"closed window from the send path", conversation.ErrWindowClosed, sm.ReasonWindowClosed},
		{"closed outbound window on an adapter", conversation.ErrOutboundWindowClosed, sm.ReasonWindowClosed},
		{"conversation gone", conversation.ErrConversationNotFound, sm.ReasonEntryUnavailable},
		{"channel not wired", conversation.ErrNoAdapterForEntryType, sm.ReasonEntryUnavailable},
		{"anything else", errors.New("502 from the provider"), sm.ReasonProviderError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newDispatchFixture(t)
			f.pending("sched-1")
			f.send.err = tc.err

			if err := f.uc.Execute(context.Background(), "sched-1"); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			stored := f.repo.get("sched-1")
			if stored.FailureReason == nil || *stored.FailureReason != tc.want {
				t.Errorf("reason = %v, want %v", stored.FailureReason, tc.want)
			}
		})
	}
}

// A provider error is terminal on purpose: we cannot tell a refused send from
// one that reached the customer before the connection dropped, and a duplicate
// is unrecoverable while a visible failure costs one click.
func TestAFailedDispatchIsNeverRetried(t *testing.T) {
	f := newDispatchFixture(t)
	f.pending("sched-1")
	f.send.err = errors.New("502 from the provider")

	_ = f.uc.Execute(context.Background(), "sched-1")

	f.send.err = nil
	if err := f.uc.Execute(context.Background(), "sched-1"); err != nil {
		t.Fatalf("a second attempt must be a no-op, got %v", err)
	}
	if got := f.send.count(); got != 1 {
		t.Fatalf("the provider was called %d times; a failed dispatch must not be retried", got)
	}
	if got := f.repo.get("sched-1").Status; got != sm.StatusFailed {
		t.Errorf("status = %q, want it to stay failed", got)
	}
}

// The message must reach the customer as the OPERATOR who wrote it, carrying
// everything they composed.
func TestDispatchPreservesTheOperatorsMessage(t *testing.T) {
	f := newDispatchFixture(t)
	mediaID, mediaType, replyTo := "med-1", "image", "msg-9"
	f.repo.put(&sm.ScheduledMessage{
		ID:               "sched-1",
		WorkspaceID:      "ws-1",
		EntryID:          "entry-1",
		EntryType:        shared.EntryTypeInstagram,
		CreatedByUserID:  "user-7",
		Text:             "oi",
		MediaID:          &mediaID,
		MediaType:        &mediaType,
		ReplyToMessageID: &replyTo,
		Signed:           true,
		ScheduledAt:      fixedNow,
		Status:           sm.StatusPending,
	})

	if err := f.uc.Execute(context.Background(), "sched-1"); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	in := f.send.first()
	if in.SenderUserID != "user-7" {
		t.Errorf("sender = %q, want the operator who composed it", in.SenderUserID)
	}
	if in.EntryType != string(shared.EntryTypeInstagram) {
		t.Errorf("entry type = %q", in.EntryType)
	}
	if in.Text != "oi" || in.MediaID != "med-1" || in.MediaType != "image" || in.ReplyToMessageID != "msg-9" {
		t.Errorf("input = %+v", in)
	}
	// Signing happens inside the send use case, at delivery, so the operator's
	// CURRENT display name is used rather than a stale copy taken at compose
	// time. The flag has to survive the trip for that to happen.
	if !in.Signed {
		t.Error("the signature flag was lost between scheduling and sending")
	}
}

func TestDispatchMarksSentAndBroadcasts(t *testing.T) {
	f := newDispatchFixture(t)
	f.pending("sched-1")

	if err := f.uc.Execute(context.Background(), "sched-1"); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	stored := f.repo.get("sched-1")
	if stored.Status != sm.StatusSent {
		t.Fatalf("status = %q, want sent", stored.Status)
	}
	if stored.SentMessageID == nil || *stored.SentMessageID != "msg-1" {
		t.Errorf("sent message id = %v, want the delivered message's id", stored.SentMessageID)
	}
	if stored.SentAt == nil {
		t.Error("sent_at was not stamped")
	}
	if f.broadcaster.messages != 1 {
		t.Errorf("broadcasts = %d, want the delivered message to reach open inboxes", f.broadcaster.messages)
	}
}

// The sweep claims in batches, so it hands the dispatcher rows that have
// already left pending. Re-claiming them would fail and strand the batch.
func TestDispatchClaimedSkipsTheClaim(t *testing.T) {
	f := newDispatchFixture(t)
	message := f.pending("sched-1")
	claimed, err := f.repo.ClaimForDispatch(message.ID, fixedNow)
	if err != nil || claimed == nil {
		t.Fatalf("setup claim failed: %v", err)
	}
	before := f.repo.claimAttempts

	if err := f.uc.DispatchClaimed(context.Background(), claimed); err != nil {
		t.Fatalf("DispatchClaimed: %v", err)
	}
	if f.repo.claimAttempts != before {
		t.Errorf("DispatchClaimed re-claimed the row")
	}
	if f.send.count() != 1 {
		t.Errorf("sends = %d, want 1", f.send.count())
	}
}

func TestNewDispatchUseCaseRefusesMissingDependencies(t *testing.T) {
	repo, windows, send, broadcaster, clock := newFakeRepo(), &fakeWindows{}, &fakeSend{}, &fakeBroadcaster{}, &fixedClock{}

	if _, err := NewDispatchUseCase(nil, windows, send, broadcaster, clock); err == nil {
		t.Error("a nil repository was accepted")
	}
	if _, err := NewDispatchUseCase(repo, nil, send, broadcaster, clock); err == nil {
		t.Error("a nil window reader was accepted")
	}
	if _, err := NewDispatchUseCase(repo, windows, nil, broadcaster, clock); err == nil {
		t.Error("a nil send use case was accepted")
	}
	if _, err := NewDispatchUseCase(repo, windows, send, nil, clock); err == nil {
		t.Error("a nil broadcaster was accepted")
	}
	if _, err := NewDispatchUseCase(repo, windows, send, broadcaster, nil); err == nil {
		t.Error("a nil clock was accepted")
	}
}
