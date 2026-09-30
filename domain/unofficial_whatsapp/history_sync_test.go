package unofficial_whatsapp

import (
	"errors"
	"testing"
	"time"
)

var syncEpoch = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

func connectedInstance() *Instance {
	return &Instance{ID: "inst-1", WorkspaceID: "ws-1", Status: StatusConnected}
}

func newTestSync(t *testing.T) *HistorySync {
	t.Helper()
	run, err := NewHistorySync(connectedInstance(), HistorySyncTriggerConnect, syncEpoch, DefaultHistorySyncPolicy())
	if err != nil {
		t.Fatalf("new history sync: %v", err)
	}
	return run
}

func TestNewHistorySyncStartsQueuedAndDueNow(t *testing.T) {
	policy := DefaultHistorySyncPolicy()
	run := newTestSync(t)

	if run.Status != HistorySyncQueued {
		t.Errorf("status = %s, want QUEUED", run.Status)
	}
	if !run.NextPollAt.Equal(syncEpoch) {
		t.Errorf("next poll = %v, want now so the first pass runs immediately", run.NextPollAt)
	}
	if want := syncEpoch.Add(-policy.Window); !run.WindowFrom.Equal(want) {
		t.Errorf("window from = %v, want %v", run.WindowFrom, want)
	}
	if want := syncEpoch.Add(policy.PollWindow); !run.PollUntil.Equal(want) {
		t.Errorf("poll until = %v, want %v", run.PollUntil, want)
	}
	if run.WorkspaceID != "ws-1" || run.InstanceID != "inst-1" {
		t.Errorf("run is not scoped to its instance: %+v", run)
	}
}

func TestNewHistorySyncRefusesAnUnscopedInstance(t *testing.T) {
	_, err := NewHistorySync(&Instance{ID: "inst-1"}, HistorySyncTriggerConnect, syncEpoch, DefaultHistorySyncPolicy())
	if !errors.Is(err, ErrWorkspaceIDRequired) {
		t.Errorf("err = %v, want ErrWorkspaceIDRequired", err)
	}
}

func TestNewHistorySyncRefusesAnUnknownTrigger(t *testing.T) {
	_, err := NewHistorySync(connectedInstance(), HistorySyncTrigger("NIGHTLY"), syncEpoch, DefaultHistorySyncPolicy())
	if !errors.Is(err, ErrHistorySyncTrigger) {
		t.Errorf("err = %v, want ErrHistorySyncTrigger", err)
	}
}

func TestPollDelayDecaysWithAge(t *testing.T) {
	policy := DefaultHistorySyncPolicy()
	cases := []struct {
		elapsed time.Duration
		want    time.Duration
	}{
		{0, 30 * time.Second},
		{9 * time.Minute, 30 * time.Second},
		{10 * time.Minute, 2 * time.Minute},
		{29 * time.Minute, 2 * time.Minute},
		{30 * time.Minute, 10 * time.Minute},
		{3 * time.Hour, 10 * time.Minute},
	}
	for _, tc := range cases {
		if got := policy.PollDelay(tc.elapsed); got != tc.want {
			t.Errorf("PollDelay(%v) = %v, want %v", tc.elapsed, got, tc.want)
		}
	}
}

func TestPassThatImportsSchedulesTheNextPoll(t *testing.T) {
	policy := DefaultHistorySyncPolicy()
	run := newTestSync(t)
	now := syncEpoch.Add(5 * time.Second)

	run.CompletePass(HistoryPass{Seen: 10, Imported: 8, Duplicate: 2}, now, policy)

	if run.Status != HistorySyncRunning {
		t.Fatalf("status = %s, want RUNNING", run.Status)
	}
	if run.Passes != 1 || run.MessagesImported != 8 || run.MessagesDuplicate != 2 || run.MessagesSeen != 10 {
		t.Errorf("counters not accumulated: %+v", run)
	}
	if want := now.Add(30 * time.Second); !run.NextPollAt.Equal(want) {
		t.Errorf("next poll = %v, want %v", run.NextPollAt, want)
	}
	if run.QuietPasses != 0 {
		t.Errorf("a pass that imported must reset the quiet streak, got %d", run.QuietPasses)
	}
}

func TestQuietPassesBeforeTheSettleTimeKeepPolling(t *testing.T) {
	policy := DefaultHistorySyncPolicy()
	run := newTestSync(t)

	run.CompletePass(HistoryPass{}, syncEpoch.Add(time.Second), policy)
	run.CompletePass(HistoryPass{}, syncEpoch.Add(31*time.Second), policy)

	if run.Status != HistorySyncRunning {
		t.Errorf("status = %s; the phone uploads history over minutes, so two early empty passes must not finish the run", run.Status)
	}
}

func TestQuietPassesAfterTheSettleTimeComplete(t *testing.T) {
	policy := DefaultHistorySyncPolicy()
	run := newTestSync(t)

	run.CompletePass(HistoryPass{Imported: 4}, syncEpoch.Add(time.Minute), policy)
	run.CompletePass(HistoryPass{}, syncEpoch.Add(policy.SettleAfter), policy)
	run.CompletePass(HistoryPass{}, syncEpoch.Add(policy.SettleAfter+2*time.Minute), policy)

	if run.Status != HistorySyncCompleted {
		t.Fatalf("status = %s, want COMPLETED", run.Status)
	}
	if run.FinishedAt == nil {
		t.Error("a finished run must stamp FinishedAt")
	}
}

func TestPollWindowEndsTheRunEvenWhileMessagesArrive(t *testing.T) {
	policy := DefaultHistorySyncPolicy()
	run := newTestSync(t)

	run.CompletePass(HistoryPass{Imported: 1}, syncEpoch.Add(policy.PollWindow), policy)

	if run.Status != HistorySyncCompleted {
		t.Errorf("status = %s, want COMPLETED once the poll window closes", run.Status)
	}
}

func TestFailedMessagesAreNotAQuietPass(t *testing.T) {
	policy := DefaultHistorySyncPolicy()
	run := newTestSync(t)

	run.CompletePass(HistoryPass{Failed: 3}, syncEpoch.Add(policy.SettleAfter), policy)
	run.CompletePass(HistoryPass{Failed: 3}, syncEpoch.Add(policy.SettleAfter+time.Minute), policy)

	if run.Status != HistorySyncRunning {
		t.Errorf("status = %s; messages that failed to save must be retried, not declared done", run.Status)
	}
	if run.MessagesFailed != 6 {
		t.Errorf("failed = %d, want 6", run.MessagesFailed)
	}
}

func TestMessageCapStopsTheRunAsPartial(t *testing.T) {
	policy := DefaultHistorySyncPolicy()
	policy.MaxMessages = 10
	run := newTestSync(t)

	run.CompletePass(HistoryPass{Imported: 10, CapReached: true}, syncEpoch.Add(time.Minute), policy)

	if run.Status != HistorySyncPartial {
		t.Fatalf("status = %s, want PARTIAL", run.Status)
	}
	if run.Reason == "" {
		t.Error("a partial run must say which cap stopped it")
	}
}

func TestHorizonTracksTheOldestAndNewestMessage(t *testing.T) {
	policy := DefaultHistorySyncPolicy()
	run := newTestSync(t)
	older := syncEpoch.Add(-6 * 24 * time.Hour)
	newer := syncEpoch.Add(-time.Hour)
	oldest := syncEpoch.Add(-7 * 24 * time.Hour)

	run.CompletePass(HistoryPass{Imported: 2, Oldest: &older, Newest: &newer}, syncEpoch.Add(time.Minute), policy)
	run.CompletePass(HistoryPass{Imported: 1, Oldest: &oldest, Newest: &older}, syncEpoch.Add(2*time.Minute), policy)

	if run.OldestMessageAt == nil || !run.OldestMessageAt.Equal(oldest) {
		t.Errorf("oldest = %v, want %v", run.OldestMessageAt, oldest)
	}
	if run.NewestMessageAt == nil || !run.NewestMessageAt.Equal(newer) {
		t.Errorf("newest = %v, want %v", run.NewestMessageAt, newer)
	}
}

func TestRetryableFailureBacksOff(t *testing.T) {
	policy := DefaultHistorySyncPolicy()
	run := newTestSync(t)

	run.FailAttempt("host timed out", true, syncEpoch, policy)
	first := run.NextPollAt
	run.FailAttempt("host timed out", true, syncEpoch, policy)

	if run.Status == HistorySyncFailed {
		t.Fatal("two retryable failures must not fail the run")
	}
	if !run.NextPollAt.After(first) {
		t.Errorf("backoff did not grow: %v then %v", first, run.NextPollAt)
	}
	if run.LastError != "host timed out" {
		t.Errorf("last error = %q", run.LastError)
	}
}

func TestRetryableFailuresExhaustAttempts(t *testing.T) {
	policy := DefaultHistorySyncPolicy()
	run := newTestSync(t)

	for i := 0; i < policy.MaxAttempts; i++ {
		run.FailAttempt("host timed out", true, syncEpoch, policy)
	}

	if run.Status != HistorySyncFailed {
		t.Errorf("status = %s, want FAILED after %d attempts", run.Status, policy.MaxAttempts)
	}
}

func TestPermanentFailureFailsAtOnce(t *testing.T) {
	policy := DefaultHistorySyncPolicy()
	run := newTestSync(t)

	run.FailAttempt("session gone", false, syncEpoch, policy)

	if run.Status != HistorySyncFailed || run.FinishedAt == nil {
		t.Errorf("status = %s, finished = %v; a permanent failure must end the run", run.Status, run.FinishedAt)
	}
}

func TestASuccessfulPassClearsTheAttemptCounter(t *testing.T) {
	policy := DefaultHistorySyncPolicy()
	run := newTestSync(t)

	run.FailAttempt("host timed out", true, syncEpoch, policy)
	run.CompletePass(HistoryPass{Imported: 1}, syncEpoch.Add(time.Minute), policy)

	if run.Attempts != 0 || run.LastError != "" {
		t.Errorf("attempts = %d, last error = %q; a good pass must reset both", run.Attempts, run.LastError)
	}
}

func TestPauseWaitsForTheSessionThenGivesUp(t *testing.T) {
	policy := DefaultHistorySyncPolicy()
	run := newTestSync(t)

	run.Pause(syncEpoch, policy)
	if run.Status == HistorySyncPartial {
		t.Fatal("a fresh disconnect must pause, not end the run")
	}
	if want := syncEpoch.Add(policy.PauseRetry); !run.NextPollAt.Equal(want) {
		t.Errorf("next poll = %v, want %v", run.NextPollAt, want)
	}

	run.Pause(syncEpoch.Add(policy.PauseLimit), policy)
	if run.Status != HistorySyncPartial {
		t.Errorf("status = %s, want PARTIAL once the pause limit passes", run.Status)
	}
}

func TestResumeExtendsThePollWindowOfAnActiveRun(t *testing.T) {
	policy := DefaultHistorySyncPolicy()
	run := newTestSync(t)
	run.Pause(syncEpoch, policy)
	later := syncEpoch.Add(3 * time.Hour)

	run.Resume(later, policy)

	if !run.NextPollAt.Equal(later) {
		t.Errorf("next poll = %v, want now", run.NextPollAt)
	}
	if want := later.Add(policy.PollWindow); !run.PollUntil.Equal(want) {
		t.Errorf("poll until = %v, want %v", run.PollUntil, want)
	}
	if run.PausedSince != nil {
		t.Error("resuming must clear the pause")
	}
}

func TestStopEndsTheRunWithAReason(t *testing.T) {
	run := newTestSync(t)

	run.Stop(HistorySyncCancelled, "número removido", syncEpoch)

	if run.Status != HistorySyncCancelled || run.Reason != "número removido" || run.FinishedAt == nil {
		t.Errorf("stop did not end the run: %+v", run)
	}
	if run.Status.Active() {
		t.Error("a cancelled run must not count as active")
	}
}

func TestActiveStatuses(t *testing.T) {
	active := map[HistorySyncStatus]bool{
		HistorySyncQueued:    true,
		HistorySyncRunning:   true,
		HistorySyncCompleted: false,
		HistorySyncPartial:   false,
		HistorySyncFailed:    false,
		HistorySyncCancelled: false,
	}
	for status, want := range active {
		if got := status.Active(); got != want {
			t.Errorf("%s.Active() = %v, want %v", status, got, want)
		}
	}
}

func TestManualRequestsAreRateLimited(t *testing.T) {
	policy := DefaultHistorySyncPolicy()
	run := newTestSync(t)
	run.Stop(HistorySyncCompleted, "", syncEpoch)

	if run.AllowsManualRetryAt(syncEpoch.Add(time.Minute), policy) {
		t.Error("a re-sync one minute after the last run finished must be refused")
	}
	if !run.AllowsManualRetryAt(syncEpoch.Add(policy.ManualCooldown), policy) {
		t.Error("a re-sync after the cooldown must be allowed")
	}
}

func TestHistoryPassAddAccumulatesPages(t *testing.T) {
	older := syncEpoch.Add(-48 * time.Hour)
	newer := syncEpoch.Add(-time.Hour)
	pass := HistoryPass{Seen: 2, Imported: 1, Newest: &newer}

	pass.Add(HistoryPass{Seen: 3, Duplicate: 2, Skipped: 1, Failed: 1, Pages: 1, Oldest: &older})

	if pass.Seen != 5 || pass.Imported != 1 || pass.Duplicate != 2 || pass.Skipped != 1 || pass.Failed != 1 || pass.Pages != 1 {
		t.Errorf("pass = %+v", pass)
	}
	if pass.Oldest == nil || !pass.Oldest.Equal(older) || pass.Newest == nil || !pass.Newest.Equal(newer) {
		t.Errorf("horizon = %v .. %v", pass.Oldest, pass.Newest)
	}
}

func TestFailedPassKeepsWhatItImported(t *testing.T) {
	policy := DefaultHistorySyncPolicy()
	run := newTestSync(t)

	run.FailPass(HistoryPass{Seen: 4, Imported: 4}, "host timed out", true, syncEpoch, policy)

	if run.MessagesImported != 4 || run.Attempts != 1 || run.Passes != 0 {
		t.Errorf("run = %+v; a pass that broke midway still imported what it imported", run)
	}
}
