package audience

import (
	"testing"
	"time"
)

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
	if !hint(-3*time.Minute, -3*time.Minute, 5).Due(now, policy()) {
		t.Error("a post idle past IdleAfter must flush")
	}
	if hint(-3*time.Minute, -time.Minute, 5).Due(now, policy()) {
		t.Error("a post that is still receiving comments must wait")
	}
	if !hint(-3*time.Minute, -2*time.Minute, 5).Due(now, policy()) {
		t.Error("idle for exactly IdleAfter must flush")
	}
}

func TestConversationHintDoesNotWaitTwice(t *testing.T) {
	h := hint(0, 0, 1)
	h.Ref.Kind = SubjectKindConversation
	if !h.Due(now, policy()) {
		t.Fatal("an already-idle conversation waited again")
	}
	h.Ref.Kind = SubjectKindComment
	if h.Due(now, policy()) {
		t.Fatal("comments lost their inactivity window")
	}
}

func TestHint_Due_MaxPending(t *testing.T) {
	if !hint(-time.Minute, -time.Second, 200).Due(now, policy()) {
		t.Error("MaxPending must flush a busy post")
	}
	if hint(-time.Minute, -time.Second, 199).Due(now, policy()) {
		t.Error("under MaxPending, a busy post waits")
	}
}

func TestHint_Due_MaxAge(t *testing.T) {
	if !hint(-10*time.Minute, -time.Second, 50).Due(now, policy()) {
		t.Error("a post older than MaxAge must flush even while active")
	}
	if hint(-9*time.Minute, -time.Second, 50).Due(now, policy()) {
		t.Error("under MaxAge and still active, the post waits")
	}
}

func TestHint_Due_ZeroTimesAreDue(t *testing.T) {
	if !(Hint{Ref: ref(), Count: 1}).Due(now, policy()) {
		t.Error("a hint with zero timestamps must be treated as due")
	}
}

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
	h = h.Stamp(ref(), "ws-1", now.Add(-time.Hour))
	if !h.LastSeen.Equal(later) {
		t.Error("LastSeen must never move backwards")
	}
}
