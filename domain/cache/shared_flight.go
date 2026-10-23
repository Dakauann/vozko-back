package cache

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrSharedRunUnavailable = errors.New("cache: the shared run could not be coordinated")

type Flight interface {
	Share(ctx context.Context, key string, run func(context.Context) ([]byte, error)) ([]byte, error)
}

type SharedFlight struct {
	State SharedState
	Owner OwnedKeyDeleter
	TTL   time.Duration
	Poll  time.Duration
	Keep  time.Duration
}

func NewSharedFlight(state SharedState, ttl, poll, keep time.Duration) (SharedFlight, error) {
	owner, ok := state.(OwnedKeyDeleter)
	if state == nil || !ok {
		return SharedFlight{}, ErrSharedRunUnavailable
	}
	return SharedFlight{State: state, Owner: owner, TTL: ttl, Poll: poll, Keep: keep}, nil
}

func (f SharedFlight) Share(ctx context.Context, key string, run func(context.Context) ([]byte, error)) ([]byte, error) {
	if f.State == nil || f.Owner == nil {
		return nil, ErrSharedRunUnavailable
	}
	token, err := lockToken()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSharedRunUnavailable, err)
	}
	runKey, doneKey := key+":run", key+":done"
	before, err := f.State.GetString(doneKey)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrSharedRunUnavailable, key, err)
	}
	for waited := false; ; waited = true {
		if waited {
			done, err := f.State.GetString(doneKey)
			if err != nil {
				return nil, fmt.Errorf("%w: %s: %w", ErrSharedRunUnavailable, key, err)
			}
			if _, value, finished := strings.Cut(done, ":"); finished && done != before {
				return []byte(value), nil
			}
		}
		acquired, err := f.State.SetNX(runKey, token, f.TTL)
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrSharedRunUnavailable, key, err)
		}
		if acquired {
			return f.lead(ctx, runKey, doneKey, token, run)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(f.Poll):
		}
	}
}

func (f SharedFlight) lead(ctx context.Context, runKey, doneKey, token string, run func(context.Context) ([]byte, error)) ([]byte, error) {
	defer func() { _, _ = f.Owner.DelIfValue(runKey, token) }()
	value, err := run(ctx)
	if err != nil {
		return nil, err
	}
	if err := f.State.SetString(doneKey, token+":"+string(value), f.Keep); err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrSharedRunUnavailable, doneKey, err)
	}
	return value, nil
}
