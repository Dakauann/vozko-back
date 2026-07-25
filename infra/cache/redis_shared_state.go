package cache

import (
	"context"
	"log"
	"time"

	domainCache "vozko/domain/cache"

	"github.com/redis/go-redis/v9"
)

type redisSharedState struct {
	client *redis.Client
	ctx    context.Context
}

var tryIncrScript = redis.NewScript(`
local key = KEYS[1]
local max = tonumber(ARGV[1])
local current = redis.call('INCR', key)
if current <= max then
    return current
else
    redis.call('DECR', key)
    return -1
end
`)

var tryIncrByScript = redis.NewScript(`
local key    = KEYS[1]
local delta  = tonumber(ARGV[1])
local budget = tonumber(ARGV[2])
local newTotal = redis.call('INCRBY', key, delta)
if newTotal <= budget then
    redis.call('PEXPIRE', key, 300000)
    return newTotal
else
    redis.call('DECRBY', key, delta)
    return -1
end
`)

func newRedisSharedState(client *redis.Client) domainCache.SharedState {
	return &redisSharedState{
		client: client,
		ctx:    context.Background(),
	}
}

func (s *redisSharedState) SetNX(key string, value string, ttl time.Duration) (bool, error) {
	return s.client.SetNX(s.ctx, key, value, ttl).Result()
}

func (s *redisSharedState) SetString(key string, value string, ttl time.Duration) error {
	return s.client.Set(s.ctx, key, value, ttl).Err()
}

func (s *redisSharedState) GetString(key string) (string, error) {
	val, err := s.client.Get(s.ctx, key).Result()
	if err == redis.Nil {
		return "", nil
	}
	return val, err
}

func (s *redisSharedState) Del(keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	return s.client.Del(s.ctx, keys...).Err()
}

func (s *redisSharedState) Exists(key string) (bool, error) {
	n, err := s.client.Exists(s.ctx, key).Result()
	return n > 0, err
}

func (s *redisSharedState) Incr(key string) (int64, error) {
	return s.client.Incr(s.ctx, key).Result()
}

func (s *redisSharedState) Decr(key string) (int64, error) {
	return s.client.Decr(s.ctx, key).Result()
}

var incrWithTTLScript = redis.NewScript(`
local key = KEYS[1]
local ttl = tonumber(ARGV[1])
local count = redis.call('INCR', key)
if count == 1 then
    redis.call('PEXPIRE', key, ttl)
end
return count
`)

func (s *redisSharedState) IncrWithTTL(key string, ttl time.Duration) (int64, error) {
	return incrWithTTLScript.Run(s.ctx, s.client, []string{key}, ttl.Milliseconds()).Int64()
}

func (s *redisSharedState) TryIncr(key string, max int64) (bool, error) {
	result, err := tryIncrScript.Run(s.ctx, s.client, []string{key}, max).Int64()
	if err != nil {
		return false, err
	}
	return result > 0, nil
}

func (s *redisSharedState) IncrBy(key string, amount int64) (int64, error) {
	return s.client.IncrBy(s.ctx, key, amount).Result()
}

func (s *redisSharedState) DecrBy(key string, amount int64) (int64, error) {
	return s.client.DecrBy(s.ctx, key, amount).Result()
}

func (s *redisSharedState) TryIncrBy(key string, delta int64, max int64) (bool, error) {
	result, err := tryIncrByScript.Run(s.ctx, s.client, []string{key}, delta, max).Int64()
	if err != nil {
		return false, err
	}
	return result >= 0, nil
}

func (s *redisSharedState) SAdd(key string, members ...string) error {
	if len(members) == 0 {
		return nil
	}
	args := make([]interface{}, len(members))
	for i, m := range members {
		args[i] = m
	}
	return s.client.SAdd(s.ctx, key, args...).Err()
}

func (s *redisSharedState) SRem(key string, members ...string) error {
	if len(members) == 0 {
		return nil
	}
	args := make([]interface{}, len(members))
	for i, m := range members {
		args[i] = m
	}
	return s.client.SRem(s.ctx, key, args...).Err()
}

func (s *redisSharedState) SMembers(key string) ([]string, error) {
	return s.client.SMembers(s.ctx, key).Result()
}

func (s *redisSharedState) Publish(channel string, data []byte) error {
	return s.client.Publish(s.ctx, channel, data).Err()
}

func (s *redisSharedState) Subscribe(ctx context.Context, channel string, handler func(data []byte)) {
	pubsub := s.client.Subscribe(ctx, channel)
	ch := pubsub.Channel()

	go func() {
		defer pubsub.Close()
		for {
			select {
			case msg, ok := <-ch:
				if !ok {
					return
				}
				handler([]byte(msg.Payload))
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (s *redisSharedState) HSet(key, field string, value string) error {
	return s.client.HSet(s.ctx, key, field, value).Err()
}

func (s *redisSharedState) HDel(key, field string) error {
	return s.client.HDel(s.ctx, key, field).Err()
}

func (s *redisSharedState) HGetAll(key string) (map[string]string, error) {
	result, err := s.client.HGetAll(s.ctx, key).Result()
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}

func (s *redisSharedState) HIncrBy(key, field string, incr int64) (int64, error) {
	return s.client.HIncrBy(s.ctx, key, field, incr).Result()
}

func (s *redisSharedState) Expire(key string, ttl time.Duration) (bool, error) {
	return s.client.Expire(s.ctx, key, ttl).Result()
}

func init() {

	_ = tryIncrScript
}

func preloadSharedStateScripts(client *redis.Client) {
	ctx := context.Background()
	if err := tryIncrScript.Load(ctx, client).Err(); err != nil {
		log.Printf("WARNING: Failed to pre-load tryIncr script: %v", err)
	}
	if err := tryIncrByScript.Load(ctx, client).Err(); err != nil {
		log.Printf("WARNING: Failed to pre-load tryIncrBy script: %v", err)
	}
}
