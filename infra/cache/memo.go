package cache

import (
	"context"
	"log"
	"time"

	"golang.org/x/sync/singleflight"

	domain "vozko/domain/cache"
)

type SharedStateMemo struct {
	state          domain.SharedState
	group          singleflight.Group
	computeTimeout time.Duration
}

func NewSharedStateMemo(state domain.SharedState, computeTimeout time.Duration) *SharedStateMemo {
	return &SharedStateMemo{state: state, computeTimeout: computeTimeout}
}

func (m *SharedStateMemo) Remember(
	ctx context.Context,
	key string,
	ttl time.Duration,
	compute func(context.Context) ([]byte, error),
) ([]byte, error) {
	if cached, ok := m.cached(key); ok {
		return cached, nil
	}

	result := m.group.DoChan(key, func() (any, error) {
		if cached, ok := m.cached(key); ok {
			return cached, nil
		}
		runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), m.computeTimeout)
		defer cancel()
		value, err := compute(runCtx)
		if err != nil {
			return nil, err
		}
		if err := m.state.SetString(key, string(value), ttl); err != nil {
			log.Printf("[memo] could not store %s: %v", key, err)
		}
		return value, nil
	})

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-result:
		if res.Err != nil {
			return nil, res.Err
		}
		return res.Val.([]byte), nil
	}
}

func (m *SharedStateMemo) cached(key string) ([]byte, bool) {
	raw, err := m.state.GetString(key)
	if err != nil {
		log.Printf("[memo] could not read %s: %v", key, err)
		return nil, false
	}
	if raw == "" {
		return nil, false
	}
	return []byte(raw), true
}
