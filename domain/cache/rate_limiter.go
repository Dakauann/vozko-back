package cache

import "time"

type RateLimiter interface {
	Allow(key string) (allowed bool, retryAfter time.Duration, err error)
}

type RateLimiterFactory func(prefix string, maxRequests int, window time.Duration) RateLimiter
