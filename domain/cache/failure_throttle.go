package cache

import "time"

type FailureThrottle interface {
	Allowed(key string) (allowed bool, retryAfter time.Duration, err error)
	RegisterFailure(key string) error
	Reset(key string) error
}
