package leadaction

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

var now = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

func newClassifyRun(t *testing.T) *Run {
	t.Helper()
	r, err := NewRun(RunRequest{
		ID: "run-1", WorkspaceID: "ws-1", ActorID: "user-1", DepartmentID: "dep-1",
		Action: ActionClassify, Params: Params{Key: "k", Value: json.RawMessage(`"A"`)},
		IdempotencyKey: "key-1", RequestFingerprint: "fp", Matched: 1200, Selected: 1200,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestNewRun(t *testing.T) {
	r := newClassifyRun(t)
	if r.Status != StatusQueued || r.Phase != PhaseEdit || r.Attempts != 0 || r.Claim != "" || !r.CreatedAt.Equal(now) {
		t.Fatalf("new run = %+v", r)
	}
	if r.Result.Matched != 1200 || r.Result.Selected != 1200 {
		t.Fatalf("new run result = %+v", r.Result)
	}
	bad := []struct {
		name string
		req  RunRequest
		want error
	}{
		{"no workspace", RunRequest{ID: "r", ActorID: "u", Action: ActionBlock, Params: Params{Blocked: blocked(true)}, IdempotencyKey: "k"}, ErrWorkspaceRequired},
		{"no actor", RunRequest{ID: "r", WorkspaceID: "w", Action: ActionBlock, Params: Params{Blocked: blocked(true)}, IdempotencyKey: "k"}, ErrActorRequired},
		{"no key", RunRequest{ID: "r", WorkspaceID: "w", ActorID: "u", Action: ActionBlock, Params: Params{Blocked: blocked(true)}}, ErrIdempotencyKeyRequired},
		{"a key too long", RunRequest{ID: "r", WorkspaceID: "w", ActorID: "u", Action: ActionBlock, Params: Params{Blocked: blocked(true)}, IdempotencyKey: string(make([]byte, MaxIdempotencyKey+1))}, ErrIdempotencyKeyRequired},
		{"export never runs", RunRequest{ID: "r", WorkspaceID: "w", ActorID: "u", Action: ActionExport, IdempotencyKey: "k"}, ErrNotARun},
		{"invalid params", RunRequest{ID: "r", WorkspaceID: "w", ActorID: "u", Action: ActionBlock, IdempotencyKey: "k"}, ErrBlockedRequired},
	}
	for _, tc := range bad {
		if _, err := NewRun(tc.req, now); !errors.Is(err, tc.want) {
			t.Errorf("%s: NewRun = %v, want %v", tc.name, err, tc.want)
		}
	}
}

func TestClaimability(t *testing.T) {
	fresh := now.Add(-time.Second)
	stale := now.Add(-StaleAfter - time.Second)
	later := now.Add(time.Minute)
	cases := []struct {
		name string
		run  Run
		want bool
	}{
		{"queued", Run{Status: StatusQueued}, true},
		{"queued and deferred", Run{Status: StatusQueued, NotBefore: &later}, false},
		{"queued and due", Run{Status: StatusQueued, NotBefore: &fresh}, true},
		{"running and alive", Run{Status: StatusRunning, Claim: "c", HeartbeatAt: &fresh, Attempts: 1}, false},
		{"running and silent", Run{Status: StatusRunning, Claim: "c", HeartbeatAt: &stale, Attempts: 1}, true},
		{"out of attempts", Run{Status: StatusRunning, Claim: "c", HeartbeatAt: &stale, Attempts: MaxAttempts}, false},
		{"done", Run{Status: StatusDone}, false},
		{"failed", Run{Status: StatusFailed}, false},
	}
	for _, tc := range cases {
		if got := tc.run.Claimable(now); got != tc.want {
			t.Errorf("%s: Claimable = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestRunLifecycle(t *testing.T) {
	r := newClassifyRun(t)
	if err := r.Start("claim-1", now); err != nil {
		t.Fatal(err)
	}
	if r.Status != StatusRunning || r.Claim != "claim-1" || r.Attempts != 1 || r.StartedAt == nil {
		t.Fatalf("started run = %+v", r)
	}
	if err := r.Start("claim-2", now); !errors.Is(err, ErrClaimLost) {
		t.Fatalf("a live run was claimed twice: %v", err)
	}

	r.Advance(BatchOutcome{Cursor: "lead-500", Processed: 500, Changed: 480, Skipped: map[SkipReason]int{SkipUnchanged: 20}}, now)
	r.Advance(BatchOutcome{Cursor: "lead-900", Processed: 400, Changed: 400}, now)
	if r.Cursor != "lead-900" || r.Result.Processed != 900 || r.Result.Changed != 880 || r.Result.Skipped[SkipUnchanged] != 20 {
		t.Fatalf("advanced run = %+v", r.Result)
	}

	r.Finish(now)
	if r.Status != StatusDone || r.Claim != "" || r.FinishedAt == nil {
		t.Fatalf("finished run = %+v", r)
	}
}

func TestBlockRunsGoThroughTheMetaPhase(t *testing.T) {
	r, err := NewRun(RunRequest{ID: "r", WorkspaceID: "w", ActorID: "u", Action: ActionBlock, Params: Params{Blocked: blocked(true), BusinessPhoneID: "phone-1"}, IdempotencyKey: "k"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if !r.NeedsMeta() {
		t.Fatal("a block with a business phone applies the Meta block")
	}
	_ = r.Start("c", now)
	r.Advance(BatchOutcome{Cursor: "x", Processed: 2, Changed: 2}, now)
	r.EnterMeta(now)
	if r.Phase != PhaseMeta || r.Cursor != "" {
		t.Fatalf("meta phase = %+v", r)
	}
	until := now.Add(30 * time.Second)
	r.Defer(until, now)
	if r.Status != StatusQueued || r.Claim != "" || r.NotBefore == nil || !r.NotBefore.Equal(until) || r.Attempts != 0 {
		t.Fatalf("deferred run = %+v", r)
	}
	if r.Claimable(now) {
		t.Fatal("a deferred run is claimed before its time")
	}
	if !r.Claimable(until) {
		t.Fatal("a deferred run is claimed once it is due")
	}
	r.Applied(1, 1)
	if r.Result.MetaApplied != 1 || r.Result.MetaFailed != 1 {
		t.Fatalf("meta counts = %+v", r.Result)
	}

	unblock, _ := NewRun(RunRequest{ID: "r", WorkspaceID: "w", ActorID: "u", Action: ActionBlock, Params: Params{Blocked: blocked(false)}, IdempotencyKey: "k"}, now)
	if unblock.NeedsMeta() {
		t.Fatal("a block without a business phone has no Meta phase")
	}
}

func TestFailAndRelease(t *testing.T) {
	r := newClassifyRun(t)
	_ = r.Start("c", now)
	r.Release(now)
	if r.Status != StatusQueued || r.Claim != "" || r.HeartbeatAt != nil {
		t.Fatalf("released run = %+v", r)
	}
	r.Fail(FailureForbidden, now)
	if r.Status != StatusFailed || r.FailureCode != FailureForbidden || r.FinishedAt == nil {
		t.Fatalf("failed run = %+v", r)
	}
}

func TestVisibleTo(t *testing.T) {
	r := newClassifyRun(t)
	if !r.VisibleTo("user-1", false) {
		t.Fatal("the actor sees its run")
	}
	if r.VisibleTo("user-2", false) {
		t.Fatal("another member without the capability sees the run")
	}
	if !r.VisibleTo("user-2", true) {
		t.Fatal("a holder of the action capability sees the run")
	}
}
