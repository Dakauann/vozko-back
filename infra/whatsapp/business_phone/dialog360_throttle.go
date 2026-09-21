package whatsapp_business_phone

import (
	"fmt"
	"log"
	"time"

	"vozko/domain/cache"
)

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

func (t *dialog360Throttle) tryAcquire(at time.Time) (bool, time.Duration) {
	if t.shared == nil || t.max <= 0 || t.windowSecs <= 0 {
		return true, 0
	}
	bucket := at.Unix() / t.windowSecs
	key := fmt.Sprintf("%s:%d", t.keyPrefix, bucket)
	ok, err := t.shared.TryIncr(key, t.max)
	if err != nil {
		return true, 0
	}
	if ok {
		_, _ = t.shared.Expire(key, time.Duration(t.windowSecs*2)*time.Second)
		return true, 0
	}
	next := time.Unix((bucket+1)*t.windowSecs, 0)
	return false, next.Sub(at)
}

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
			wait = 3 * time.Second
		}
		if t.now().Add(wait).After(deadline) {
			log.Printf("[dialog360] partner throttle wait budget exhausted; proceeding (may 429)")
			return
		}
		t.sleep(wait)
	}
}
