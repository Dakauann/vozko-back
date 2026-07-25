package queue

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	dialer "vozko/domain/dialer"
)

// --- fakes ------------------------------------------------------------------

type staticPolicy struct{ p dialer.QueuePolicy }

func (s staticPolicy) Resolve(context.Context, string, dialer.QueueTarget) dialer.QueuePolicy {
	return s.p
}

type staticCandidates struct {
	mu  sync.Mutex
	ids []string
}

func (s *staticCandidates) AvailableForTarget(context.Context, string, dialer.QueueTarget) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.ids))
	copy(out, s.ids)
	return out, nil
}

type recorder struct {
	mu     sync.Mutex
	events []Event
}

func (r *recorder) QueueEvent(ev Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
}

func (r *recorder) countType(t string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, e := range r.events {
		if e.Type == t {
			n++
		}
	}
	return n
}

func newDirector(p dialer.QueuePolicy, rec *recorder) *Director {
	return New(Config{
		Policy:     staticPolicy{p: p},
		Candidates: &staticCandidates{ids: []string{"a", "b"}},
		Events:     rec,
		Now:        func() time.Time { return time.Unix(0, 0) },
	})
}

func caller(id string, target dialer.QueueTarget) dialer.QueuedCaller {
	return dialer.QueuedCaller{
		TransferID:  id,
		WorkspaceID: "ws-1",
		CallID:      "call-" + id,
		Target:      target,
		EnqueuedAt:  time.Unix(0, 0),
	}
}

// --- behavior ---------------------------------------------------------------

func TestAdmitFIFOPositions(t *testing.T) {
	rec := &recorder{}
	d := newDirector(dialer.QueuePolicy{Enabled: true, MaxLength: 10}, rec)
	tgt := dialer.QueueTarget{Kind: dialer.QueueTargetWorkspace}

	for i, id := range []string{"t1", "t2", "t3"} {
		pos, err := d.Admit(context.Background(), caller(id, tgt), d.Policy(context.Background(), "ws-1", tgt))
		if err != nil {
			t.Fatalf("Admit %s: %v", id, err)
		}
		if pos != i+1 {
			t.Fatalf("Admit %s position = %d, want %d", id, pos, i+1)
		}
	}
	if got := d.Len("ws-1", tgt); got != 3 {
		t.Fatalf("Len = %d, want 3", got)
	}
	// Head removal shifts everyone forward.
	d.Remove(context.Background(), "ws-1", "t1", dialer.QueueReasonConnected)
	if got := d.Position("ws-1", "t2"); got != 1 {
		t.Fatalf("after head removal t2 position = %d, want 1", got)
	}
	if got := d.Position("ws-1", "t3"); got != 2 {
		t.Fatalf("after head removal t3 position = %d, want 2", got)
	}
}

func TestAdmitIdempotent(t *testing.T) {
	d := newDirector(dialer.QueuePolicy{Enabled: true, MaxLength: 10}, &recorder{})
	tgt := dialer.QueueTarget{Kind: dialer.QueueTargetAgent, ID: "u9"}
	p := d.Policy(context.Background(), "ws-1", tgt)

	pos1, _ := d.Admit(context.Background(), caller("t1", tgt), p)
	pos2, err := d.Admit(context.Background(), caller("t1", tgt), p)
	if err != nil {
		t.Fatalf("re-admit err: %v", err)
	}
	if pos1 != 1 || pos2 != 1 {
		t.Fatalf("idempotent admit positions = %d,%d want 1,1", pos1, pos2)
	}
	if got := d.Len("ws-1", tgt); got != 1 {
		t.Fatalf("re-admit must not duplicate: Len = %d want 1", got)
	}
}

func TestAdmitQueueFull(t *testing.T) {
	rec := &recorder{}
	d := newDirector(dialer.QueuePolicy{Enabled: true, MaxLength: 2}, rec)
	tgt := dialer.QueueTarget{Kind: dialer.QueueTargetWorkspace}
	p := d.Policy(context.Background(), "ws-1", tgt)

	if _, err := d.Admit(context.Background(), caller("t1", tgt), p); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Admit(context.Background(), caller("t2", tgt), p); err != nil {
		t.Fatal(err)
	}
	_, err := d.Admit(context.Background(), caller("t3", tgt), p)
	if err != dialer.ErrQueueFull {
		t.Fatalf("3rd admit err = %v, want ErrQueueFull", err)
	}
	if rec.countType(dialer.QueueReasonFull) != 1 {
		t.Fatalf("expected 1 queue_full event, got %d", rec.countType(dialer.QueueReasonFull))
	}
	// A slot frees up -> the next admit succeeds (bounds are not sticky).
	d.Remove(context.Background(), "ws-1", "t1", dialer.QueueReasonConnected)
	if _, err := d.Admit(context.Background(), caller("t3", tgt), p); err != nil {
		t.Fatalf("admit after free slot: %v", err)
	}
}

func TestRemoveIdempotentAndEvents(t *testing.T) {
	rec := &recorder{}
	d := newDirector(dialer.QueuePolicy{Enabled: true, MaxLength: 10}, rec)
	tgt := dialer.QueueTarget{Kind: dialer.QueueTargetWorkspace}
	p := d.Policy(context.Background(), "ws-1", tgt)
	d.Admit(context.Background(), caller("t1", tgt), p)

	d.Remove(context.Background(), "ws-1", "t1", dialer.QueueReasonAbandoned)
	d.Remove(context.Background(), "ws-1", "t1", dialer.QueueReasonAbandoned) // no-op
	d.Remove(context.Background(), "ws-1", "unknown", dialer.QueueReasonAbandoned)

	if got := d.Position("ws-1", "t1"); got != 0 {
		t.Fatalf("removed caller position = %d, want 0", got)
	}
	if rec.countType(dialer.QueueReasonAbandoned) != 1 {
		t.Fatalf("expected exactly 1 abandoned event, got %d", rec.countType(dialer.QueueReasonAbandoned))
	}
	if rec.countType("enqueued") != 1 {
		t.Fatalf("expected 1 enqueued event, got %d", rec.countType("enqueued"))
	}
}

func TestPolicyNilResolverDisables(t *testing.T) {
	d := New(Config{Candidates: &staticCandidates{}})
	got := d.Policy(context.Background(), "ws-1", dialer.QueueTarget{Kind: dialer.QueueTargetWorkspace})
	if got.Enabled {
		t.Fatal("no resolver must yield a disabled policy")
	}
}

func TestCandidatesPassthrough(t *testing.T) {
	src := &staticCandidates{ids: []string{"x", "y", "z"}}
	d := New(Config{Policy: staticPolicy{}, Candidates: src})
	got, err := d.Candidates(context.Background(), caller("t1", dialer.QueueTarget{Kind: dialer.QueueTargetWorkspace}))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("candidates = %v, want 3", got)
	}
}

// TestConcurrentAdmitRemove hammers the director from many goroutines with -race:
// concurrent admits, removes, positions and candidate reads must never race or
// leave the two indexes (lines vs byID) inconsistent.
func TestConcurrentAdmitRemove(t *testing.T) {
	rec := &recorder{}
	d := newDirector(dialer.QueuePolicy{Enabled: true, MaxLength: dialer.QueueMaxLengthCap}, rec)
	tgt := dialer.QueueTarget{Kind: dialer.QueueTargetWorkspace}
	p := d.Policy(context.Background(), "ws-1", tgt)

	const n = 200
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("t-%d", i)
			if _, err := d.Admit(context.Background(), caller(id, tgt), p); err != nil {
				return
			}
			_ = d.Position("ws-1", id)
			_, _ = d.Candidates(context.Background(), caller(id, tgt))
			d.Remove(context.Background(), "ws-1", id, dialer.QueueReasonConnected)
		}(i)
	}
	wg.Wait()

	// Everything admitted was removed -> the line and index are empty and consistent.
	if got := d.Len("ws-1", tgt); got != 0 {
		t.Fatalf("after concurrent churn Len = %d, want 0", got)
	}
	d.mu.Lock()
	leftLines, leftByID := len(d.lines), len(d.byID)
	d.mu.Unlock()
	if leftLines != 0 || leftByID != 0 {
		t.Fatalf("indexes not fully drained: lines=%d byID=%d", leftLines, leftByID)
	}
}

// TestConcurrentAdmitRespectsMaxLength ensures the MaxLength bound holds under a
// concurrent admit stampede: no more than MaxLength callers are ever admitted, so
// channel/MOH consumption is truly bounded.
func TestConcurrentAdmitRespectsMaxLength(t *testing.T) {
	const maxLen = 10
	rec := &recorder{}
	d := newDirector(dialer.QueuePolicy{Enabled: true, MaxLength: maxLen}, rec)
	tgt := dialer.QueueTarget{Kind: dialer.QueueTargetWorkspace}
	p := d.Policy(context.Background(), "ws-1", tgt)

	var wg sync.WaitGroup
	var admitted int64
	var mu sync.Mutex
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := d.Admit(context.Background(), caller(fmt.Sprintf("t-%d", i), tgt), p); err == nil {
				mu.Lock()
				admitted++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if admitted != maxLen {
		t.Fatalf("admitted %d callers, want exactly %d (MaxLength must hold under concurrency)", admitted, maxLen)
	}
	if got := d.Len("ws-1", tgt); got != maxLen {
		t.Fatalf("Len = %d, want %d", got, maxLen)
	}
}
