package comment_analysis

import (
	"testing"
	"time"
)

// Debounce is keyed on the POST, not the comment (§6.2). A container flushes
// when ANY of three triggers fires; each is tested with the other two held
// far away.

func policy() DebouncePolicy {
	return DebouncePolicy{IdleAfter: 2 * time.Minute, MaxPending: 200, MaxAge: 10 * time.Minute}
}

func hint(first, last time.Duration, count int) Hint {
	return Hint{
		Ref:         ref(),
		WorkspaceID: "ws-1",
		FirstSeen:   now.Add(first),
		LastSeen:    now.Add(last),
		Count:       count,
	}
}

func TestDebouncePolicy_Normalize(t *testing.T) {
	var p DebouncePolicy
	p.Normalize()
	if p != DefaultDebouncePolicy() {
		t.Fatalf("zero policy should normalise to the defaults, got %+v", p)
	}
	d := DefaultDebouncePolicy()
	if d.IdleAfter != 2*time.Minute || d.MaxPending != 200 || d.MaxAge != 10*time.Minute {
		t.Fatalf("defaults drifted: %+v", d)
	}
}

func TestHint_Due_Idle(t *testing.T) {
	// Last comment 3 minutes ago, only 5 pending, first seen 3 minutes ago.
	if !hint(-3*time.Minute, -3*time.Minute, 5).Due(now, policy()) {
		t.Error("a post idle past IdleAfter must flush")
	}
	// Last comment 1 minute ago: still settling.
	if hint(-3*time.Minute, -time.Minute, 5).Due(now, policy()) {
		t.Error("a post that is still receiving comments must wait")
	}
	// Exactly at the boundary counts as idle.
	if !hint(-3*time.Minute, -2*time.Minute, 5).Due(now, policy()) {
		t.Error("idle for exactly IdleAfter must flush")
	}
}

func TestHint_Due_MaxPending(t *testing.T) {
	// Busy post (last comment seconds ago, first seen a minute ago) but 200
	// pending: do not sit on a fifth of a batch-hour of work.
	if !hint(-time.Minute, -time.Second, 200).Due(now, policy()) {
		t.Error("MaxPending must flush a busy post")
	}
	if hint(-time.Minute, -time.Second, 199).Due(now, policy()) {
		t.Error("under MaxPending, a busy post waits")
	}
}

// THE viral guard. A pure inactivity debounce never fires on a post that
// keeps receiving comments, which is precisely the post the customer is
// watching.
func TestHint_Due_MaxAge(t *testing.T) {
	// Comments every few seconds for ten minutes, never 200 pending at once.
	if !hint(-10*time.Minute, -time.Second, 50).Due(now, policy()) {
		t.Error("a post older than MaxAge must flush even while active")
	}
	if hint(-9*time.Minute, -time.Second, 50).Due(now, policy()) {
		t.Error("under MaxAge and still active, the post waits")
	}
}

func TestHint_Due_ZeroTimesAreDue(t *testing.T) {
	// A corrupt or partial hint (zero times) must flush rather than wedge:
	// flushing early costs a smaller batch, wedging costs the feature.
	if !(Hint{Ref: ref(), Count: 1}).Due(now, policy()) {
		t.Error("a hint with zero timestamps must be treated as due")
	}
}

// Stamp is the ingest-side read-modify-write: first seen sticks, last seen
// advances, the count grows. A lost update here is harmless (§2.2): the
// database says what is pending; the hint only says when to look.
func TestHint_Stamp(t *testing.T) {
	var h Hint
	h = h.Stamp(ref(), "ws-1", now)
	if !h.FirstSeen.Equal(now) || !h.LastSeen.Equal(now) || h.Count != 1 {
		t.Fatalf("first stamp: %+v", h)
	}
	later := now.Add(30 * time.Second)
	h = h.Stamp(ref(), "ws-1", later)
	if !h.FirstSeen.Equal(now) {
		t.Error("FirstSeen must not move")
	}
	if !h.LastSeen.Equal(later) || h.Count != 2 {
		t.Errorf("second stamp: %+v", h)
	}
	// Time going backwards (clock skew between replicas) must not rewind
	// LastSeen, or a post could look idle while it is not.
	h = h.Stamp(ref(), "ws-1", now.Add(-time.Hour))
	if !h.LastSeen.Equal(later) {
		t.Error("LastSeen must never move backwards")
	}
}
