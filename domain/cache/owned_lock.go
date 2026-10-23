package cache

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

var ErrLockUnwired = errors.New("cache: no shared state with a guarded delete wired")

type OwnedLock struct {
	State SharedState
	Owner OwnedKeyDeleter
	TTL   time.Duration
	Poll  time.Duration
}

func (l OwnedLock) Acquire(ctx context.Context, key string) (func() (bool, error), error) {
	if l.State == nil || l.Owner == nil {
		return nil, ErrLockUnwired
	}
	token, err := lockToken()
	if err != nil {
		return nil, err
	}
	for {
		acquired, err := l.State.SetNX(key, token, l.TTL)
		if err != nil {
			return nil, fmt.Errorf("lock %s: %w", key, err)
		}
		if acquired {
			return func() (bool, error) { return l.Owner.DelIfValue(key, token) }, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(l.Poll):
		}
	}
}

func lockToken() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("lock token: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

func NewOwnedLock(state SharedState, ttl, poll time.Duration) (OwnedLock, error) {
	owner, ok := state.(OwnedKeyDeleter)
	if state == nil || !ok {
		return OwnedLock{}, ErrLockUnwired
	}
	return OwnedLock{State: state, Owner: owner, TTL: ttl, Poll: poll}, nil
}

type Locker interface {
	Acquire(ctx context.Context, key string) (func() (bool, error), error)
}

var _ Locker = (*OwnedLock)(nil)
