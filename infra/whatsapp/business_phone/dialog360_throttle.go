package whatsapp_business_phone

import (
	"fmt"
	"log"
	"time"

	"vozko/domain/cache"
)

// dialog360Throttle keeps our calls to the 360dialog Partner API within its
// documented limit, 5 requests / 30s per partner. It WAITS for a slot instead of
// rejecting, so an onboarding burst (CreateClient + account_sharing/numbers +
// ListChannels + api_keys) is smoothed out rather than 429-ing (and cascading into
// failed onboardings). It is backed by Redis (SharedState.TryIncr) so the budget is
// shared across replicas; TryIncr is atomic and DECRs a rejected bump, so retrying
// never inflates the window counter. Fail-open: a Redis blip must never block calls.
type dialog360Throttle struct {
	shared     cache.SharedState
	keyPrefix  string
	max        int64
	windowSecs int64
	maxWait    time.Duration
	now        func() time.Time
	sleep      func(time.Duration)
}

func newDialog360Throttle(shared cache.SharedState, max int64, window, maxWait time.Duration) *dialog360Throttle {
	return &dialog360Throttle{
		shared:     shared,
		keyPrefix:  "throttle:dialog360:partner",
		max:        max,
		windowSecs: int64(window.Seconds()),
		maxWait:    maxWait,
		now:        time.Now,
		sleep:      time.Sleep,
	}
}

// tryAcquire attempts to take a slot in the fixed window containing `at`. It returns
// whether a slot was granted and, if not, how long until the next window opens. It is
// the pure, sleep-free core so the windowing can be unit-tested deterministically.
func (t *dialog360Throttle) tryAcquire(at time.Time) (bool, time.Duration) {
	if t.shared == nil || t.max <= 0 || t.windowSecs <= 0 {
		return true, 0
	}
	bucket := at.Unix() / t.windowSecs
	key := fmt.Sprintf("%s:%d", t.keyPrefix, bucket)
	ok, err := t.shared.TryIncr(key, t.max)
	if err != nil {
		return true, 0 // fail open
	}
	if ok {
		// Let the window key expire after two windows so old buckets self-clean.
		_, _ = t.shared.Expire(key, time.Duration(t.windowSecs*2)*time.Second)
		return true, 0
	}
	next := time.Unix((bucket+1)*t.windowSecs, 0)
	return false, next.Sub(at)
}

// Acquire blocks until a slot is free or the max wait elapses (then proceeds
// best-effort, a rare 429 is still retried by the caller).
func (t *dialog360Throttle) Acquire() {
	if t == nil || t.shared == nil {
		return
	}
	deadline := t.now().Add(t.maxWait)
	for {
		ok, wait := t.tryAcquire(t.now())
		if ok {
			return
		}
		if wait <= 0 {
			wait = 500 * time.Millisecond
		}
		if wait > 3*time.Second {
			wait = 3 * time.Second // re-check periodically rather than sleep a whole window
		}
		if t.now().Add(wait).After(deadline) {
			log.Printf("[dialog360] partner throttle wait budget exhausted; proceeding (may 429)")
			return
		}
		t.sleep(wait)
	}
}
