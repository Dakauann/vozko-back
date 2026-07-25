package cache

import (
	"context"
	"time"
	"vozko/domain/cache"

	"github.com/redis/go-redis/v9"
)

type redisCache struct {
	client *redis.Client
	ctx    context.Context
}

func NewRedisCache(addr string) cache.Cache {
	client := redis.NewClient(&redis.Options{
		Addr: addr,
	})
	return &redisCache{
		client: client,
		ctx:    context.Background(),
	}
}

func (r *redisCache) Delete(key string) error {
	r.client.Del(r.ctx, key)
	return nil
}

func (r *redisCache) Get(key string) (any, error) {
	result, err := r.client.Get(r.ctx, key).Result()
	if err == redis.Nil {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return result, nil
}

func (r *redisCache) Set(key string, value any, ttl time.Duration) error {
	return r.client.Set(r.ctx, key, value, ttl).Err()
}
