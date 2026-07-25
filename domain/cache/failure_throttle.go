package cache

import "time"

// FailureThrottle counts failures per key over a sliding window and blocks once a
// threshold is reached, until the window expires; a success resets it. It is the
// standard per-account defence against brute force: unlike a per-IP limit it is
// not defeated by proxy pools and does not collide for legitimate users behind a
// shared NAT.
type FailureThrottle interface {
	// Allowed reports whether an attempt for key may proceed. When blocked it also
	// returns a conservative time-until-retry.
	Allowed(key string) (allowed bool, retryAfter time.Duration, err error)
	// RegisterFailure records one failed attempt for key.
	RegisterFailure(key string) error
	// Reset clears the failure count for key after a success.
	Reset(key string) error
}
