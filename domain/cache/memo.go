package cache

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var ErrGateBusy = errors.New("cache: no capacity freed up before the deadline")

type Memo interface {
	Remember(
		ctx context.Context,
		key string,
		ttl time.Duration,
		compute func(context.Context) ([]byte, error),
	) ([]byte, error)
}

type Gate interface {
	Acquire(ctx context.Context) (release func(), err error)
}

type Versions interface {
	Version(scope string) (string, error)
	Bump(scope string) error
}

func Remember[T any](
	ctx context.Context,
	memo Memo,
	key string,
	ttl time.Duration,
	compute func(context.Context) (T, error),
) (T, error) {
	if memo == nil {
		return compute(ctx)
	}
	var out T
	raw, err := memo.Remember(ctx, key, ttl, func(ctx context.Context) ([]byte, error) {
		value, err := compute(ctx)
		if err != nil {
			return nil, err
		}
		return json.Marshal(value)
	})
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	return out, nil
}
