package balance_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/balance"
	"vozko/domain/shared"
)

type cachedCheckerBalanceRepo struct {
	bal *balance.Balance
	err error

	getCalls int
}

func (m *cachedCheckerBalanceRepo) GetByWorkspaceID(_ string) (*balance.Balance, error) {
	m.getCalls++
	return m.bal, m.err
}

func (m *cachedCheckerBalanceRepo) Create(_ *balance.Balance) error { return nil }
func (m *cachedCheckerBalanceRepo) EnsureBalanceExists(_ string, _ string) (*balance.Balance, error) {
	return nil, nil
}
func (m *cachedCheckerBalanceRepo) CreditBalance(_ balance.CreditBalanceInput) (*balance.Transaction, error) {
	return nil, nil
}
func (m *cachedCheckerBalanceRepo) DebitBalance(_ balance.DebitBalanceInput) (*balance.Transaction, error) {
	return nil, nil
}
func (m *cachedCheckerBalanceRepo) HasSufficientBalance(_ string, _ int64) (bool, error) {
	return false, nil
}
func (m *cachedCheckerBalanceRepo) GetFullBalanceSummary(_ string) (*balance.FullBalanceSummary, error) {
	return nil, nil
}
func (m *cachedCheckerBalanceRepo) GetTransaction(_ string) (*balance.Transaction, error) {
	return nil, nil
}
func (m *cachedCheckerBalanceRepo) ListTransactions(_ balance.ListTransactionsInput) (*shared.PaginatedResult[*balance.Transaction], error) {
	return nil, nil
}
func (m *cachedCheckerBalanceRepo) ExistsTransactionByReferenceID(_ string) (bool, error) {
	return false, nil
}
func (m *cachedCheckerBalanceRepo) AggregateDailyCosts(date time.Time) ([]balance.DailyCostRow, error) {
	return nil, nil
}

type mockSharedState struct {
	store map[string]string
}

func newMockShared() *mockSharedState {
	return &mockSharedState{store: make(map[string]string)}
}

func (m *mockSharedState) SetString(key string, value string, _ time.Duration) error {
	m.store[key] = value
	return nil
}
func (m *mockSharedState) GetString(key string) (string, error) {
	v, ok := m.store[key]
	if !ok {
		return "", nil
	}
	return v, nil
}
func (m *mockSharedState) Del(keys ...string) error {
	for _, k := range keys {
		delete(m.store, k)
	}
	return nil
}

func (m *mockSharedState) SetNX(_ string, _ string, _ time.Duration) (bool, error) {
	return false, nil
}
func (m *mockSharedState) Exists(_ string) (bool, error) { return false, nil }
func (m *mockSharedState) Incr(_ string) (int64, error)  { return 0, nil }
func (m *mockSharedState) Decr(_ string) (int64, error)  { return 0, nil }
func (m *mockSharedState) IncrWithTTL(_ string, _ time.Duration) (int64, error) {
	return 0, nil
}
func (m *mockSharedState) TryIncr(_ string, _ int64) (bool, error)               { return false, nil }
func (m *mockSharedState) SAdd(_ string, _ ...string) error                      { return nil }
func (m *mockSharedState) SRem(_ string, _ ...string) error                      { return nil }
func (m *mockSharedState) SMembers(_ string) ([]string, error)                   { return nil, nil }
func (m *mockSharedState) Publish(_ string, _ []byte) error                      { return nil }
func (m *mockSharedState) Subscribe(_ context.Context, _ string, _ func([]byte)) {}
func (m *mockSharedState) HSet(_ string, _ string, _ string) error               { return nil }
func (m *mockSharedState) HDel(_ string, _ string) error                         { return nil }
func (m *mockSharedState) HGetAll(_ string) (map[string]string, error)           { return nil, nil }
func (m *mockSharedState) HIncrBy(_ string, _ string, _ int64) (int64, error) {
	return 0, nil
}
func (m *mockSharedState) Expire(_ string, _ time.Duration) (bool, error) { return false, nil }
func (m *mockSharedState) IncrBy(_ string, _ int64) (int64, error)        { return 0, nil }
func (m *mockSharedState) DecrBy(_ string, _ int64) (int64, error)        { return 0, nil }
func (m *mockSharedState) TryIncrBy(_ string, _ int64, _ int64) (bool, error) {
	return false, nil
}

func TestCachedBalanceChecker_CacheMiss_ReadsDB(t *testing.T) {
	repo := &cachedCheckerBalanceRepo{bal: &balance.Balance{Amount: 500_000}}
	shared := newMockShared()
	checker := NewCachedBalanceChecker(repo, shared, 10*time.Second)

	ok, err := checker.HasSufficientBalance("ws-1", 100_000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected sufficient balance")
	}
	if repo.getCalls != 1 {
		t.Fatalf("expected 1 DB call, got %d", repo.getCalls)
	}

	cached, _ := shared.GetString("balance:cache:ws-1")
	if cached != "500000" {
		t.Fatalf("expected cached value '500000', got '%s'", cached)
	}
}

func TestCachedBalanceChecker_CacheHit_SkipsDB(t *testing.T) {
	repo := &cachedCheckerBalanceRepo{bal: &balance.Balance{Amount: 999}}
	shared := newMockShared()

	_ = shared.SetString("balance:cache:ws-2", "1000000", 10*time.Second)

	checker := NewCachedBalanceChecker(repo, shared, 10*time.Second)

	ok, err := checker.HasSufficientBalance("ws-2", 500_000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected sufficient balance from cache")
	}
	if repo.getCalls != 0 {
		t.Fatalf("expected 0 DB calls (cache hit), got %d", repo.getCalls)
	}
}

func TestCachedBalanceChecker_InsufficientBalance(t *testing.T) {
	repo := &cachedCheckerBalanceRepo{bal: &balance.Balance{Amount: 50_000}}
	shared := newMockShared()
	checker := NewCachedBalanceChecker(repo, shared, 10*time.Second)

	ok, err := checker.HasSufficientBalance("ws-3", 100_000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected insufficient balance")
	}
}

func TestCachedBalanceChecker_InsufficientFromCache(t *testing.T) {
	repo := &cachedCheckerBalanceRepo{}
	shared := newMockShared()
	_ = shared.SetString("balance:cache:ws-4", "30000", 10*time.Second)

	checker := NewCachedBalanceChecker(repo, shared, 10*time.Second)

	ok, err := checker.HasSufficientBalance("ws-4", 100_000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected insufficient balance from cache")
	}
	if repo.getCalls != 0 {
		t.Fatalf("expected 0 DB calls, got %d", repo.getCalls)
	}
}

func TestCachedBalanceChecker_Invalidate_DeletesKey(t *testing.T) {
	repo := &cachedCheckerBalanceRepo{bal: &balance.Balance{Amount: 200_000}}
	shared := newMockShared()
	_ = shared.SetString("balance:cache:ws-5", "999999", 10*time.Second)

	checker := NewCachedBalanceChecker(repo, shared, 10*time.Second)
	checker.Invalidate("ws-5")

	ok, err := checker.HasSufficientBalance("ws-5", 100_000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected sufficient balance after invalidation + DB read")
	}
	if repo.getCalls != 1 {
		t.Fatalf("expected 1 DB call after invalidation, got %d", repo.getCalls)
	}
}

func TestCachedBalanceChecker_DBError_Propagated(t *testing.T) {
	repo := &cachedCheckerBalanceRepo{err: errors.New("db down")}
	shared := newMockShared()
	checker := NewCachedBalanceChecker(repo, shared, 10*time.Second)

	_, err := checker.HasSufficientBalance("ws-6", 1)
	if err == nil {
		t.Fatal("expected error from DB")
	}
}

func TestCachedBalanceChecker_BalanceNotFound(t *testing.T) {
	repo := &cachedCheckerBalanceRepo{err: balance.ErrBalanceNotFound}
	shared := newMockShared()
	checker := NewCachedBalanceChecker(repo, shared, 10*time.Second)

	_, err := checker.HasSufficientBalance("ws-7", 1)
	if err == nil {
		t.Fatal("expected ErrBalanceNotFound")
	}
	if !errors.Is(err, balance.ErrBalanceNotFound) {
		t.Fatalf("expected ErrBalanceNotFound, got: %v", err)
	}
}

func TestCachedBalanceChecker_CorruptedCache_FallsBackToDB(t *testing.T) {
	repo := &cachedCheckerBalanceRepo{bal: &balance.Balance{Amount: 300_000}}
	shared := newMockShared()
	_ = shared.SetString("balance:cache:ws-8", "not-a-number", 10*time.Second)

	checker := NewCachedBalanceChecker(repo, shared, 10*time.Second)

	ok, err := checker.HasSufficientBalance("ws-8", 100_000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected sufficient balance after corrupted cache fallback")
	}
	if repo.getCalls != 1 {
		t.Fatalf("expected 1 DB call for corrupted cache, got %d", repo.getCalls)
	}
}

func TestCachedBalanceChecker_ZeroBalance(t *testing.T) {
	repo := &cachedCheckerBalanceRepo{bal: &balance.Balance{Amount: 0}}
	shared := newMockShared()
	checker := NewCachedBalanceChecker(repo, shared, 10*time.Second)

	ok, err := checker.HasSufficientBalance("ws-9", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected insufficient balance for zero amount")
	}
}

func TestCachedBalanceChecker_ExactBalance(t *testing.T) {
	repo := &cachedCheckerBalanceRepo{bal: &balance.Balance{Amount: 100_000}}
	shared := newMockShared()
	checker := NewCachedBalanceChecker(repo, shared, 10*time.Second)

	ok, err := checker.HasSufficientBalance("ws-10", 100_000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected sufficient when balance == amount")
	}
}

func TestCachedBalanceChecker_DefaultTTL(t *testing.T) {
	repo := &cachedCheckerBalanceRepo{bal: &balance.Balance{Amount: 100_000}}
	shared := newMockShared()

	checker := NewCachedBalanceChecker(repo, shared, 0)

	ok, err := checker.HasSufficientBalance("ws-11", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected sufficient balance")
	}
}

func TestCachedBalanceChecker_InvalidateDebounced_Invalidates(t *testing.T) {
	repo := &cachedCheckerBalanceRepo{bal: &balance.Balance{Amount: 200_000}}
	shared := newMockShared()
	_ = shared.SetString("balance:cache:ws-d1", "999999", 10*time.Second)

	checker := NewCachedBalanceChecker(repo, shared, 10*time.Second)
	checker.InvalidateDebounced("ws-d1")

	v, _ := shared.GetString("balance:cache:ws-d1")
	if v != "" {
		t.Fatalf("expected cache cleared but got %q", v)
	}
}

func TestCachedBalanceChecker_InvalidateDebounced_SkipsTooSoon(t *testing.T) {
	repo := &cachedCheckerBalanceRepo{bal: &balance.Balance{Amount: 200_000}}
	shared := newMockShared()

	checker := NewCachedBalanceChecker(repo, shared, 10*time.Second)

	_ = shared.SetString("balance:cache:ws-d2", "111", 10*time.Second)
	checker.InvalidateDebounced("ws-d2")

	v, _ := shared.GetString("balance:cache:ws-d2")
	if v != "" {
		t.Fatal("first invalidation should clear cache")
	}

	_ = shared.SetString("balance:cache:ws-d2", "222", 10*time.Second)

	checker.InvalidateDebounced("ws-d2")

	v, _ = shared.GetString("balance:cache:ws-d2")
	if v != "222" {
		t.Fatal("second call within debounce interval should NOT clear cache")
	}
}

func TestCachedBalanceChecker_GetBalance_CacheHit(t *testing.T) {
	repo := &cachedCheckerBalanceRepo{bal: &balance.Balance{Amount: 999}}
	shared := newMockShared()
	_ = shared.SetString("balance:cache:ws-g1", "500000", 10*time.Second)

	checker := NewCachedBalanceChecker(repo, shared, 10*time.Second)

	val, err := checker.GetBalance("ws-g1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 500_000 {
		t.Errorf("GetBalance = %d, want 500000 (from cache)", val)
	}
	if repo.getCalls != 0 {
		t.Fatalf("expected 0 DB calls (cache hit), got %d", repo.getCalls)
	}
}

func TestCachedBalanceChecker_GetBalance_CacheMiss(t *testing.T) {
	repo := &cachedCheckerBalanceRepo{bal: &balance.Balance{Amount: 300_000}}
	shared := newMockShared()

	checker := NewCachedBalanceChecker(repo, shared, 10*time.Second)

	val, err := checker.GetBalance("ws-g2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 300_000 {
		t.Errorf("GetBalance = %d, want 300000 (from DB)", val)
	}
	if repo.getCalls != 1 {
		t.Fatalf("expected 1 DB call (cache miss), got %d", repo.getCalls)
	}

	cached, _ := shared.GetString("balance:cache:ws-g2")
	if cached != "300000" {
		t.Fatalf("expected cached '300000', got '%s'", cached)
	}
}

func TestCachedBalanceChecker_GetBalance_DBError(t *testing.T) {
	repo := &cachedCheckerBalanceRepo{err: errors.New("db down")}
	shared := newMockShared()

	checker := NewCachedBalanceChecker(repo, shared, 10*time.Second)

	_, err := checker.GetBalance("ws-g3")
	if err == nil {
		t.Fatal("expected error from DB")
	}
}

func TestCachedBalanceChecker_GetBalance_CorruptedCache(t *testing.T) {
	repo := &cachedCheckerBalanceRepo{bal: &balance.Balance{Amount: 400_000}}
	shared := newMockShared()
	_ = shared.SetString("balance:cache:ws-g4", "not-a-number", 10*time.Second)

	checker := NewCachedBalanceChecker(repo, shared, 10*time.Second)

	val, err := checker.GetBalance("ws-g4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 400_000 {
		t.Errorf("GetBalance = %d, want 400000 (fallback to DB)", val)
	}
	if repo.getCalls != 1 {
		t.Fatalf("expected 1 DB call for corrupted cache, got %d", repo.getCalls)
	}
}
