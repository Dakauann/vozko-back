package cache

import (
	"context"
	"errors"
	"testing"
	"time"

	domain "vozko/domain/cache"
)

func TestGateAdmitsUpToItsCapacity(t *testing.T) {
	gate := NewSemaphoreGate(2, 20*time.Millisecond)
	first, err := gate.Acquire(context.Background())
	if err != nil {
		t.Fatalf("first Acquire() error = %v", err)
	}
	second, err := gate.Acquire(context.Background())
	if err != nil {
		t.Fatalf("second Acquire() error = %v", err)
	}
	if _, err := gate.Acquire(context.Background()); !errors.Is(err, domain.ErrGateBusy) {
		t.Fatalf("third Acquire() error = %v, want ErrGateBusy", err)
	}
	first()
	third, err := gate.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire() after a release error = %v", err)
	}
	second()
	third()
}

func TestGateWaitsForASlotToFreeUp(t *testing.T) {
	gate := NewSemaphoreGate(1, time.Second)
	release, err := gate.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	go func() {
		time.Sleep(20 * time.Millisecond)
		release()
	}()
	next, err := gate.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire() while waiting error = %v", err)
	}
	next()
}

func TestGateStopsWaitingWhenTheCallerLeaves(t *testing.T) {
	gate := NewSemaphoreGate(1, time.Second)
	release, _ := gate.Acquire(context.Background())
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := gate.Acquire(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Acquire() error = %v, want the caller's deadline", err)
	}
}

func TestGateReleaseIsIdempotent(t *testing.T) {
	gate := NewSemaphoreGate(1, 10*time.Millisecond)
	release, _ := gate.Acquire(context.Background())
	release()
	release()

	first, err := gate.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if _, err := gate.Acquire(context.Background()); !errors.Is(err, domain.ErrGateBusy) {
		t.Fatalf("a double release freed two slots: Acquire() error = %v, want ErrGateBusy", err)
	}
	first()
}

func TestGateNeverHasLessThanOneSlot(t *testing.T) {
	gate := NewSemaphoreGate(0, 10*time.Millisecond)
	release, err := gate.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire() on a zero-capacity gate error = %v, want one slot", err)
	}
	release()
}

func TestVersionsStartAtZeroAndMoveOnBump(t *testing.T) {
	versions := NewSharedStateVersions(newFakeStrings(), "attendance:overview")
	before, err := versions.Version("ws1")
	if err != nil || before != "0" {
		t.Fatalf("Version() = %q, %v, want 0", before, err)
	}
	if err := versions.Bump("ws1"); err != nil {
		t.Fatalf("Bump() error = %v", err)
	}
	after, err := versions.Version("ws1")
	if err != nil || after == before {
		t.Fatalf("Version() after Bump = %q, %v, want something other than %q", after, err, before)
	}
	other, _ := versions.Version("ws2")
	if other != "0" {
		t.Fatalf("Bump(ws1) moved ws2 to %q", other)
	}
}

func TestVersionsReportAnUnreadableCounter(t *testing.T) {
	state := newFakeStrings()
	state.getErr = errors.New("redis down")
	if _, err := NewSharedStateVersions(state, "p").Version("ws1"); err == nil {
		t.Fatalf("Version() hid a read failure")
	}
	state.incrErr = errors.New("redis down")
	if err := NewSharedStateVersions(state, "p").Bump("ws1"); err == nil {
		t.Fatalf("Bump() hid a write failure")
	}
}
