package notification_service

import (
	"time"

	"vozko/domain/cache"
	"vozko/domain/notification"
)

type sharedStateDedup struct {
	shared cache.SharedState
}

func NewDedup(shared cache.SharedState) notification.Dedup {
	return &sharedStateDedup{shared: shared}
}

func (d *sharedStateDedup) key(k string) string { return "notify:dedup:" + k }

func (d *sharedStateDedup) FirstTime(k string, ttl time.Duration) (bool, error) {
	if d.shared == nil {
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
