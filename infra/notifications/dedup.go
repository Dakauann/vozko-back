package notification_service

import (
	"time"

	"vozko/domain/cache"
	"vozko/domain/notification"
)

// sharedStateDedup implements notification.Dedup on the shared Redis state via an
// atomic SetNX, so only the first caller for a key within the window wins, even
// across replicas.
type sharedStateDedup struct {
	shared cache.SharedState
}

func NewDedup(shared cache.SharedState) notification.Dedup {
	return &sharedStateDedup{shared: shared}
}

func (d *sharedStateDedup) key(k string) string { return "notify:dedup:" + k }

func (d *sharedStateDedup) FirstTime(k string, ttl time.Duration) (bool, error) {
	if d.shared == nil {
		// No shared state (e.g. single-replica dev without Redis): best-effort send.
		return true, nil
	}
	acquired, err := d.shared.SetNX(d.key(k), "1", ttl)
	if err != nil {
		return false, err
	}
	return acquired, nil
}

func (d *sharedStateDedup) Clear(k string) error {
	if d.shared == nil {
		return nil
	}
	return d.shared.Del(d.key(k))
}
