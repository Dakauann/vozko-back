package balance

import "time"

type InflightReserver interface {
	Reserve(workspaceID string, deltaMicros int64, budgetMicros int64) (bool, error)

	Release(workspaceID string, deltaMicros int64) error

	RefreshTTL(workspaceID string, ttl time.Duration) error

	GetInflight(workspaceID string) (int64, error)
}
