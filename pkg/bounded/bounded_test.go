package bounded

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestForEachRunsEveryItemWithinTheWorkerLimit(t *testing.T) {
	var running, peak, done int32
	items := make([]int, 40)
	ForEach(context.Background(), items, 4, func(ctx context.Context, _ int) {
		n := atomic.AddInt32(&running, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
				break
			}
		}
		time.Sleep(time.Millisecond)
		atomic.AddInt32(&running, -1)
		atomic.AddInt32(&done, 1)
	})
	if done != 40 {
		t.Fatalf("ran %d of 40", done)
	}
	if peak > 4 {
		t.Fatalf("peak concurrency %d over the limit", peak)
	}
}

func TestForEachStopsStartingWorkOnceCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	ran := 0
	ForEach(ctx, make([]int, 100), 1, func(ctx context.Context, _ int) {
		mu.Lock()
		ran++
		if ran == 3 {
			cancel()
		}
		mu.Unlock()
	})
	if ran > 4 {
		t.Fatalf("ran %d items after the cancel", ran)
	}
}
