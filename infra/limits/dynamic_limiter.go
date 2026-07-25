package limits

import (
	"context"
	"sync"
)

type DynamicLimiter struct {
	mu       sync.Mutex
	max      int
	inFlight int
	changed  chan struct{}
}

func NewDynamicLimiter(max int) *DynamicLimiter {
	if max <= 0 {
		max = 1
	}
	return &DynamicLimiter{
		max:     max,
		changed: make(chan struct{}),
	}
}

func (l *DynamicLimiter) Acquire(ctx context.Context) error {
	if l == nil {
		return nil
	}

	for {
		l.mu.Lock()
		if l.inFlight < l.max {
			l.inFlight++
			l.mu.Unlock()
			return nil
		}
		changed := l.changed
		l.mu.Unlock()

		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (l *DynamicLimiter) Release() {
	if l == nil {
		return
	}

	l.mu.Lock()
	if l.inFlight > 0 {
		l.inFlight--
		l.notifyLocked()
	}
	l.mu.Unlock()
}

func (l *DynamicLimiter) SetMax(max int) {
	if l == nil || max <= 0 {
		return
	}

	l.mu.Lock()
	if l.max != max {
		l.max = max
		l.notifyLocked()
	}
	l.mu.Unlock()
}

func (l *DynamicLimiter) Max() int {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.max
}

func (l *DynamicLimiter) InFlight() int {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.inFlight
}

func (l *DynamicLimiter) notifyLocked() {
	close(l.changed)
	l.changed = make(chan struct{})
}
