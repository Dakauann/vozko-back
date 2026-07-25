package limits

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDynamicLimiter_AcquireReleaseAccounting(t *testing.T) {
	t.Parallel()
	l := NewDynamicLimiter(3)

	for i := 0; i < 3; i++ {
		if err := l.Acquire(context.Background()); err != nil {
			t.Fatalf("acquire %d: %v", i, err)
		}
	}
	if got := l.InFlight(); got != 3 {
		t.Fatalf("InFlight after 3 acquires = %d, want 3", got)
	}

	for i := 0; i < 3; i++ {
		l.Release()
	}
	if got := l.InFlight(); got != 0 {
		t.Fatalf("InFlight after 3 releases = %d, want 0", got)
	}
}

func TestDynamicLimiter_OverReleaseClampsAtZero(t *testing.T) {
	t.Parallel()
	l := NewDynamicLimiter(2)

	l.Release()
	l.Release()
	l.Release()
	if got := l.InFlight(); got != 0 {
		t.Fatalf("InFlight after over-release = %d, want 0", got)
	}

	if err := l.Acquire(context.Background()); err != nil {
		t.Fatalf("acquire after over-release: %v", err)
	}
	if got := l.InFlight(); got != 1 {
		t.Fatalf("InFlight after acquire post over-release = %d, want 1", got)
	}
}

func TestDynamicLimiter_ContextCancelDoesNotLeakSlot(t *testing.T) {
	t.Parallel()
	l := NewDynamicLimiter(1)

	if err := l.Acquire(context.Background()); err != nil {
		t.Fatalf("initial acquire: %v", err)
	}

	const waiters = 50
	var canceled atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < waiters; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			if err := l.Acquire(ctx); err != nil {
				canceled.Add(1)
			} else {

				l.Release()
			}
		}()
	}
	wg.Wait()

	if canceled.Load() != waiters {
		t.Fatalf("expected all %d waiters to cancel, got %d", waiters, canceled.Load())
	}
	if got := l.InFlight(); got != 1 {
		t.Fatalf("InFlight after canceled waiters = %d, want 1 (slot leaked)", got)
	}

	l.Release()
	if got := l.InFlight(); got != 0 {
		t.Fatalf("InFlight after final release = %d, want 0", got)
	}
}

func TestDynamicLimiter_PeakNeverExceedsMax(t *testing.T) {
	t.Parallel()
	const (
		max    = 5
		bursts = 200
	)
	l := NewDynamicLimiter(max)

	var inFlight, peak atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < bursts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := l.Acquire(context.Background()); err != nil {
				t.Errorf("acquire: %v", err)
				return
			}
			cur := inFlight.Add(1)
			for {
				p := peak.Load()
				if cur <= p || peak.CompareAndSwap(p, cur) {
					break
				}
			}

			runtime.Gosched()
			inFlight.Add(-1)
			l.Release()
		}()
	}
	wg.Wait()

	if peak.Load() > int32(max) {
		t.Fatalf("peak inflight=%d exceeded max=%d (over-admission)", peak.Load(), max)
	}
	if got := l.InFlight(); got != 0 {
		t.Fatalf("InFlight after burst = %d, want 0", got)
	}
}

func TestDynamicLimiter_SetMaxWakesWaitersOnIncrease(t *testing.T) {
	t.Parallel()
	l := NewDynamicLimiter(1)
	if err := l.Acquire(context.Background()); err != nil {
		t.Fatalf("acquire: %v", err)
	}

	var wg sync.WaitGroup
	const waiters = 5
	wg.Add(waiters)
	acquired := make(chan struct{}, waiters)
	for i := 0; i < waiters; i++ {
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := l.Acquire(ctx); err != nil {
				t.Errorf("waiter acquire: %v", err)
				return
			}
			acquired <- struct{}{}
		}()
	}

	time.Sleep(20 * time.Millisecond)
	l.SetMax(waiters + 1)

	wg.Wait()
	if got := len(acquired); got != waiters {
		t.Fatalf("acquired waiters = %d, want %d", got, waiters)
	}

	for i := 0; i < waiters; i++ {
		l.Release()
	}
	l.Release()
	if got := l.InFlight(); got != 0 {
		t.Fatalf("residual InFlight = %d", got)
	}
}

func TestDynamicLimiter_SetMaxLowerThrottlesNewAcquires(t *testing.T) {
	t.Parallel()
	l := NewDynamicLimiter(5)
	for i := 0; i < 5; i++ {
		if err := l.Acquire(context.Background()); err != nil {
			t.Fatalf("acquire %d: %v", i, err)
		}
	}

	l.SetMax(2)

	if got := l.InFlight(); got != 5 {
		t.Fatalf("InFlight after lowering SetMax = %d, want 5 (holders kept)", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := l.Acquire(ctx); err == nil {
		t.Fatal("acquire should have timed out while inFlight=5 > max=2")
	}

	for i := 0; i < 4; i++ {
		l.Release()
	}
	if err := l.Acquire(context.Background()); err != nil {
		t.Fatalf("acquire after release: %v", err)
	}

	l.Release()
	l.Release()
}

func TestDynamicLimiter_SetMaxRejectsNonPositive(t *testing.T) {
	t.Parallel()
	l := NewDynamicLimiter(3)
	l.SetMax(0)
	l.SetMax(-5)
	if got := l.Max(); got != 3 {
		t.Fatalf("Max after invalid SetMax = %d, want 3", got)
	}
}

func TestDynamicLimiter_NewMaxFloor(t *testing.T) {
	t.Parallel()
	l := NewDynamicLimiter(0)
	if got := l.Max(); got != 1 {
		t.Fatalf("New(0).Max() = %d, want 1", got)
	}
	l = NewDynamicLimiter(-1)
	if got := l.Max(); got != 1 {
		t.Fatalf("New(-1).Max() = %d, want 1", got)
	}
}

func TestDynamicLimiter_NilReceiverSafe(t *testing.T) {
	t.Parallel()
	var l *DynamicLimiter
	if err := l.Acquire(context.Background()); err != nil {
		t.Fatalf("nil Acquire returned err: %v", err)
	}
	l.Release()
	l.SetMax(5)
	if got := l.Max(); got != 0 {
		t.Fatalf("nil Max() = %d, want 0", got)
	}
	if got := l.InFlight(); got != 0 {
		t.Fatalf("nil InFlight() = %d, want 0", got)
	}
}

func TestDynamicLimiter_StressNoLeakUnderResize(t *testing.T) {
	t.Parallel()
	l := NewDynamicLimiter(4)

	const (
		workers = 50
		ops     = 200
	)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				if err := l.Acquire(ctx); err != nil {
					cancel()
					continue
				}
				cancel()
				runtime.Gosched()
				l.Release()
			}
		}()
	}

	resizerDone := make(chan struct{})
	go func() {
		defer close(resizerDone)
		sizes := []int{1, 2, 4, 8, 16, 8, 4, 2}
		for k := 0; k < 200; k++ {
			l.SetMax(sizes[k%len(sizes)])
			time.Sleep(time.Millisecond)
		}
	}()

	wg.Wait()
	<-resizerDone
	l.SetMax(8)

	if got := l.InFlight(); got != 0 {
		t.Fatalf("residual InFlight after stress = %d (slot leak)", got)
	}
}
