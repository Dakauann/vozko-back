package webhook_usecase

import (
	"fmt"
	"log"
	"time"
	"vozko/domain/cache"
)

type IdempotencyAcquireResult int

const (
	IdempotencyAcquireProceed IdempotencyAcquireResult = iota
	IdempotencyAcquireDuplicate
	IdempotencyAcquireInProgress
)

type IdempotencyGuard struct {
	state cache.SharedState
	ttl   time.Duration
}

func NewIdempotencyGuard(state cache.SharedState, ttl time.Duration) *IdempotencyGuard {
	return &IdempotencyGuard{state: state, ttl: ttl}
}

func (g *IdempotencyGuard) Acquire(key string) (IdempotencyAcquireResult, error) {
	if key == "" || g.state == nil {
		return IdempotencyAcquireProceed, nil
	}

	completed, err := g.state.Exists(g.completedKey(key))
	if err != nil {
		return IdempotencyAcquireProceed, fmt.Errorf("check completed idempotency key: %w", err)
	}
	if completed {
		return IdempotencyAcquireDuplicate, nil
	}

	acquired, err := g.state.SetNX(g.processingKey(key), "1", g.ttl)
	if err != nil {
		return IdempotencyAcquireProceed, fmt.Errorf("acquire processing idempotency key: %w", err)
	}
	if !acquired {
		return IdempotencyAcquireInProgress, nil
	}

	return IdempotencyAcquireProceed, nil
}

func (g *IdempotencyGuard) Complete(key string) error {
	if key == "" || g.state == nil {
		return nil
	}

	if err := g.state.SetString(g.completedKey(key), "1", g.ttl); err != nil {
		return fmt.Errorf("store completed idempotency key: %w", err)
	}
	if err := g.state.Del(g.processingKey(key)); err != nil {
		log.Printf("[webhook-dedup] failed to clear processing key for %s: %v", key, err)
	}
	return nil
}

func (g *IdempotencyGuard) Release(key string) error {
	if key == "" || g.state == nil {
		return nil
	}
	if err := g.state.Del(g.processingKey(key)); err != nil {
		return fmt.Errorf("release processing idempotency key: %w", err)
	}
	return nil
}

func (g *IdempotencyGuard) IsDuplicate(key string) bool {
	if key == "" || g.state == nil {
		return false
	}
	acquired, err := g.state.SetNX("dedup:webhook:"+key, "1", g.ttl)
	if err != nil {
		log.Printf("[webhook-dedup] Redis error: %v, allowing message", err)
		return false
	}
	return !acquired
}

func (g *IdempotencyGuard) processingKey(key string) string {
	return "dedup:webhook:processing:" + key
}

func (g *IdempotencyGuard) completedKey(key string) string {
	return "dedup:webhook:done:" + key
}
