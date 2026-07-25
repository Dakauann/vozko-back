package cache

import (
	"context"
	"time"
)

type SharedState interface {
	SetNX(key string, value string, ttl time.Duration) (bool, error)

	SetString(key string, value string, ttl time.Duration) error

	GetString(key string) (string, error)

	Del(keys ...string) error

	Exists(key string) (bool, error)

	Incr(key string) (int64, error)

	Decr(key string) (int64, error)

	IncrWithTTL(key string, ttl time.Duration) (int64, error)

	TryIncr(key string, max int64) (bool, error)

	SAdd(key string, members ...string) error

	SRem(key string, members ...string) error

	SMembers(key string) ([]string, error)

	Publish(channel string, data []byte) error

	Subscribe(ctx context.Context, channel string, handler func(data []byte))

	HSet(key, field string, value string) error

	HDel(key, field string) error

	HGetAll(key string) (map[string]string, error)

	HIncrBy(key, field string, incr int64) (int64, error)

	IncrBy(key string, amount int64) (int64, error)

	DecrBy(key string, amount int64) (int64, error)

	TryIncrBy(key string, delta int64, max int64) (bool, error)

	Expire(key string, ttl time.Duration) (bool, error)
}
