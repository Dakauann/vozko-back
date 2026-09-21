package audience

import "time"

type DebouncePolicy struct {
	IdleAfter  time.Duration
	MaxPending int
	MaxAge     time.Duration
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

type Hint struct {
	Ref         ContainerRef
	WorkspaceID string
	FirstSeen   time.Time
	LastSeen    time.Time
	Count       int
}

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

func (h Hint) Due(now time.Time, p DebouncePolicy) bool {
	if h.Ref.Normalized().Kind == SubjectKindConversation {
		return true
	}
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
