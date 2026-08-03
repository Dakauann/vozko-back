package cache

import (
	"context"
	"fmt"
	"log"
	"time"

	"vozko/domain/cache"

	"github.com/redis/go-redis/v9"
)

type RedisProvider struct {
	client *redis.Client
}

func NewRedisProvider(addr, password string) *RedisProvider {
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		PoolSize:     50,
		MinIdleConns: 5,
	})

	ctx := context.Background()

	if err := client.Ping(ctx).Err(); err != nil {
		log.Printf("WARNING: Redis ping failed (%s): %v, rate limiting will fail-open", addr, err)
	} else {
		log.Printf("[Redis] Connected to %s", addr)
	}

	if err := rateLimitScript.Load(ctx, client).Err(); err != nil {
		log.Printf("WARNING: Failed to pre-load rate-limit script: %v", err)
	}

	preloadSharedStateScripts(client)

	return &RedisProvider{client: client}
}

func (p *RedisProvider) Ping() error {
	return p.client.Ping(context.Background()).Err()
}

func (p *RedisProvider) Close() error {
	return p.client.Close()
}

func (p *RedisProvider) Cache() cache.Cache {
	return &redisCache{
		client: p.client,
		ctx:    context.Background(),
	}
}

func (p *RedisProvider) RateLimiterFactory() cache.RateLimiterFactory {
	return func(prefix string, maxRequests int, window time.Duration) cache.RateLimiter {
		return &redisRateLimiter{
			client:      p.client,
			ctx:         context.Background(),
			prefix:      prefix,
			maxRequests: maxRequests,
			windowSecs:  int(window.Seconds()),
			window:      window,
		}
	}
}

func (p *RedisProvider) Info() string {
	stats := p.client.PoolStats()
	return fmt.Sprintf("Redis pool: hits=%d misses=%d timeouts=%d totalConns=%d idleConns=%d",
		stats.Hits, stats.Misses, stats.Timeouts, stats.TotalConns, stats.IdleConns)
}

func (p *RedisProvider) SharedState() cache.SharedState {
	return newRedisSharedState(p.client)
}

func (p *RedisProvider) RunLocker() *redisRunLocker {
	return &redisRunLocker{
		client: p.client,
		ctx:    context.Background(),
	}
}
