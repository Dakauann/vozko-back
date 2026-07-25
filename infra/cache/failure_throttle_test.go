package cache

import (
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	domain "vozko/domain/cache"
)

// fakeShared embeds the SharedState interface (nil) and overrides only the three
// methods the throttle uses; any other call would panic, which never happens here.
type fakeShared struct {
	domain.SharedState
	mu sync.Mutex
	m  map[string]int
}

func newFakeShared() *fakeShared { return &fakeShared{m: map[string]int{}} }

func (f *fakeShared) GetString(k string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.m[k]
	if !ok {
		return "", nil
	}
	return strconv.Itoa(v), nil
}

func (f *fakeShared) IncrWithTTL(k string, _ time.Duration) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.m[k]++
	return int64(f.m[k]), nil
}

func (f *fakeShared) Del(keys ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, k := range keys {
		delete(f.m, k)
	}
	return nil
}

func TestFailureThrottle_AllowsBelowThresholdBlocksAt(t *testing.T) {
	shared := newFakeShared()
	thr := NewFailureThrottle(shared, "login", 3, time.Minute)

	for i := 0; i < 3; i++ {
		if ok, _, _ := thr.Allowed("user@x.com"); !ok {
			t.Fatalf("attempt %d should be allowed (below threshold)", i)
		}
		if err := thr.RegisterFailure("user@x.com"); err != nil {
			t.Fatalf("register: %v", err)
		}
	}

	ok, retryAfter, err := thr.Allowed("user@x.com")
	if err != nil {
		t.Fatalf("allowed: %v", err)
	}
	if ok {
		t.Fatal("expected blocked once count reaches the threshold")
	}
	if retryAfter <= 0 {
		t.Fatalf("expected a positive retry-after, got %v", retryAfter)
	}
}

func TestFailureThrottle_ResetClearsTheCount(t *testing.T) {
	shared := newFakeShared()
	thr := NewFailureThrottle(shared, "login", 2, time.Minute)
	_ = thr.RegisterFailure("u")
	_ = thr.RegisterFailure("u")
	if ok, _, _ := thr.Allowed("u"); ok {
		t.Fatal("expected blocked at threshold")
	}
	if err := thr.Reset("u"); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if ok, _, _ := thr.Allowed("u"); !ok {
		t.Fatal("expected allowed after reset")
	}
}

func TestFailureThrottle_KeysAreHashedAndPerAccount(t *testing.T) {
	shared := newFakeShared()
	thr := NewFailureThrottle(shared, "login", 1, time.Minute)
	_ = thr.RegisterFailure("a@x.com")

	if ok, _, _ := thr.Allowed("a@x.com"); ok {
		t.Fatal("a@x.com should be blocked")
	}
	if ok, _, _ := thr.Allowed("b@x.com"); !ok {
		t.Fatal("b@x.com must be throttled independently of a@x.com")
	}
	for k := range shared.m {
		if strings.Contains(k, "a@x.com") {
			t.Fatalf("raw account identifier leaked into the cache key: %s", k)
		}
	}
}

func TestFailureThrottle_NilSharedAndZeroThresholdAreNoOps(t *testing.T) {
	for _, thr := range []domain.FailureThrottle{
		NewFailureThrottle(nil, "login", 3, time.Minute),
		NewFailureThrottle(newFakeShared(), "login", 0, time.Minute),
	} {
		if ok, _, err := thr.Allowed("u"); !ok || err != nil {
			t.Fatalf("expected allow/no-op, got ok=%v err=%v", ok, err)
		}
		if err := thr.RegisterFailure("u"); err != nil {
			t.Fatalf("register no-op: %v", err)
		}
		if err := thr.Reset("u"); err != nil {
			t.Fatalf("reset no-op: %v", err)
		}
	}
}
