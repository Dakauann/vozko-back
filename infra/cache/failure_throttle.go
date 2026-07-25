package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"

	domain "vozko/domain/cache"
)

// sharedStateFailureThrottle implements domain.FailureThrottle on top of the shared
// Redis state (memory in single-replica dev). The key is hashed so raw account
// identifiers (emails) are never written to the cache. IncrWithTTL increments and
// (re)sets the window TTL atomically; Allowed only reads, so it never consumes the
// budget on a legitimate attempt.
type sharedStateFailureThrottle struct {
	shared    domain.SharedState
	prefix    string
	threshold int
	window    time.Duration
}

func NewFailureThrottle(shared domain.SharedState, prefix string, threshold int, window time.Duration) domain.FailureThrottle {
	return &sharedStateFailureThrottle{shared: shared, prefix: prefix, threshold: threshold, window: window}
}

func (t *sharedStateFailureThrottle) key(k string) string {
	sum := sha256.Sum256([]byte(k))
	return "throttle:" + t.prefix + ":" + hex.EncodeToString(sum[:12])
}

func (t *sharedStateFailureThrottle) Allowed(k string) (bool, time.Duration, error) {
	if t.shared == nil || t.threshold <= 0 {
		return true, 0, nil
	}
	v, err := t.shared.GetString(t.key(k))
	if err != nil {
		// Fail-open: a cache blip must not lock everyone out. The per-IP middleware
		// is the fail-closed backstop.
		return true, 0, err
	}
	if v == "" {
		return true, 0, nil
	}
	n, perr := strconv.Atoi(v)
	if perr != nil {
		return true, 0, nil
	}
	if n >= t.threshold {
		// window is a safe upper bound for Retry-After (the real reset may be sooner).
		return false, t.window, nil
	}
	return true, 0, nil
}

func (t *sharedStateFailureThrottle) RegisterFailure(k string) error {
	if t.shared == nil || t.threshold <= 0 {
		return nil
	}
	_, err := t.shared.IncrWithTTL(t.key(k), t.window)
	return err
}

func (t *sharedStateFailureThrottle) Reset(k string) error {
	if t.shared == nil {
		return nil
	}
	return t.shared.Del(t.key(k))
}
