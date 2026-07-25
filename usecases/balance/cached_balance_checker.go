package balance_usecase

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	"vozko/domain/balance"
	"vozko/domain/cache"
)

const defaultBalanceCacheTTL = 10 * time.Second
const invalidateDebounceInterval = 1 * time.Second

type cachedBalanceChecker struct {
	balanceRepo balance.Repository
	shared      cache.SharedState
	cacheTTL    time.Duration

	debounceMu    sync.Mutex
	lastInvalidAt map[string]time.Time
}

func NewCachedBalanceChecker(
	balanceRepo balance.Repository,
	shared cache.SharedState,
	cacheTTL time.Duration,
) balance.CachedBalanceChecker {
	if cacheTTL <= 0 {
		cacheTTL = defaultBalanceCacheTTL
	}
	return &cachedBalanceChecker{
		balanceRepo:   balanceRepo,
		shared:        shared,
		cacheTTL:      cacheTTL,
		lastInvalidAt: make(map[string]time.Time),
	}
}

func balanceCacheKey(workspaceID string) string {
	return "balance:cache:" + workspaceID
}

func (c *cachedBalanceChecker) HasSufficientBalance(workspaceID string, amountMicros int64) (bool, error) {
	key := balanceCacheKey(workspaceID)

	val, err := c.shared.GetString(key)
	if err == nil && val != "" {
		cached, parseErr := strconv.ParseInt(val, 10, 64)
		if parseErr == nil {
			return cached >= amountMicros, nil
		}

	}

	bal, err := c.balanceRepo.GetByWorkspaceID(workspaceID)
	if err != nil {
		return false, fmt.Errorf("cached balance checker: %w", err)
	}

	_ = c.shared.SetString(key, strconv.FormatInt(bal.Amount, 10), c.cacheTTL)

	return bal.Amount >= amountMicros, nil
}

func (c *cachedBalanceChecker) Invalidate(workspaceID string) {
	_ = c.shared.Del(balanceCacheKey(workspaceID))
}

func (c *cachedBalanceChecker) InvalidateDebounced(workspaceID string) {
	now := time.Now()
	c.debounceMu.Lock()
	if last, ok := c.lastInvalidAt[workspaceID]; ok && now.Sub(last) < invalidateDebounceInterval {
		c.debounceMu.Unlock()
		return
	}
	c.lastInvalidAt[workspaceID] = now
	c.debounceMu.Unlock()

	_ = c.shared.Del(balanceCacheKey(workspaceID))
}

func (c *cachedBalanceChecker) GetBalance(workspaceID string) (int64, error) {
	key := balanceCacheKey(workspaceID)

	val, err := c.shared.GetString(key)
	if err == nil && val != "" {
		cached, parseErr := strconv.ParseInt(val, 10, 64)
		if parseErr == nil {
			return cached, nil
		}
	}

	bal, err := c.balanceRepo.GetByWorkspaceID(workspaceID)
	if err != nil {
		return 0, fmt.Errorf("cached balance checker: %w", err)
	}

	_ = c.shared.SetString(key, strconv.FormatInt(bal.Amount, 10), c.cacheTTL)
	return bal.Amount, nil
}
