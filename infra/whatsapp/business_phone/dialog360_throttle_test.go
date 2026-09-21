package whatsapp_business_phone

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/cache"
)

type fakeSemState struct {
	cache.SharedState
	counts map[string]int64
	err    error
}

func newFakeSemState() *fakeSemState { return &fakeSemState{counts: map[string]int64{}} }

func (s *fakeSemState) TryIncr(key string, max int64) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	if s.counts[key] >= max {
		return false, nil
	}
	s.counts[key]++
	return true, nil
}
func (s *fakeSemState) Expire(string, time.Duration) (bool, error) { return true, nil }

func TestDialog360Throttle_WindowLimitsThenResets(t *testing.T) {
	th := newDialog360Throttle(newFakeSemState(), 5, 30*time.Second, 40*time.Second)
	base := time.Unix(1_000_000_000, 0)

	for i := 0; i < 5; i++ {
		if ok, _ := th.tryAcquire(base); !ok {
			t.Fatalf("acquire %d should succeed within the 5/window budget", i+1)
		}
	}
	ok, wait := th.tryAcquire(base)
	if ok {
		t.Fatal("the 6th acquire in the same 30s window must be denied")
	}
	if wait <= 0 {
		t.Fatal("a denied acquire must report a positive wait until the next window")
	}

	if ok, _ := th.tryAcquire(base.Add(30 * time.Second)); !ok {
		t.Fatal("a new 30s window must reset the budget")
	}
}

func TestDialog360Throttle_FailsOpenOnRedisError(t *testing.T) {
	st := &fakeSemState{counts: map[string]int64{}, err: errors.New("redis down")}
	th := newDialog360Throttle(st, 5, 30*time.Second, 40*time.Second)
	if ok, _ := th.tryAcquire(time.Unix(1_000_000_000, 0)); !ok {
		t.Fatal("a Redis error must fail OPEN so partner calls are never blocked")
	}
}

func TestDialog360Throttle_AcquireWaitsAcrossWindow(t *testing.T) {
	th := newDialog360Throttle(newFakeSemState(), 1, 30*time.Second, 60*time.Second)
	cur := time.Unix(1_000_000_000, 0)
	th.now = func() time.Time { return cur }
	th.sleep = func(d time.Duration) { cur = cur.Add(d) }

	th.Acquire()
	th.Acquire()

	th.Acquire()
}

func TestDialog360Throttle_NilIsNoop(t *testing.T) {
	var th *dialog360Throttle
	th.Acquire()
	th2 := newDialog360Throttle(nil, 5, 30*time.Second, 40*time.Second)
	if ok, _ := th2.tryAcquire(time.Now()); !ok {
		t.Fatal("nil shared state must be a no-op (allow)")
	}
}
