package cache

import (
	"context"
	"time"

	"vozko/domain/workflow"

	"github.com/redis/go-redis/v9"
)

const runLockPrefix = "wf:run_lock:"

type redisRunLocker struct {
	client *redis.Client
	ctx    context.Context
}

func NewRedisRunLocker(client *redis.Client) workflow.RunLocker {
	return &redisRunLocker{
		client: client,
		ctx:    context.Background(),
	}
}

func (r *redisRunLocker) TryLock(runID string, ttl time.Duration) (bool, error) {
	return r.client.SetNX(r.ctx, runLockPrefix+runID, "1", ttl).Result()
}

func (r *redisRunLocker) Unlock(runID string) error {
	return r.client.Del(r.ctx, runLockPrefix+runID).Err()
}
