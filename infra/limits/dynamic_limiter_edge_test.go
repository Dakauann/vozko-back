package limits

import (
	"context"
	"math"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDynamicLimiter_NoGoroutineLeakUnderChurn(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in -short")
	}
	t.Parallel()
	l := NewDynamicLimiter(8)

	runtime.GC()
	runtime.Gosched()
	time.Sleep(20 * time.Millisecond)
	before := runtime.NumGoroutine()

	const (
		workers = 16
		ops     = 5000
	)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				if err := l.Acquire(context.Background()); err == nil {
					l.Release()
				}
			}
		}()
	}
	wg.Wait()

	runtime.GC()
	runtime.Gosched()
	time.Sleep(20 * time.Millisecond)
	after := runtime.NumGoroutine()

	if delta := after - before; delta > 8 {
		t.Fatalf("goroutine leak: before=%d after=%d delta=%d", before, after, delta)
	}
}

func TestDynamicLimiter_PerInstanceIsolation(t *testing.T) {
	t.Parallel()
	a := NewDynamicLimiter(2)
	b := NewDynamicLimiter(2)

	for i := 0; i < 2; i++ {
		if err := a.Acquire(context.Background()); err != nil {
			t.Fatalf("a.Acquire: %v", err)
		}
	}

	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		err := b.Acquire(ctx)
		cancel()
		if err != nil {
			t.Fatalf("b.Acquire(%d) blocked despite separate limiter: %v", i, err)
		}
	}
	if a.InFlight() != 2 || b.InFlight() != 2 {
		t.Fatalf("InFlight a=%d b=%d, want 2/2", a.InFlight(), b.InFlight())
	}

	a.Release()
	a.Release()
	if a.InFlight() != 0 {
		t.Fatalf("a.InFlight after release = %d, want 0", a.InFlight())
	}
	if b.InFlight() != 2 {
		t.Fatalf("b.InFlight after a.Release = %d, want 2 (cross-instance leak)", b.InFlight())
	}
	b.Release()
	b.Release()
}

func TestDynamicLimiter_SetMaxHugeValueIsCheap(t *testing.T) {
	t.Parallel()
	l := NewDynamicLimiter(4)

	done := make(chan struct{})
	go func() {
		defer close(done)
		l.SetMax(math.MaxInt32)
		if got := l.Max(); got != math.MaxInt32 {
			t.Errorf("Max after huge SetMax = %d", got)
		}
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("SetMax(MaxInt32) did not return quickly — possible allocation")
	}

	l.SetMax(8)
}

func TestDynamicLimiter_NoWaiterStarvedAfterBroadcast(t *testing.T) {
	t.Parallel()
	l := NewDynamicLimiter(1)

	if err := l.Acquire(context.Background()); err != nil {
		t.Fatalf("acquire: %v", err)
	}

	const waiters = 100
	var admitted atomic.Int32
	var wg sync.WaitGroup
	wg.Add(waiters)
	for i := 0; i < waiters; i++ {
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := l.Acquire(ctx); err != nil {
				return
			}
			admitted.Add(1)
		}()
	}
	time.Sleep(30 * time.Millisecond)

	l.SetMax(waiters + 1)
	wg.Wait()

	if admitted.Load() != int32(waiters) {
		t.Fatalf("admitted=%d, want %d (waiter starvation)", admitted.Load(), waiters)
	}

	for i := 0; i < waiters+1; i++ {
		l.Release()
	}
}

func TestDynamicLimiter_ConcurrentCancelAndReleaseRace(t *testing.T) {
	t.Parallel()
	const iterations = 200
	for i := 0; i < iterations; i++ {
		l := NewDynamicLimiter(1)

		if err := l.Acquire(context.Background()); err != nil {
			t.Fatalf("iter %d holder: %v", i, err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		var wg sync.WaitGroup
		wg.Add(1)
		var acquired bool
		go func() {
			defer wg.Done()
			if err := l.Acquire(ctx); err == nil {
				acquired = true
			}
		}()

		var rg sync.WaitGroup
		rg.Add(1)
		go func() {
			defer rg.Done()
			l.Release()
		}()
		go cancel()

		wg.Wait()
		rg.Wait()

		got := l.InFlight()
		if got < 0 || got > 1 {
			t.Fatalf("iter %d: InFlight=%d (invariant broken), acquired=%v", i, got, acquired)
		}
		if acquired && got != 1 {
			t.Fatalf("iter %d: acquired but InFlight=%d", i, got)
		}
		if acquired {
			l.Release()
		}
		if got := l.InFlight(); got != 0 {
			t.Fatalf("iter %d: residual InFlight=%d", i, got)
		}
	}
}
