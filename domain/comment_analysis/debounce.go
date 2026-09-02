package comment_analysis

import "time"

// Debounce keyed on the post, not the comment (§6.2). A post taking 5,000
// comments in ten minutes produces ONE hint, not 5,000.
//
// The hint is a timing HINT and nothing more. The invariant (§2.2): the
// database says what is pending; Redis says when to look. Losing every hint
// costs at most one backstop interval of latency and never a comment, which
// is why the read-modify-write in Stamp needs no lock.

// DebouncePolicy is the three triggers, any of which flushes a container.
type DebouncePolicy struct {
	// IdleAfter: the conversation under the post settled. The ordinary case.
	IdleAfter time.Duration
	// MaxPending: do not sit on a fifth of a batch-hour of work.
	MaxPending int
	// MaxAge: the viral guard. A pure inactivity debounce never fires on a
	// post that keeps receiving comments, which is precisely the post the
	// customer is watching. Without this ceiling the feature is silent
	// exactly when it matters.
	MaxAge time.Duration
}

func DefaultDebouncePolicy() DebouncePolicy {
	return DebouncePolicy{IdleAfter: 2 * time.Minute, MaxPending: 200, MaxAge: 10 * time.Minute}
}

func (p *DebouncePolicy) Normalize() {
	d := DefaultDebouncePolicy()
	if p.IdleAfter <= 0 {
		p.IdleAfter = d.IdleAfter
	}
	if p.MaxPending <= 0 {
		p.MaxPending = d.MaxPending
	}
	if p.MaxAge <= 0 {
		p.MaxAge = d.MaxAge
	}
}

// Hint is what ingest knows about a container's recent activity.
type Hint struct {
	Ref         ContainerRef
	WorkspaceID string
	FirstSeen   time.Time
	LastSeen    time.Time
	Count       int
}

// Stamp records one more comment. FirstSeen sticks, LastSeen only advances
// (clock skew between replicas must not make a busy post look idle), the
// count grows.
func (h Hint) Stamp(ref ContainerRef, workspaceID string, now time.Time) Hint {
	h.Ref = ref
	h.WorkspaceID = workspaceID
	if h.FirstSeen.IsZero() || now.Before(h.FirstSeen) {
		h.FirstSeen = now
	}
	if now.After(h.LastSeen) {
		h.LastSeen = now
	}
	h.Count++
	return h
}

// Due reports whether the container should flush now. A hint with zero
// timestamps is due: flushing early costs a smaller batch, wedging costs
// the feature.
func (h Hint) Due(now time.Time, p DebouncePolicy) bool {
	p.Normalize()
	if h.FirstSeen.IsZero() || h.LastSeen.IsZero() {
		return true
	}
	if now.Sub(h.LastSeen) >= p.IdleAfter {
		return true
	}
	if h.Count >= p.MaxPending {
		return true
	}
	if now.Sub(h.FirstSeen) >= p.MaxAge {
		return true
	}
	return false
}
