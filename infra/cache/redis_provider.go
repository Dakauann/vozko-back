package cache

import (
	"context"
	"log"
	"time"

	"vozko/domain/cache"

	"github.com/redis/go-redis/v9"
)

type RedisProvider struct {
	client *redis.Client
}

func NewRedisProvider(addr, password string) *RedisProvider {
	return NewRedisProviderOnDB(addr, password, 0)
}

func NewRedisProviderOnDB(addr, password string, db int) *RedisProvider {
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		PoolSize:     50,
		MinIdleConns: 5,
	})

	ctx := context.Background()

	if err := client.Ping(ctx).Err(); err != nil {
		log.Printf("WARNING: Redis ping failed (%s): %v, rate-limited requests will be refused until Redis answers", addr, err)
	} else {
		log.Printf("[Redis] Connected to %s", addr)
	}

	if err := rateLimitScript.Load(ctx, client).Err(); err != nil {
		log.Printf("WARNING: Failed to pre-load rate-limit script: %v", err)
	}

	preloadSharedStateScripts(client)

	return &RedisProvider{client: client}
}

func (p *RedisProvider) Cache() cache.Cache {
	return &redisCache{
		client: p.client,
		ctx:    context.Background(),
	}
}

func (p *RedisProvider) RateLimiterFactory() cache.RateLimiterFactory {
	return func(prefix string, maxRequests int, window time.Duration) cache.RateLimiter {
		return NewRedisRateLimiter(p.client, prefix, maxRequests, window)
	}
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
