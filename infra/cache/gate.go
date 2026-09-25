package cache

import (
	"context"
	"sync"
	"time"

	domain "vozko/domain/cache"
)

type SemaphoreGate struct {
	slots   chan struct{}
	maxWait time.Duration
}

func NewSemaphoreGate(capacity int, maxWait time.Duration) *SemaphoreGate {
	return &SemaphoreGate{slots: make(chan struct{}, max(capacity, 1)), maxWait: maxWait}
}

func (g *SemaphoreGate) Acquire(ctx context.Context) (func(), error) {
	timer := time.NewTimer(g.maxWait)
	defer timer.Stop()

	select {
	case g.slots <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-g.slots }) }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, domain.ErrGateBusy
	}
}

type SharedStateVersions struct {
	state  domain.SharedState
	prefix string
}

func NewSharedStateVersions(state domain.SharedState, prefix string) *SharedStateVersions {
	return &SharedStateVersions{state: state, prefix: prefix}
}

func (v *SharedStateVersions) Version(scope string) (string, error) {
	raw, err := v.state.GetString(v.key(scope))
	if err != nil {
		return "", err
	}
	if raw == "" {
		return "0", nil
	}
	return raw, nil
}

func (v *SharedStateVersions) Bump(scope string) error {
	_, err := v.state.Incr(v.key(scope))
	return err
}

func (v *SharedStateVersions) key(scope string) string {
	return v.prefix + ":version:" + scope
}
