package workspace

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestTryIncr_NeverExceedsMax(t *testing.T) {
	ss := newMockSharedState()
	const max int64 = 5
	const goroutines = 200

	var successes int64
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			ok, err := ss.TryIncr("slots", max)
			if err != nil {
				t.Errorf("TryIncr error: %v", err)
				return
			}
			if ok {
				atomic.AddInt64(&successes, 1)
			}
		}()
	}
	wg.Wait()

	if successes != max {
		t.Fatalf("expected exactly %d successful increments, got %d", max, successes)
	}
	if final := ss.get("slots"); final != max {
		t.Fatalf("expected counter = %d, got %d", max, final)
	}
}

func TestTryIncr_AllowsAfterDecrement(t *testing.T) {
	ss := newMockSharedState()
	const max int64 = 1

	ok, _ := ss.TryIncr("k", max)
	if !ok {
		t.Fatal("first TryIncr should succeed")
	}

	ok, _ = ss.TryIncr("k", max)
	if ok {
		t.Fatal("second TryIncr should fail, at capacity")
	}

	ss.Decr("k")

	ok, _ = ss.TryIncr("k", max)
	if !ok {
		t.Fatal("TryIncr after Decr should succeed")
	}
}

func TestAcquire_ConcurrentRace_NoOverAllocation(t *testing.T) {
	mgr, ss, subscriptions, planReader := newTestCallSlotManager("replica-concurrency")
	_ = ss

	const workspaceMax int64 = 3
	setupSubscription(subscriptions, planReader, "ws-1", int(workspaceMax))

	const goroutines = 50
	var acquired int64
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			if _, ok := mgr.Acquire("ws-1", 100); ok {
				atomic.AddInt64(&acquired, 1)
			}
		}()
	}
	wg.Wait()

	if acquired != workspaceMax {
		t.Fatalf("expected exactly %d acquired slots, got %d", workspaceMax, acquired)
	}
}
