package oauthstate

import (
	"errors"
	"sync"
	"testing"
	"time"
)

type memoryState struct {
	mu     sync.Mutex
	values map[string]string
	fail   error
}

func newMemoryState() *memoryState { return &memoryState{values: map[string]string{}} }

func (m *memoryState) SetNX(key, value string, _ time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return false, m.fail
	}
	if _, ok := m.values[key]; ok {
		return false, nil
	}
	m.values[key] = value
	return true, nil
}

func (m *memoryState) Exists(key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return false, m.fail
	}
	_, ok := m.values[key]
	return ok, nil
}

func (m *memoryState) Del(keys ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range keys {
		delete(m.values, k)
	}
	return nil
}

func TestNonceIsSingleUse(t *testing.T) {
	store, err := NewNonceStore(newMemoryState(), "fb:oauth")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Issue("n-1", "ws-1"); err != nil {
		t.Fatalf("issue: %v", err)
	}
	if err := store.Consume("n-1"); err != nil {
		t.Fatalf("first consume: %v", err)
	}
	if err := store.Consume("n-1"); !errors.Is(err, ErrReplayedState) {
		t.Fatalf("second consume = %v, want ErrReplayedState", err)
	}
}

func TestUnknownNonceIsRefused(t *testing.T) {
	store, _ := NewNonceStore(newMemoryState(), "fb:oauth")
	if err := store.Consume("never-issued"); !errors.Is(err, ErrReplayedState) {
		t.Fatalf("got %v, want ErrReplayedState", err)
	}
}

func TestIssueRefusesCollision(t *testing.T) {
	store, _ := NewNonceStore(newMemoryState(), "fb:oauth")
	_ = store.Issue("n-1", "ws-1")
	if err := store.Issue("n-1", "ws-2"); err == nil {
		t.Fatal("colliding nonce accepted")
	}
}

func TestStoreFailureRefuses(t *testing.T) {
	state := newMemoryState()
	store, _ := NewNonceStore(state, "fb:oauth")
	_ = store.Issue("n-1", "ws-1")
	state.fail = errors.New("redis down")
	if err := store.Consume("n-1"); err == nil || errors.Is(err, ErrReplayedState) {
		t.Fatalf("store failure must surface as an error, got %v", err)
	}
	if err := store.Issue("n-2", "ws-1"); err == nil {
		t.Fatal("issue succeeded while the store is down")
	}
}

func TestPrefixesIsolateChannels(t *testing.T) {
	state := newMemoryState()
	ig, _ := NewNonceStore(state, "ig:oauth")
	fb, _ := NewNonceStore(state, "fb:oauth")
	_ = ig.Issue("n-1", "ws-1")
	if err := fb.Consume("n-1"); !errors.Is(err, ErrReplayedState) {
		t.Fatalf("a nonce from another channel was accepted: %v", err)
	}
}

func TestNewNonceStoreRequiresState(t *testing.T) {
	if _, err := NewNonceStore(nil, "fb:oauth"); err == nil {
		t.Fatal("nil state accepted")
	}
	if _, err := NewNonceStore(newMemoryState(), ""); err == nil {
		t.Fatal("empty prefix accepted")
	}
}
