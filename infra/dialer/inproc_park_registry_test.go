package dialer

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	dialer_domain "vozko/domain/dialer"
	"vozko/domain/voip"
)

type fakeParkLeg struct {
	id      string
	done    chan struct{}
	hangups atomic.Int32
}

func newFakeParkLeg(id string) *fakeParkLeg {
	return &fakeParkLeg{id: id, done: make(chan struct{})}
}

func (l *fakeParkLeg) CallID() string      { return l.id }
func (l *fakeParkLeg) PhoneNumber() string { return "+5511999999999" }
func (l *fakeParkLeg) SurrenderMedia() (voip.MediaSession, error) {
	return nil, errors.New("no media")
}
func (l *fakeParkLeg) Hangup() error {
	l.hangups.Add(1)
	return nil
}
func (l *fakeParkLeg) Done() <-chan struct{} { return l.done }

var _ dialer_domain.CallLeg = (*fakeParkLeg)(nil)

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for !cond() {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %s", what)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestPark_RetrieveIsOwnerTokenCAS(t *testing.T) {
	t.Parallel()
	r := NewInProcParkRegistry()
	leg := newFakeParkLeg("call-1")
	if err := r.Park("ws-1", leg, "transfer-1", nil); err != nil {
		t.Fatalf("Park: %v", err)
	}

	if _, ok := r.Retrieve("ws-1", "call-1", "WRONG"); ok {
		t.Fatal("a foreign token must not retrieve the leg")
	}
	got, ok := r.Retrieve("ws-1", "call-1", "transfer-1")
	if !ok || got != dialer_domain.CallLeg(leg) {
		t.Fatal("the owner token must retrieve the exact leg")
	}
	if _, ok := r.Retrieve("ws-1", "call-1", "transfer-1"); ok {
		t.Fatal("retrieve must consume the entry (second retrieve fails)")
	}
}

func TestPark_SecondParkForSameCallIsRejected(t *testing.T) {
	t.Parallel()
	r := NewInProcParkRegistry()
	if err := r.Park("ws-1", newFakeParkLeg("call-1"), "transfer-1", nil); err != nil {
		t.Fatalf("Park: %v", err)
	}
	if err := r.Park("ws-1", newFakeParkLeg("call-1"), "transfer-2", nil); err == nil {
		t.Fatal("a different owner must not park over an existing entry")
	}
	// Same owner re-park (the swap-failure re-park path) is allowed.
	if err := r.Park("ws-1", newFakeParkLeg("call-1"), "transfer-1", nil); err != nil {
		t.Fatalf("same-owner re-park: %v", err)
	}
}

func TestPark_DeathWatcherFiresOnceWhileParked(t *testing.T) {
	t.Parallel()
	r := NewInProcParkRegistry()
	leg := newFakeParkLeg("call-1")
	var deaths atomic.Int32
	if err := r.Park("ws-1", leg, "transfer-1", func() { deaths.Add(1) }); err != nil {
		t.Fatalf("Park: %v", err)
	}

	close(leg.done)
	waitFor(t, "death callback", func() bool { return deaths.Load() == 1 })

	// The watcher removed the entry FIRST, so the funnel's defensive Abandon no-ops.
	if _, ok := r.Find("ws-1", "call-1"); ok {
		t.Fatal("a dead leg must be removed from the park")
	}
	if _, ok := r.Abandon("ws-1", "call-1", "transfer-1"); ok {
		t.Fatal("abandon after death must be a no-op")
	}
	time.Sleep(20 * time.Millisecond)
	if deaths.Load() != 1 {
		t.Fatalf("death callback fired %d times, want exactly 1", deaths.Load())
	}
}

func TestPark_RetrieveDisarmsDeathWatcher(t *testing.T) {
	t.Parallel()
	r := NewInProcParkRegistry()
	leg := newFakeParkLeg("call-1")
	var deaths atomic.Int32
	if err := r.Park("ws-1", leg, "transfer-1", func() { deaths.Add(1) }); err != nil {
		t.Fatalf("Park: %v", err)
	}
	if _, ok := r.Retrieve("ws-1", "call-1", "transfer-1"); !ok {
		t.Fatal("retrieve failed")
	}

	close(leg.done) // the leg dies AFTER a new owner took it
	time.Sleep(30 * time.Millisecond)
	if deaths.Load() != 0 {
		t.Fatalf("a retrieved leg's death must NOT fire the park funnel, fired %d", deaths.Load())
	}
}

func TestPark_FindPeeksWithoutConsuming(t *testing.T) {
	t.Parallel()
	r := NewInProcParkRegistry()
	leg := newFakeParkLeg("call-1")
	if err := r.Park("ws-1", leg, "transfer-1", nil); err != nil {
		t.Fatalf("Park: %v", err)
	}
	entry, ok := r.Find("ws-1", "call-1")
	if !ok || entry.OwnerToken != "transfer-1" || entry.Leg != dialer_domain.CallLeg(leg) {
		t.Fatalf("Find = %+v ok=%v", entry, ok)
	}
	if _, ok := r.Find("ws-1", "call-1"); !ok {
		t.Fatal("Find must not consume the entry")
	}
	// Cross-workspace isolation.
	if _, ok := r.Find("ws-OTHER", "call-1"); ok {
		t.Fatal("a park entry must be invisible outside its workspace")
	}
}
