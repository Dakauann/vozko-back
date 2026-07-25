package cache

import (
	"context"
	"fmt"
	"time"

	domain "vozko/domain/cache"

	"github.com/redis/go-redis/v9"
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
	window      time.Duration
}

func NewRedisRateLimiter(client *redis.Client, prefix string, maxRequests int, window time.Duration) domain.RateLimiter {
	return &redisRateLimiter{
		client:      client,
		ctx:         context.Background(),
		prefix:      prefix,
		maxRequests: maxRequests,
		windowSecs:  int(window.Seconds()),
		window:      window,
	}
}

func (r *redisRateLimiter) Allow(key string) (bool, time.Duration, error) {
	fullKey := fmt.Sprintf("rl:%s:%s", r.prefix, key)

	res, err := rateLimitScript.Run(r.ctx, r.client, []string{fullKey}, r.maxRequests, r.windowSecs).Result()
	if err != nil {

		return true, 0, err
	}

	vals, ok := res.([]interface{})
	if !ok || len(vals) < 2 {
		return true, 0, nil
	}

	allowed := vals[0].(int64) == 1
	ttl := vals[1].(int64)

	if !allowed {
		return false, time.Duration(ttl) * time.Second, nil
	}
	return true, 0, nil
}
