package balance_usecase

import (
	"fmt"
	"strconv"
	"time"

	"vozko/domain/balance"
	"vozko/domain/cache"
)

const inflightKeyPrefix = "balance:inflight:"

type redisInflightReserver struct {
	shared cache.SharedState
}

func NewInflightReserver(shared cache.SharedState) balance.InflightReserver {
	return &redisInflightReserver{shared: shared}
}

func inflightKey(workspaceID string) string {
	return inflightKeyPrefix + workspaceID
}

func (r *redisInflightReserver) Reserve(workspaceID string, deltaMicros int64, budgetMicros int64) (bool, error) {
	if r.shared == nil {
		return false, fmt.Errorf("inflight reserver: shared state is nil")
	}
	return r.shared.TryIncrBy(inflightKey(workspaceID), deltaMicros, budgetMicros)
}

func (r *redisInflightReserver) Release(workspaceID string, deltaMicros int64) error {
	if r.shared == nil {
		return fmt.Errorf("inflight reserver: shared state is nil")
	}
	_, err := r.shared.DecrBy(inflightKey(workspaceID), deltaMicros)
	return err
}

func (r *redisInflightReserver) RefreshTTL(workspaceID string, ttl time.Duration) error {
	if r.shared == nil {
		return fmt.Errorf("inflight reserver: shared state is nil")
	}
	_, err := r.shared.Expire(inflightKey(workspaceID), ttl)
	return err
}

func (r *redisInflightReserver) GetInflight(workspaceID string) (int64, error) {
	if r.shared == nil {
		return 0, fmt.Errorf("inflight reserver: shared state is nil")
	}
	val, err := r.shared.GetString(inflightKey(workspaceID))
	if err != nil || val == "" {
		return 0, nil
	}
	return strconv.ParseInt(val, 10, 64)
}
