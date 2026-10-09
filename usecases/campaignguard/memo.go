package campaignguard

import (
	"context"
	"sync"
	"time"
)

const WorkspaceMemoTTL = 20 * time.Second

type workspaceMemo[T any] struct {
	mu      sync.Mutex
	ttl     time.Duration
	now     func() time.Time
	entries map[string]memoEntry[T]
}

type memoEntry[T any] struct {
	value   T
	expires time.Time
}

func newWorkspaceMemo[T any](ttl time.Duration, now func() time.Time) *workspaceMemo[T] {
	if now == nil {
		now = time.Now
	}
	return &workspaceMemo[T]{ttl: ttl, now: now, entries: map[string]memoEntry[T]{}}
}

func (m *workspaceMemo[T]) read(workspaceID string, load func() (T, error)) (T, error) {
	at := m.now()
	m.mu.Lock()
	if entry, ok := m.entries[workspaceID]; ok && at.Before(entry.expires) {
		m.mu.Unlock()
		return entry.value, nil
	}
	m.mu.Unlock()
	value, err := load()
	if err != nil {
		var zero T
		return zero, err
	}
	m.mu.Lock()
	m.entries[workspaceID] = memoEntry[T]{value: value, expires: at.Add(m.ttl)}
	m.mu.Unlock()
	return value, nil
}

type memoSpamPolicy struct {
	inner SpamPolicy
	memo  *workspaceMemo[int]
}

func NewMemoSpamPolicy(inner SpamPolicy, ttl time.Duration, now func() time.Time) SpamPolicy {
	return &memoSpamPolicy{inner: inner, memo: newWorkspaceMemo[int](ttl, now)}
}

func (p *memoSpamPolicy) SpamProtectionDays(ctx context.Context, workspaceID string) (int, error) {
	if p.inner == nil {
		return 0, ErrUnavailable
	}
	return p.memo.read(workspaceID, func() (int, error) { return p.inner.SpamProtectionDays(ctx, workspaceID) })
}
