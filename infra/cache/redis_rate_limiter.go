package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	domain "vozko/domain/cache"

	"github.com/redis/go-redis/v9"
)

var (
	ErrRateLimitUnexpectedResult = errors.New("rate limiter: unexpected script result")
	ErrRateLimiterUnavailable    = errors.New("rate limiter: redis client is not configured")
)

var rateLimitScript = redis.NewScript(`
local key     = KEYS[1]
local max     = tonumber(ARGV[1])
local window  = tonumber(ARGV[2])

local current = redis.call("INCR", key)
if current == 1 then
    redis.call("EXPIRE", key, window)
end

if current > max then
    local ttl = redis.call("TTL", key)
    return {0, ttl}
end

return {1, 0}
`)

type redisRateLimiter struct {
	client      *redis.Client
	ctx         context.Context
	prefix      string
	maxRequests int
	windowSecs  int
}

func NewRedisRateLimiter(client *redis.Client, prefix string, maxRequests int, window time.Duration) domain.RateLimiter {
	return &redisRateLimiter{
		client:      client,
		ctx:         context.Background(),
		prefix:      prefix,
		maxRequests: maxRequests,
		windowSecs:  max(1, int(window.Seconds())),
	}
}

func (r *redisRateLimiter) Allow(key string) (bool, time.Duration, error) {
	if r.client == nil {
		return false, 0, ErrRateLimiterUnavailable
	}
	fullKey := fmt.Sprintf("rl:%s:%s", r.prefix, key)

	res, err := rateLimitScript.Run(r.ctx, r.client, []string{fullKey}, r.maxRequests, r.windowSecs).Result()
	if err != nil {
		return false, 0, fmt.Errorf("rate limiter %s: %w", r.prefix, err)
	}
	return rateLimitVerdict(res)
}

func rateLimitVerdict(res interface{}) (bool, time.Duration, error) {
	vals, ok := res.([]interface{})
	if !ok || len(vals) < 2 {
		return false, 0, fmt.Errorf("%w: %v", ErrRateLimitUnexpectedResult, res)
	}
	verdict, verdictOK := vals[0].(int64)
	ttl, ttlOK := vals[1].(int64)
	if !verdictOK || !ttlOK {
		return false, 0, fmt.Errorf("%w: %v", ErrRateLimitUnexpectedResult, res)
	}
	switch verdict {
	case 1:
		return true, 0, nil
	case 0:
		return false, time.Duration(max(ttl, 0)) * time.Second, nil
	default:
		return false, 0, fmt.Errorf("%w: verdict %d", ErrRateLimitUnexpectedResult, verdict)
	}
}
