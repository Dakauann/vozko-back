package notification

import "time"

type Dedup interface {
	FirstTime(key string, ttl time.Duration) (bool, error)
	Clear(key string) error
}
