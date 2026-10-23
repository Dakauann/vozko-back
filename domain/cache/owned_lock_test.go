package cache

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type ownedState struct {
	SharedState
	mu   sync.Mutex
	held map[string]string
	err  error
}

func newOwnedState() *ownedState { return &ownedState{held: map[string]string{}} }

func (s *ownedState) SetNX(key, value string, _ time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return false, s.err
	}
	if _, taken := s.held[key]; taken {
		return false, nil
	}
	s.held[key] = value
	return true, nil
}

func (s *ownedState) DelIfValue(key, value string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, taken := s.held[key]; !taken || current != value {
		return false, nil
	}
	delete(s.held, key)
	return true, nil
}

func (s *ownedState) expire(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.held, key)
}

func testLock(state *ownedState) OwnedLock {
	return OwnedLock{State: state, Owner: state, TTL: time.Minute, Poll: time.Millisecond}
}

func TestOwnedLockSerializesHolders(t *testing.T) {
	lock := testLock(newOwnedState())
	var mu sync.Mutex
	inside, peak := 0, 0
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := lock.Acquire(context.Background(), "k")
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			inside++
			peak = max(peak, inside)
			mu.Unlock()
			time.Sleep(time.Millisecond)
			mu.Lock()
			inside--
			mu.Unlock()
			if released, err := release(); err != nil || !released {
				t.Errorf("release = %v, %v", released, err)
			}
		}()
	}
	wg.Wait()
	if peak != 1 {
		t.Fatalf("%d holders at once, want 1", peak)
	}
}

func TestOwnedLockReleaseNeverFreesTheNextHolder(t *testing.T) {
	state := newOwnedState()
	lock := testLock(state)
	release, err := lock.Acquire(context.Background(), "k")
	if err != nil {
		t.Fatal(err)
	}
	state.expire("k")
	if _, err := lock.Acquire(context.Background(), "k"); err != nil {
		t.Fatal(err)
	}
	if released, err := release(); err != nil || released {
		t.Fatalf("an expired holder released the next one: %v, %v", released, err)
	}
	if len(state.held) != 1 {
		t.Fatal("the next holder lost its lock")
	}
}

func TestOwnedLockFailsClosed(t *testing.T) {
	broken := newOwnedState()
	broken.err = errors.New("redis down")
	if _, err := testLock(broken).Acquire(context.Background(), "k"); err == nil {
		t.Fatal("a lock store failure must not grant the lock")
	}
	if _, err := (OwnedLock{State: newOwnedState(), TTL: time.Minute, Poll: time.Millisecond}).Acquire(context.Background(), "k"); !errors.Is(err, ErrLockUnwired) {
		t.Fatalf("a lock without a guarded delete = %v", err)
	}
	held := newOwnedState()
	held.held["k"] = "someone"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	if _, err := testLock(held).Acquire(ctx, "k"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting past the deadline = %v", err)
	}
}

func TestNewOwnedLockNeedsAGuardedDelete(t *testing.T) {
	if _, err := NewOwnedLock(newOwnedState(), time.Minute, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	type plain struct{ SharedState }
	if _, err := NewOwnedLock(plain{}, time.Minute, time.Millisecond); !errors.Is(err, ErrLockUnwired) {
		t.Fatalf("err = %v", err)
	}
	if _, err := NewOwnedLock(nil, time.Minute, time.Millisecond); !errors.Is(err, ErrLockUnwired) {
		t.Fatalf("nil state err = %v", err)
	}
}
