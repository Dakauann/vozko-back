package dialer

import (
	"testing"
	"time"
)

func TestQueueTargetValidate(t *testing.T) {
	cases := []struct {
		name    string
		target  QueueTarget
		wantErr bool
	}{
		{"department ok", QueueTarget{Kind: QueueTargetDepartment, ID: "dep-1"}, false},
		{"department no id", QueueTarget{Kind: QueueTargetDepartment}, true},
		{"agent ok", QueueTarget{Kind: QueueTargetAgent, ID: "user-1"}, false},
		{"agent blank id", QueueTarget{Kind: QueueTargetAgent, ID: "   "}, true},
		{"workspace ok", QueueTarget{Kind: QueueTargetWorkspace}, false},
		{"workspace ignores id", QueueTarget{Kind: QueueTargetWorkspace, ID: "x"}, false},
		{"bad kind", QueueTarget{Kind: "nope"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.target.Validate()
			if (err != nil) != c.wantErr {
				t.Fatalf("Validate()=%v, wantErr=%v", err, c.wantErr)
			}
		})
	}
}

func TestQueueTargetKeyDistinguishesLines(t *testing.T) {
	dep := QueueTarget{Kind: QueueTargetDepartment, ID: "dep-1"}
	agent := QueueTarget{Kind: QueueTargetAgent, ID: "dep-1"} // same id, different kind
	ws := QueueTarget{Kind: QueueTargetWorkspace}

	keys := map[string]bool{
		dep.Key("ws-1"):   true,
		agent.Key("ws-1"): true,
		ws.Key("ws-1"):    true,
		dep.Key("ws-2"):   true, // same target, different workspace must not collide
	}
	if len(keys) != 4 {
		t.Fatalf("expected 4 distinct line keys, got %d: %v", len(keys), keys)
	}
	if dep.Key("ws-1") == agent.Key("ws-1") {
		t.Fatal("department and agent lines with the same id must not share a key")
	}
}

func TestQueuePolicyNormalizedDefaultsAndCaps(t *testing.T) {
	// Zero policy => safe defaults, never unbounded.
	got := QueuePolicy{Enabled: true}.Normalized()
	if got.MaxWait != DefaultQueueMaxWait {
		t.Errorf("MaxWait default: got %v want %v", got.MaxWait, DefaultQueueMaxWait)
	}
	if got.MaxLength != DefaultQueueMaxLength {
		t.Errorf("MaxLength default: got %d want %d", got.MaxLength, DefaultQueueMaxLength)
	}
	if got.Overflow != QueueOverflowHangup {
		t.Errorf("Overflow default: got %q want %q", got.Overflow, QueueOverflowHangup)
	}
	if !got.Enabled {
		t.Error("Normalized must not disable an enabled policy")
	}

	// Over-cap values are clamped so a caller can never wait forever or a line grow
	// without bound.
	capped := QueuePolicy{
		Enabled:   true,
		MaxWait:   99 * time.Hour,
		MaxLength: 100000,
		Overflow:  "bogus",
	}.Normalized()
	if capped.MaxWait != QueueMaxWaitCap {
		t.Errorf("MaxWait cap: got %v want %v", capped.MaxWait, QueueMaxWaitCap)
	}
	if capped.MaxLength != QueueMaxLengthCap {
		t.Errorf("MaxLength cap: got %d want %d", capped.MaxLength, QueueMaxLengthCap)
	}
	if capped.Overflow != QueueOverflowHangup {
		t.Errorf("invalid overflow must fall back to hangup, got %q", capped.Overflow)
	}

	// A disabled policy stays disabled (but is still normalized so its bounds are sane).
	off := QueuePolicy{Enabled: false}.Normalized()
	if off.Enabled {
		t.Error("Normalized must not enable a disabled policy")
	}
}

func TestQueuedStageTransitions(t *testing.T) {
	ok := func(from, to TransferStage) {
		t.Helper()
		if err := ValidateStageTransition(from, to); err != nil {
			t.Errorf("expected %s->%s legal, got %v", from, to, err)
		}
	}
	bad := func(from, to TransferStage) {
		t.Helper()
		if err := ValidateStageTransition(from, to); err == nil {
			t.Errorf("expected %s->%s illegal", from, to)
		}
	}

	// Entry: a pending offer can drop into the queue.
	ok(TransferStagePendingOffer, TransferStageQueued)
	// Dequeue: an accepted wave moves to completing; teardown to failed/cancelled.
	ok(TransferStageQueued, TransferStageCompleting)
	ok(TransferStageQueued, TransferStageFailed)
	ok(TransferStageQueued, TransferStageCancelled)
	// Overflow action "recall" hands a timed-out queued caller to the recall ladder.
	ok(TransferStageQueued, TransferStageRecalling)
	// A wave whose swap fails re-enters the queue from completing.
	ok(TransferStageCompleting, TransferStageQueued)

	// Queued is NOT terminal.
	if TransferStageQueued.Terminal() {
		t.Fatal("queued must not be terminal")
	}
	// Illegal jumps.
	bad(TransferStageQueued, TransferStageConsulting)
	bad(TransferStageQueued, TransferStageQueued)
	bad(TransferStageCompleted, TransferStageQueued) // from terminal
}

func TestAsQueuedCaller(t *testing.T) {
	now := time.Now()
	h := &TransferHandle{
		ID:          "t-1",
		WorkspaceID: "ws-1",
		CallID:      "c-1",
		QueueKind:   QueueTargetDepartment,
		QueueID:     "dep-1",
		EnqueuedAt:  now,
	}
	qc := h.AsQueuedCaller("+5511999999999")
	if qc.TransferID != "t-1" || qc.WorkspaceID != "ws-1" || qc.CallID != "c-1" {
		t.Fatalf("identity not carried: %+v", qc)
	}
	if qc.Target.Kind != QueueTargetDepartment || qc.Target.ID != "dep-1" {
		t.Fatalf("target not reconstructed: %+v", qc.Target)
	}
	if qc.Phone != "+5511999999999" || !qc.EnqueuedAt.Equal(now) {
		t.Fatalf("phone/enqueuedAt not carried: %+v", qc)
	}
}
