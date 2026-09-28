package meta

import (
	"fmt"
	"time"

	"vozko/domain/cache"
)

type Bucket struct {
	Name   string
	Max    int
	Window time.Duration
}

type Throttle struct {
	limiters map[string]cache.RateLimiter
}

func NewThrottle(factory cache.RateLimiterFactory, buckets ...Bucket) (*Throttle, error) {
	if factory == nil {
		return nil, fmt.Errorf("meta: throttle requires a rate limiter factory")
	}
	t := &Throttle{limiters: make(map[string]cache.RateLimiter, len(buckets))}
	for _, b := range buckets {
		if b.Name == "" || b.Max <= 0 || b.Window <= 0 {
			return nil, fmt.Errorf("meta: invalid throttle bucket %+v", b)
		}
		t.limiters[b.Name] = factory(b.Name, b.Max, b.Window)
	}
	return t, nil
}

func (t *Throttle) Allow(bucket, key string) error {
	limiter, ok := t.limiters[bucket]
	if !ok {
		return fmt.Errorf("meta: throttle bucket %q is not registered", bucket)
	}
	allowed, retryAfter, err := limiter.Allow(key)
	if err != nil {
		return &Error{
			Code:        CodeMessagingRate,
			IsTransient: true,
			Message:     fmt.Sprintf("rate limiter unavailable for %s/%s: %v", bucket, key, err),
		}
	}
	if !allowed {
		return &Error{
			Code:        CodeMessagingRate,
			IsTransient: true,
			Message:     fmt.Sprintf("local rate limit reached for %s/%s; retry in %s", bucket, key, retryAfter),
		}
	}
	return nil
}
