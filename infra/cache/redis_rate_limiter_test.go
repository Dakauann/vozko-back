package cache

import (
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestRateLimitVerdict(t *testing.T) {
	cases := []struct {
		name       string
		result     interface{}
		allowed    bool
		retryAfter time.Duration
		wantErr    bool
	}{
		{"allowed", []interface{}{int64(1), int64(0)}, true, 0, false},
		{"refused with ttl", []interface{}{int64(0), int64(42)}, false, 42 * time.Second, false},
		{"refused without ttl", []interface{}{int64(0), int64(-1)}, false, 0, false},
		{"not a list", "OK", false, 0, true},
		{"nil result", nil, false, 0, true},
		{"short list", []interface{}{int64(1)}, false, 0, true},
		{"non integer verdict", []interface{}{"1", int64(0)}, false, 0, true},
		{"non integer ttl", []interface{}{int64(0), "soon"}, false, 0, true},
		{"unknown verdict", []interface{}{int64(7), int64(0)}, false, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			allowed, retryAfter, err := rateLimitVerdict(tc.result)
			if tc.wantErr {
				if !errors.Is(err, ErrRateLimitUnexpectedResult) || allowed {
					t.Fatalf("want a refusal with ErrRateLimitUnexpectedResult, got allowed=%v err=%v", allowed, err)
				}
				return
			}
			if err != nil || allowed != tc.allowed || retryAfter != tc.retryAfter {
				t.Fatalf("got allowed=%v retry=%v err=%v", allowed, retryAfter, err)
			}
		})
	}
}

func TestRedisRateLimiter_RefusesWhenRedisIsDown(t *testing.T) {
	client := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1",
		DialTimeout: 50 * time.Millisecond,
		MaxRetries:  -1,
	})
	defer client.Close()

	allowed, _, err := NewRedisRateLimiter(client, "test", 10, time.Minute).Allow("k")
	if err == nil || allowed {
		t.Fatalf("an unreachable Redis must refuse with an error, got allowed=%v err=%v", allowed, err)
	}
}

func TestRedisRateLimiter_WindowNeverRoundsToZero(t *testing.T) {
	limiter := NewRedisRateLimiter(nil, "test", 10, 300*time.Millisecond).(*redisRateLimiter)
	if limiter.windowSecs != 1 {
		t.Fatalf("a sub-second window must still expire after one second, got %d", limiter.windowSecs)
	}
}

func TestRedisRateLimiter_RefusesWithoutAClient(t *testing.T) {
	allowed, _, err := NewRedisRateLimiter(nil, "test", 10, time.Minute).Allow("k")
	if err == nil || allowed {
		t.Fatalf("a missing client must refuse with an error, got allowed=%v err=%v", allowed, err)
	}
}
