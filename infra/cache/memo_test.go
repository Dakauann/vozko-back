package cache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	domain "vozko/domain/cache"
)

type fakeStrings struct {
	domain.SharedState
	mu      sync.Mutex
	values  map[string]string
	ttls    map[string]time.Duration
	getErr  error
	setErr  error
	incrErr error
}

func newFakeStrings() *fakeStrings {
	return &fakeStrings{values: map[string]string{}, ttls: map[string]time.Duration{}}
}

func (f *fakeStrings) GetString(key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return "", f.getErr
	}
	return f.values[key], nil
}

func (f *fakeStrings) SetString(key, value string, ttl time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.setErr != nil {
		return f.setErr
	}
	f.values[key] = value
	f.ttls[key] = ttl
	return nil
}

func (f *fakeStrings) Incr(key string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.incrErr != nil {
		return 0, f.incrErr
	}
	current := int64(0)
	if raw := f.values[key]; raw != "" {
		current = int64(len(raw))
	}
	f.values[key] += "x"
	return current + 1, nil
}

func (f *fakeStrings) stored(key string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	value, ok := f.values[key]
	return value, ok
}

func TestMemoServesACachedCopyWithoutComputing(t *testing.T) {
	state := newFakeStrings()
	state.values["k"] = `{"n":1}`
	memo := NewSharedStateMemo(state, time.Second)

	got, err := memo.Remember(context.Background(), "k", time.Minute, func(context.Context) ([]byte, error) {
		t.Fatalf("compute ran although the value was cached")
		return nil, nil
	})
	if err != nil || string(got) != `{"n":1}` {
		t.Fatalf("Remember() = %q, %v, want the cached copy", got, err)
	}
}

func TestMemoComputesAndStoresAMissWithItsTTL(t *testing.T) {
	state := newFakeStrings()
	memo := NewSharedStateMemo(state, time.Second)

	got, err := memo.Remember(context.Background(), "k", 45*time.Second, func(context.Context) ([]byte, error) {
		return []byte(`{"n":2}`), nil
	})
	if err != nil || string(got) != `{"n":2}` {
		t.Fatalf("Remember() = %q, %v, want the computed value", got, err)
	}
	if stored, _ := state.stored("k"); stored != `{"n":2}` {
		t.Fatalf("stored = %q, want the computed value", stored)
	}
	if state.ttls["k"] != 45*time.Second {
		t.Fatalf("ttl = %v, want 45s", state.ttls["k"])
	}
}

func TestMemoRunsOneComputationForConcurrentCallers(t *testing.T) {
	state := newFakeStrings()
	memo := NewSharedStateMemo(state, time.Second)
	release := make(chan struct{})
	var computed atomic.Int32

	var wg sync.WaitGroup
	results := make([]string, 8)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got, err := memo.Remember(context.Background(), "k", time.Minute, func(context.Context) ([]byte, error) {
				computed.Add(1)
				<-release
				return []byte("v"), nil
			})
			if err != nil {
				t.Errorf("Remember() error = %v", err)
			}
			results[i] = string(got)
		}(i)
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	if computed.Load() != 1 {
		t.Fatalf("compute ran %d times for concurrent callers, want 1", computed.Load())
	}
	for i, got := range results {
		if got != "v" {
			t.Fatalf("caller %d got %q, want v", i, got)
		}
	}
}

func TestMemoLetsACallerLeaveWhileTheComputationFinishesForTheNext(t *testing.T) {
	state := newFakeStrings()
	memo := NewSharedStateMemo(state, time.Second)
	release := make(chan struct{})
	finished := make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := memo.Remember(ctx, "k", time.Minute, func(runCtx context.Context) ([]byte, error) {
			defer close(finished)
			<-release
			return []byte("v"), runCtx.Err()
		})
		done <- err
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()

	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Remember() error = %v, want context.Canceled for the caller who left", err)
	}
	close(release)
	<-finished
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if stored, ok := state.stored("k"); ok && stored == "v" {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the abandoned computation never stored its result")
}

func TestMemoBoundsTheComputationWithItsOwnTimeout(t *testing.T) {
	memo := NewSharedStateMemo(newFakeStrings(), 20*time.Millisecond)
	_, err := memo.Remember(context.Background(), "k", time.Minute, func(ctx context.Context) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Remember() error = %v, want context.DeadlineExceeded", err)
	}
}

func TestMemoNeverCachesAFailure(t *testing.T) {
	state := newFakeStrings()
	memo := NewSharedStateMemo(state, time.Second)
	boom := errors.New("boom")

	if _, err := memo.Remember(context.Background(), "k", time.Minute, func(context.Context) ([]byte, error) {
		return nil, boom
	}); !errors.Is(err, boom) {
		t.Fatalf("Remember() error = %v, want %v", err, boom)
	}
	if _, ok := state.stored("k"); ok {
		t.Fatalf("a failed computation was cached")
	}
}

func TestMemoTreatsAnUnreadableCacheAsAMiss(t *testing.T) {
	state := newFakeStrings()
	state.getErr = errors.New("redis down")
	memo := NewSharedStateMemo(state, time.Second)
	computed := false

	got, err := memo.Remember(context.Background(), "k", time.Minute, func(context.Context) ([]byte, error) {
		computed = true
		return []byte("fresh"), nil
	})
	if err != nil || string(got) != "fresh" || !computed {
		t.Fatalf("Remember() = %q, %v (computed %v), want a fresh computation", got, err, computed)
	}
}

func TestMemoStillAnswersWhenTheCacheCannotBeWritten(t *testing.T) {
	state := newFakeStrings()
	state.setErr = errors.New("redis read only")
	memo := NewSharedStateMemo(state, time.Second)

	got, err := memo.Remember(context.Background(), "k", time.Minute, func(context.Context) ([]byte, error) {
		return []byte("fresh"), nil
	})
	if err != nil || string(got) != "fresh" {
		t.Fatalf("Remember() = %q, %v, want the computed value", got, err)
	}
}
