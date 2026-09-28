package meta

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/cache"
)

type stubLimiter struct {
	allowed bool
	err     error
}

func (s stubLimiter) Allow(string) (bool, time.Duration, error) {
	return s.allowed, time.Second, s.err
}

func factoryReturning(l cache.RateLimiter) cache.RateLimiterFactory {
	return func(string, int, time.Duration) cache.RateLimiter { return l }
}

func TestThrottleAllows(t *testing.T) {
	th, err := NewThrottle(factoryReturning(stubLimiter{allowed: true}), Bucket{Name: "send", Max: 10, Window: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := th.Allow("send", "page-1"); err != nil {
		t.Fatalf("allow: %v", err)
	}
}

func TestThrottleRefusalIsTransientRateLimit(t *testing.T) {
	th, _ := NewThrottle(factoryReturning(stubLimiter{allowed: false}), Bucket{Name: "send", Max: 10, Window: time.Second})
	err := th.Allow("send", "page-1")
	me, ok := AsError(err)
	if !ok || !me.IsRateLimit() || !me.Retryable() {
		t.Fatalf("want retryable rate limit, got %v", err)
	}
}

func TestThrottleLimiterFailureRefuses(t *testing.T) {
	th, _ := NewThrottle(factoryReturning(stubLimiter{err: errors.New("redis down")}), Bucket{Name: "send", Max: 10, Window: time.Second})
	err := th.Allow("send", "page-1")
	me, ok := AsError(err)
	if !ok || !me.Retryable() {
		t.Fatalf("limiter failure must refuse with a retryable error, got %v", err)
	}
}

func TestThrottleUnknownBucketRefuses(t *testing.T) {
	th, _ := NewThrottle(factoryReturning(stubLimiter{allowed: true}), Bucket{Name: "send", Max: 10, Window: time.Second})
	if err := th.Allow("media", "page-1"); err == nil {
		t.Fatal("unknown bucket allowed")
	}
}

func TestThrottleRequiresFactory(t *testing.T) {
	if _, err := NewThrottle(nil, Bucket{Name: "send", Max: 1, Window: time.Second}); err == nil {
		t.Fatal("nil factory accepted")
	}
}
