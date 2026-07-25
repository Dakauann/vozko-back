package workflow

import "time"

type RunLocker interface {
	TryLock(runID string, ttl time.Duration) (bool, error)

	Unlock(runID string) error
}
