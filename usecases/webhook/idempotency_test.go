package webhook_usecase

import (
	"context"
	"errors"
	"testing"
	"time"
)

type mockSharedState struct {
	store     map[string]bool
	setNXErr  error
	setStrErr error
	existsErr error
	delErr    error
}

func newMockSharedState() *mockSharedState {
	return &mockSharedState{store: make(map[string]bool)}
}

func (m *mockSharedState) SetNX(key, value string, ttl time.Duration) (bool, error) {
	if m.setNXErr != nil {
		return false, m.setNXErr
	}
	if m.store[key] {
		return false, nil
	}
	m.store[key] = true
	return true, nil
}

func (m *mockSharedState) SetString(key, value string, ttl time.Duration) error {
	if m.setStrErr != nil {
		return m.setStrErr
	}
	m.store[key] = true
	return nil
}
func (m *mockSharedState) GetString(string) (string, error) { return "", nil }
func (m *mockSharedState) Del(keys ...string) error {
	if m.delErr != nil {
		return m.delErr
	}
	for _, key := range keys {
		delete(m.store, key)
	}
	return nil
}
func (m *mockSharedState) Exists(key string) (bool, error) {
	if m.existsErr != nil {
		return false, m.existsErr
	}
	return m.store[key], nil
}
func (m *mockSharedState) Incr(string) (int64, error)                       { return 0, nil }
func (m *mockSharedState) Decr(string) (int64, error)                       { return 0, nil }
func (m *mockSharedState) TryIncr(string, int64) (bool, error)              { return false, nil }
func (m *mockSharedState) SAdd(string, ...string) error                     { return nil }
func (m *mockSharedState) SRem(string, ...string) error                     { return nil }
func (m *mockSharedState) SMembers(string) ([]string, error)                { return nil, nil }
func (m *mockSharedState) Publish(string, []byte) error                     { return nil }
func (m *mockSharedState) Subscribe(context.Context, string, func([]byte))  {}
func (m *mockSharedState) HSet(string, string, string) error                { return nil }
func (m *mockSharedState) HDel(string, string) error                        { return nil }
func (m *mockSharedState) HGetAll(string) (map[string]string, error)        { return nil, nil }
func (m *mockSharedState) IncrWithTTL(string, time.Duration) (int64, error) { return 1, nil }
func (m *mockSharedState) HIncrBy(string, string, int64) (int64, error)     { return 0, nil }
func (m *mockSharedState) Expire(string, time.Duration) (bool, error)       { return true, nil }
func (m *mockSharedState) IncrBy(string, int64) (int64, error)              { return 0, nil }
func (m *mockSharedState) DecrBy(string, int64) (int64, error)              { return 0, nil }
func (m *mockSharedState) TryIncrBy(string, int64, int64) (bool, error)     { return false, nil }

func TestIdempotencyGuard_FirstCallNotDuplicate(t *testing.T) {
	state := newMockSharedState()
	guard := NewIdempotencyGuard(state, 5*time.Minute)

	if guard.IsDuplicate("test-key") {
		t.Fatal("first call should not be duplicate")
	}
}

func TestIdempotencyGuard_SecondCallIsDuplicate(t *testing.T) {
	state := newMockSharedState()
	guard := NewIdempotencyGuard(state, 5*time.Minute)

	guard.IsDuplicate("test-key")

	if !guard.IsDuplicate("test-key") {
		t.Fatal("second call with same key should be duplicate")
	}
}

func TestIdempotencyGuard_DifferentKeysNotDuplicate(t *testing.T) {
	state := newMockSharedState()
	guard := NewIdempotencyGuard(state, 5*time.Minute)

	guard.IsDuplicate("key-a")

	if guard.IsDuplicate("key-b") {
		t.Fatal("different keys should not be duplicate")
	}
}

func TestIdempotencyGuard_EmptyKeyNeverDuplicate(t *testing.T) {
	state := newMockSharedState()
	guard := NewIdempotencyGuard(state, 5*time.Minute)

	if guard.IsDuplicate("") {
		t.Fatal("empty key should never be duplicate")
	}

	if guard.IsDuplicate("") {
		t.Fatal("empty key on second call should never be duplicate")
	}
}

func TestIdempotencyGuard_NilStateNeverDuplicate(t *testing.T) {
	guard := NewIdempotencyGuard(nil, 5*time.Minute)

	if guard.IsDuplicate("test-key") {
		t.Fatal("nil state should never report duplicate")
	}
}

func TestIdempotencyGuard_RedisErrorFailsOpen(t *testing.T) {
	state := newMockSharedState()
	state.setNXErr = errors.New("connection refused")
	guard := NewIdempotencyGuard(state, 5*time.Minute)

	if guard.IsDuplicate("test-key") {
		t.Fatal("redis error should fail open (not duplicate)")
	}
}

func TestIdempotencyGuard_StoresWithPrefix(t *testing.T) {
	state := newMockSharedState()
	guard := NewIdempotencyGuard(state, 5*time.Minute)

	guard.IsDuplicate("my-key")

	if !state.store["dedup:webhook:my-key"] {
		t.Fatal("expected key to be stored with dedup:webhook: prefix")
	}
}

func TestIdempotencyGuard_AcquireTwiceReturnsInProgress(t *testing.T) {
	state := newMockSharedState()
	guard := NewIdempotencyGuard(state, 5*time.Minute)

	result, err := guard.Acquire("my-key")
	if err != nil {
		t.Fatalf("unexpected acquire error: %v", err)
	}
	if result != IdempotencyAcquireProceed {
		t.Fatalf("expected first acquire to proceed, got %v", result)
	}

	result, err = guard.Acquire("my-key")
	if err != nil {
		t.Fatalf("unexpected second acquire error: %v", err)
	}
	if result != IdempotencyAcquireInProgress {
		t.Fatalf("expected second acquire to be in progress, got %v", result)
	}
}

func TestIdempotencyGuard_CompleteMarksDuplicate(t *testing.T) {
	state := newMockSharedState()
	guard := NewIdempotencyGuard(state, 5*time.Minute)

	if _, err := guard.Acquire("my-key"); err != nil {
		t.Fatalf("unexpected acquire error: %v", err)
	}
	if err := guard.Complete("my-key"); err != nil {
		t.Fatalf("unexpected complete error: %v", err)
	}

	result, err := guard.Acquire("my-key")
	if err != nil {
		t.Fatalf("unexpected acquire error after complete: %v", err)
	}
	if result != IdempotencyAcquireDuplicate {
		t.Fatalf("expected duplicate after complete, got %v", result)
	}
	if !state.store["dedup:webhook:done:my-key"] {
		t.Fatal("expected completed key to be stored")
	}
}

func TestIdempotencyGuard_ReleaseAllowsRetry(t *testing.T) {
	state := newMockSharedState()
	guard := NewIdempotencyGuard(state, 5*time.Minute)

	if _, err := guard.Acquire("my-key"); err != nil {
		t.Fatalf("unexpected acquire error: %v", err)
	}
	if err := guard.Release("my-key"); err != nil {
		t.Fatalf("unexpected release error: %v", err)
	}

	result, err := guard.Acquire("my-key")
	if err != nil {
		t.Fatalf("unexpected reacquire error: %v", err)
	}
	if result != IdempotencyAcquireProceed {
		t.Fatalf("expected reacquire to proceed, got %v", result)
	}
}

func TestIdempotencyGuard_AcquireRedisErrorFailsClosed(t *testing.T) {
	state := newMockSharedState()
	state.existsErr = errors.New("redis down")
	guard := NewIdempotencyGuard(state, 5*time.Minute)

	if _, err := guard.Acquire("my-key"); err == nil {
		t.Fatal("expected acquire error when shared state fails")
	}
}
