package notification_service

import (
	"sync"
	"testing"
	"time"

	"vozko/domain/cache"
)

type fakeSharedDedup struct {
	cache.SharedState
	mu sync.Mutex
	m  map[string]bool
}

func newFakeSharedDedup() *fakeSharedDedup { return &fakeSharedDedup{m: map[string]bool{}} }

func (f *fakeSharedDedup) SetNX(key, _ string, _ time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.m[key] {
		return false, nil
	}
	f.m[key] = true
	return true, nil
}

func (f *fakeSharedDedup) Del(keys ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, k := range keys {
		delete(f.m, k)
	}
	return nil
}

func TestDedup_FirstTimeWinsOnce(t *testing.T) {
	d := NewDedup(newFakeSharedDedup())
	first, err := d.FirstTime("plan_expiry:sub1:1700000000", time.Hour)
	if err != nil || !first {
		t.Fatalf("expected first=true, got first=%v err=%v", first, err)
	}
	again, err := d.FirstTime("plan_expiry:sub1:1700000000", time.Hour)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if again {
		t.Fatal("a repeated key within the window must be suppressed (idempotent)")
	}
}

func TestDedup_DifferentKeysAreIndependent(t *testing.T) {
	d := NewDedup(newFakeSharedDedup())
	if ok, _ := d.FirstTime("a", time.Hour); !ok {
		t.Fatal("key a should fire")
	}
	if ok, _ := d.FirstTime("b", time.Hour); !ok {
		t.Fatal("key b must be independent of a")
	}
}

func TestDedup_ClearReArms(t *testing.T) {
	d := NewDedup(newFakeSharedDedup())
	_, _ = d.FirstTime("low_balance:ws1:critical", time.Hour)
	if err := d.Clear("low_balance:ws1:critical"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	again, _ := d.FirstTime("low_balance:ws1:critical", time.Hour)
	if !again {
		t.Fatal("expected the key to fire again after Clear (condition recovered then recurred)")
	}
}

func TestDedup_NilSharedIsBestEffortAllow(t *testing.T) {
	d := NewDedup(nil)
	first, err := d.FirstTime("k", time.Hour)
	if err != nil || !first {
		t.Fatalf("nil shared state must allow (best-effort), got first=%v err=%v", first, err)
	}
	if err := d.Clear("k"); err != nil {
		t.Fatalf("clear no-op: %v", err)
	}
}
