package balance_usecase

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"vozko/domain/balance"
	"vozko/domain/shared"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

type memBalanceRepo struct {
	mu       sync.Mutex
	balances map[string]*balance.Balance
	txs      []*balance.Transaction
	txSeq    int
}

func newMemBalanceRepo() *memBalanceRepo {
	return &memBalanceRepo{
		balances: make(map[string]*balance.Balance),
	}
}

func (m *memBalanceRepo) Create(b *balance.Balance) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.balances[b.WorkspaceID]; exists {
		return balance.ErrWorkspaceAlreadyHasBalance
	}
	m.balances[b.WorkspaceID] = b
	return nil
}

func (m *memBalanceRepo) GetByWorkspaceID(wsID string) (*balance.Balance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if b, ok := m.balances[wsID]; ok {
		return b, nil
	}
	return nil, balance.ErrBalanceNotFound
}

func (m *memBalanceRepo) EnsureBalanceExists(wsID, currency string) (*balance.Balance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if b, ok := m.balances[wsID]; ok {
		return b, nil
	}
	b := &balance.Balance{ID: "bal-" + wsID, WorkspaceID: wsID, Amount: 0, Currency: currency}
	m.balances[wsID] = b
	return b, nil
}

func (m *memBalanceRepo) CreditBalance(p balance.CreditBalanceInput) (*balance.Transaction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.balances[p.WorkspaceID]
	if !ok {
		return nil, balance.ErrBalanceNotFound
	}
	before := b.Amount
	b.Amount += p.Amount
	m.txSeq++
	exRate := p.ExchangeRateMicros
	if exRate == 0 {
		exRate = 6_000_000
	}
	tx := &balance.Transaction{
		ID:                 fmt.Sprintf("tx-%d", m.txSeq),
		WorkspaceID:        p.WorkspaceID,
		Type:               balance.TransactionTypeCredit,
		Amount:             p.Amount,
		BalanceBefore:      before,
		BalanceAfter:       b.Amount,
		ServiceType:        p.ServiceType,
		ReferenceID:        p.ReferenceID,
		Description:        p.Description,
		CostMicros:         p.CostMicros,
		ProfitMicros:       p.ProfitMicros,
		ExchangeRateMicros: exRate,
		IsRefund:           p.IsRefund,
	}
	m.txs = append(m.txs, tx)
	return tx, nil
}

func (m *memBalanceRepo) DebitBalance(p balance.DebitBalanceInput) (*balance.Transaction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.balances[p.WorkspaceID]
	if !ok {
		return nil, balance.ErrBalanceNotFound
	}
	if !p.AllowNegative && b.Amount < p.Amount {
		return nil, balance.ErrInsufficientBalance
	}
	before := b.Amount
	b.Amount -= p.Amount
	m.txSeq++
	exRate := p.ExchangeRateMicros
	if exRate == 0 {
		exRate = 6_000_000
	}
	tx := &balance.Transaction{
		ID:                 fmt.Sprintf("tx-%d", m.txSeq),
		WorkspaceID:        p.WorkspaceID,
		Type:               balance.TransactionTypeDebit,
		Amount:             p.Amount,
		BalanceBefore:      before,
		BalanceAfter:       b.Amount,
		ServiceType:        p.ServiceType,
		ReferenceID:        p.ReferenceID,
		Description:        p.Description,
		CostMicros:         p.CostMicros,
		ProfitMicros:       p.ProfitMicros,
		ExchangeRateMicros: exRate,
	}
	m.txs = append(m.txs, tx)
	return tx, nil
}

func (m *memBalanceRepo) HasSufficientBalance(wsID string, amount int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.balances[wsID]
	if !ok {
		return false, balance.ErrBalanceNotFound
	}
	return b.Amount >= amount, nil
}

func (m *memBalanceRepo) GetFullBalanceSummary(wsID string) (*balance.FullBalanceSummary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.balances[wsID]
	if !ok {
		return nil, balance.ErrBalanceNotFound
	}
	var credits, debits int64
	for _, tx := range m.txs {
		if tx.WorkspaceID != wsID {
			continue
		}
		if tx.Type == balance.TransactionTypeCredit {
			credits += tx.Amount
		} else {
			debits += tx.Amount
		}
	}
	return &balance.FullBalanceSummary{Balance: b, TotalMoneyCredits: credits, TotalMoneyDebits: debits}, nil
}

func (m *memBalanceRepo) GetTransaction(txID string) (*balance.Transaction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, tx := range m.txs {
		if tx.ID == txID {
			return tx, nil
		}
	}
	return nil, balance.ErrTransactionNotFound
}

func (m *memBalanceRepo) ListTransactions(input balance.ListTransactionsInput) (*shared.PaginatedResult[*balance.Transaction], error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*balance.Transaction
	for _, tx := range m.txs {
		if tx.WorkspaceID == input.WorkspaceID {
			result = append(result, tx)
		}
	}
	return &shared.PaginatedResult[*balance.Transaction]{Items: result, TotalItems: int64(len(result))}, nil
}

func (m *memBalanceRepo) ExistsTransactionByReferenceID(refID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, tx := range m.txs {
		if tx.ReferenceID != nil && *tx.ReferenceID == refID {
			return true, nil
		}
	}
	return false, nil
}

func (m *memBalanceRepo) AggregateDailyCosts(date time.Time) ([]balance.DailyCostRow, error) {
	return nil, nil
}

type memSharedState struct {
	mu   sync.Mutex
	data map[string]string
}

func newMemSharedState() *memSharedState {
	return &memSharedState{data: make(map[string]string)}
}

func (m *memSharedState) SetNX(key, value string, _ time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.data[key]; exists {
		return false, nil
	}
	m.data[key] = value
	return true, nil
}
func (m *memSharedState) SetString(key, value string, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
	return nil
}
func (m *memSharedState) GetString(key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.data[key], nil
}
func (m *memSharedState) Del(keys ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range keys {
		delete(m.data, k)
	}
	return nil
}
func (m *memSharedState) Exists(key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.data[key]
	return ok, nil
}
func (m *memSharedState) Incr(key string) (int64, error)                         { return 0, nil }
func (m *memSharedState) Decr(key string) (int64, error)                         { return 0, nil }
func (m *memSharedState) IncrWithTTL(key string, _ time.Duration) (int64, error) { return 0, nil }
func (m *memSharedState) TryIncr(key string, max int64) (bool, error)            { return true, nil }
func (m *memSharedState) SAdd(string, ...string) error                           { return nil }
func (m *memSharedState) SRem(string, ...string) error                           { return nil }
func (m *memSharedState) SMembers(string) ([]string, error)                      { return nil, nil }
func (m *memSharedState) Publish(string, []byte) error                           { return nil }
func (m *memSharedState) Subscribe(context.Context, string, func([]byte))        {}
func (m *memSharedState) HSet(string, string, string) error                      { return nil }
func (m *memSharedState) HDel(string, string) error                              { return nil }
func (m *memSharedState) HGetAll(string) (map[string]string, error)              { return nil, nil }
func (m *memSharedState) HIncrBy(string, string, int64) (int64, error)           { return 0, nil }
func (m *memSharedState) IncrBy(key string, amount int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, _ := strconv.ParseInt(m.data[key], 10, 64)
	cur += amount
	m.data[key] = strconv.FormatInt(cur, 10)
	return cur, nil
}
func (m *memSharedState) DecrBy(key string, amount int64) (int64, error) {
	return m.IncrBy(key, -amount)
}
func (m *memSharedState) TryIncrBy(key string, delta, max int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, _ := strconv.ParseInt(m.data[key], 10, 64)
	if cur+delta > max {
		return false, nil
	}
	m.data[key] = strconv.FormatInt(cur+delta, 10)
	return true, nil
}
func (m *memSharedState) Expire(key string, _ time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.data[key]
	return ok, nil
}

func TestCreateBalance_Success(t *testing.T) {
	repo := newMemBalanceRepo()
	uc := NewCreateBalanceUseCase(repo)

	b, err := uc.Execute(balance.CreateBalanceInput{WorkspaceID: "ws1", InitialAmount: 1000, Currency: "USD"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.WorkspaceID != "ws1" || b.Amount != 1000 || b.Currency != "USD" {
		t.Errorf("got %+v", b)
	}
}

func TestCreateBalance_DefaultCurrency(t *testing.T) {
	repo := newMemBalanceRepo()
	uc := NewCreateBalanceUseCase(repo)

	b, err := uc.Execute(balance.CreateBalanceInput{WorkspaceID: "ws1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.Currency != "USD" {
		t.Errorf("currency = %q, want USD", b.Currency)
	}
}

func TestCreateBalance_NegativeAmount(t *testing.T) {
	repo := newMemBalanceRepo()
	uc := NewCreateBalanceUseCase(repo)

	_, err := uc.Execute(balance.CreateBalanceInput{WorkspaceID: "ws1", InitialAmount: -1})
	if !errors.Is(err, balance.ErrInvalidAmount) {
		t.Errorf("err = %v, want ErrInvalidAmount", err)
	}
}

func TestCreateBalance_Duplicate(t *testing.T) {
	repo := newMemBalanceRepo()
	uc := NewCreateBalanceUseCase(repo)

	_, _ = uc.Execute(balance.CreateBalanceInput{WorkspaceID: "ws1"})
	_, err := uc.Execute(balance.CreateBalanceInput{WorkspaceID: "ws1"})
	if !errors.Is(err, balance.ErrWorkspaceAlreadyHasBalance) {
		t.Errorf("err = %v, want ErrWorkspaceAlreadyHasBalance", err)
	}
}

func TestCreditBalance_DelegatesToRepo(t *testing.T) {
	repo := newMemBalanceRepo()
	repo.balances["ws1"] = &balance.Balance{ID: "b1", WorkspaceID: "ws1", Amount: 100}
	uc := NewCreditBalanceUseCase(repo)

	tx, err := uc.Execute(balance.CreditBalanceInput{WorkspaceID: "ws1", Amount: 50, ServiceType: balance.ServiceTopUp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.Amount != 50 || tx.BalanceAfter != 150 {
		t.Errorf("tx = %+v", tx)
	}
}

func TestDebitBalance_DelegatesToRepo(t *testing.T) {
	repo := newMemBalanceRepo()
	repo.balances["ws1"] = &balance.Balance{ID: "b1", WorkspaceID: "ws1", Amount: 100}
	uc := NewDebitBalanceUseCase(repo)

	tx, err := uc.Execute(balance.DebitBalanceInput{WorkspaceID: "ws1", Amount: 30, ServiceType: balance.ServiceAI})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.Amount != 30 || tx.BalanceAfter != 70 {
		t.Errorf("tx = %+v", tx)
	}
}

func TestDebitBalance_InsufficientBalance(t *testing.T) {
	repo := newMemBalanceRepo()
	repo.balances["ws1"] = &balance.Balance{ID: "b1", WorkspaceID: "ws1", Amount: 10}
	uc := NewDebitBalanceUseCase(repo)

	_, err := uc.Execute(balance.DebitBalanceInput{WorkspaceID: "ws1", Amount: 100})
	if !errors.Is(err, balance.ErrInsufficientBalance) {
		t.Errorf("err = %v, want ErrInsufficientBalance", err)
	}
}

func TestCreditResource_Success(t *testing.T) {
	repo := newMemBalanceRepo()
	repo.balances["ws1"] = &balance.Balance{ID: "b1", WorkspaceID: "ws1", Amount: 100}
	uc := NewCreditResourceUseCase(repo)

	tx, err := uc.Execute(balance.CreditBalanceInput{WorkspaceID: "ws1", Amount: 50, ServiceType: balance.ServiceTopUp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.BalanceAfter != 150 {
		t.Errorf("BalanceAfter = %d, want 150", tx.BalanceAfter)
	}
}

func TestCreditResource_InvalidAmount(t *testing.T) {
	repo := newMemBalanceRepo()
	uc := NewCreditResourceUseCase(repo)

	_, err := uc.Execute(balance.CreditBalanceInput{WorkspaceID: "ws1", Amount: 0})
	if !errors.Is(err, balance.ErrInvalidAmount) {
		t.Errorf("err = %v, want ErrInvalidAmount", err)
	}
}

func TestDebitResource_Success(t *testing.T) {
	repo := newMemBalanceRepo()
	repo.balances["ws1"] = &balance.Balance{ID: "b1", WorkspaceID: "ws1", Amount: 100}
	uc := NewDebitResourceUseCase(repo)

	tx, err := uc.Execute(balance.DebitBalanceInput{WorkspaceID: "ws1", Amount: 30})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.BalanceAfter != 70 {
		t.Errorf("BalanceAfter = %d, want 70", tx.BalanceAfter)
	}
}

func TestDebitResource_InvalidAmount(t *testing.T) {
	repo := newMemBalanceRepo()
	uc := NewDebitResourceUseCase(repo)

	_, err := uc.Execute(balance.DebitBalanceInput{WorkspaceID: "ws1", Amount: 0})
	if !errors.Is(err, balance.ErrInvalidAmount) {
		t.Errorf("err = %v, want ErrInvalidAmount", err)
	}
}

func TestGetBalance_Success(t *testing.T) {
	repo := newMemBalanceRepo()
	repo.balances["ws1"] = &balance.Balance{ID: "b1", WorkspaceID: "ws1", Amount: 500}
	uc := NewGetBalanceUseCase(repo)

	b, err := uc.Execute("ws1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.Amount != 500 {
		t.Errorf("Amount = %d, want 500", b.Amount)
	}
}

func TestGetBalance_NotFound(t *testing.T) {
	repo := newMemBalanceRepo()
	uc := NewGetBalanceUseCase(repo)

	_, err := uc.Execute("nonexistent")
	if !errors.Is(err, balance.ErrBalanceNotFound) {
		t.Errorf("err = %v, want ErrBalanceNotFound", err)
	}
}

func TestGetFullBalanceSummary_Success(t *testing.T) {
	repo := newMemBalanceRepo()
	repo.balances["ws1"] = &balance.Balance{ID: "b1", WorkspaceID: "ws1", Amount: 100}
	uc := NewGetFullBalanceSummaryUseCase(repo)

	s, err := uc.Execute("ws1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Balance.Amount != 100 {
		t.Errorf("Amount = %d, want 100", s.Balance.Amount)
	}
}

func TestGetOrCreateBalance_CreatesNew(t *testing.T) {
	repo := newMemBalanceRepo()
	uc := NewGetOrCreateBalanceUseCase(repo)

	b, err := uc.Execute("ws-new")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.WorkspaceID != "ws-new" || b.Currency != "USD" {
		t.Errorf("got %+v", b)
	}
}

func TestGetOrCreateBalance_ReturnsExisting(t *testing.T) {
	repo := newMemBalanceRepo()
	repo.balances["ws1"] = &balance.Balance{ID: "b1", WorkspaceID: "ws1", Amount: 999}
	uc := NewGetOrCreateBalanceUseCase(repo)

	b, err := uc.Execute("ws1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.Amount != 999 {
		t.Errorf("Amount = %d, want 999", b.Amount)
	}
}

func TestGetOrCreateFullBalanceSummary_Success(t *testing.T) {
	repo := newMemBalanceRepo()
	uc := NewGetOrCreateFullBalanceSummaryUseCase(repo)

	s, err := uc.Execute("ws-new")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Balance.WorkspaceID != "ws-new" {
		t.Errorf("WorkspaceID = %q, want ws-new", s.Balance.WorkspaceID)
	}
}

func TestListTransactions_Success(t *testing.T) {
	repo := newMemBalanceRepo()
	repo.balances["ws1"] = &balance.Balance{ID: "b1", WorkspaceID: "ws1", Amount: 100}

	creditUC := NewCreditBalanceUseCase(repo)
	_, _ = creditUC.Execute(balance.CreditBalanceInput{WorkspaceID: "ws1", Amount: 50, ServiceType: balance.ServiceTopUp})
	debitUC := NewDebitBalanceUseCase(repo)
	_, _ = debitUC.Execute(balance.DebitBalanceInput{WorkspaceID: "ws1", Amount: 20, ServiceType: balance.ServiceAI})

	listUC := NewListTransactionsUseCase(repo)
	result, err := listUC.Execute(balance.ListTransactionsInput{WorkspaceID: "ws1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TotalItems != 2 {
		t.Errorf("TotalItems = %d, want 2", result.TotalItems)
	}
}

func TestCheckBalance_Sufficient(t *testing.T) {
	repo := newMemBalanceRepo()
	repo.balances["ws1"] = &balance.Balance{ID: "b1", WorkspaceID: "ws1", Amount: 100}
	uc := NewCheckBalanceUseCase(repo)

	ok, err := uc.Execute("ws1", 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected sufficient balance")
	}
}

func TestCheckBalance_Insufficient(t *testing.T) {
	repo := newMemBalanceRepo()
	repo.balances["ws1"] = &balance.Balance{ID: "b1", WorkspaceID: "ws1", Amount: 10}
	uc := NewCheckBalanceUseCase(repo)

	ok, err := uc.Execute("ws1", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected insufficient balance")
	}
}

func TestInflightReserver_ReserveAndRelease(t *testing.T) {
	shared := newMemSharedState()
	r := NewInflightReserver(shared)

	ok, err := r.Reserve("ws1", 100, 500)
	if err != nil || !ok {
		t.Fatalf("Reserve failed: ok=%v err=%v", ok, err)
	}

	inflight, err := r.GetInflight("ws1")
	if err != nil {
		t.Fatalf("GetInflight error: %v", err)
	}
	if inflight != 100 {
		t.Errorf("inflight = %d, want 100", inflight)
	}

	err = r.Release("ws1", 100)
	if err != nil {
		t.Fatalf("Release error: %v", err)
	}
	inflight, _ = r.GetInflight("ws1")
	if inflight != 0 {
		t.Errorf("inflight after release = %d, want 0", inflight)
	}
}

func TestInflightReserver_ExceedsBudget(t *testing.T) {
	shared := newMemSharedState()
	r := NewInflightReserver(shared)

	ok, _ := r.Reserve("ws1", 600, 500)
	if ok {
		t.Error("expected reservation to fail when exceeding budget")
	}
}

func TestInflightReserver_RefreshTTL(t *testing.T) {
	shared := newMemSharedState()
	r := NewInflightReserver(shared)

	_ = shared.SetString("balance:inflight:ws1", "100", 0)
	err := r.RefreshTTL("ws1", 5*time.Second)
	if err != nil {
		t.Fatalf("RefreshTTL error: %v", err)
	}
}

func TestInflightReserver_NilSharedState(t *testing.T) {
	r := NewInflightReserver(nil)

	_, err := r.Reserve("ws1", 100, 500)
	if err == nil {
		t.Error("expected error with nil shared state")
	}

	err = r.Release("ws1", 100)
	if err == nil {
		t.Error("expected error with nil shared state")
	}

	err = r.RefreshTTL("ws1", time.Second)
	if err == nil {
		t.Error("expected error with nil shared state")
	}

	_, err = r.GetInflight("ws1")
	if err == nil {
		t.Error("expected error with nil shared state")
	}
}

func TestInflightReserver_GetInflight_NoKey(t *testing.T) {
	shared := newMemSharedState()
	r := NewInflightReserver(shared)

	inflight, err := r.GetInflight("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inflight != 0 {
		t.Errorf("inflight = %d, want 0", inflight)
	}
}

func TestGetTemplateCostMicros_Utility(t *testing.T) {
	pricer := workspace_pricing.NewPricer(&waMockPricingRepo{})
	uc := NewConsumeWhatsappTemplateUseCase(newWAMockBalanceRepo(), pricer, &allowAllSubscriptionChecker{})

	cost, err := uc.GetTemplateCostMicros("ws1", "UTILITY")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cat := findWACatalogItem("utility")
	if cost != cat.PriceMicros {
		t.Errorf("cost = %d, want %d", cost, cat.PriceMicros)
	}
}

func TestGetTemplateCostMicros_NoSubscription(t *testing.T) {
	pricer := workspace_pricing.NewPricer(&waMockPricingRepo{})
	checker := &allowAllSubscriptionChecker{err: errors.New("subscription required")}
	uc := NewConsumeWhatsappTemplateUseCase(newWAMockBalanceRepo(), pricer, checker)

	_, err := uc.GetTemplateCostMicros("ws1", "UTILITY")
	if err == nil {
		t.Fatal("expected error when no subscription")
	}
}

func TestWhatsApp_Execute_NoSubscription(t *testing.T) {
	pricer := workspace_pricing.NewPricer(&waMockPricingRepo{})
	checker := &allowAllSubscriptionChecker{err: errors.New("subscription required")}
	uc := NewConsumeWhatsappTemplateUseCase(newWAMockBalanceRepo(), pricer, checker)

	_, err := uc.Execute("ws1", "ref1", "UTILITY")
	if err == nil {
		t.Fatal("expected error when no subscription")
	}
}

func TestWhatsApp_Execute_NilChecker(t *testing.T) {
	pricer := workspace_pricing.NewPricer(&waMockPricingRepo{})
	uc := NewConsumeWhatsappTemplateUseCase(newWAMockBalanceRepo(), pricer, nil)

	_, err := uc.Execute("ws1", "ref1", "UTILITY")
	if err == nil {
		t.Fatal("expected error when checker is nil")
	}
}

type errPricingRepo struct{ err error }

func (m *errPricingRepo) ListDefaultPricingItems() ([]workspace_pricing.PricingItem, error) {
	return nil, m.err
}
func (m *errPricingRepo) GetPricingItem(string) (*workspace_pricing.PricingItem, error) {
	return nil, nil
}
func (m *errPricingRepo) UpsertPricingItem(*workspace_pricing.PricingItem) error      { return nil }
func (m *errPricingRepo) DeletePricingItem(string) error                              { return nil }
func (m *errPricingRepo) SeedDefaults([]workspace_pricing.PricingItem) error          { return nil }
func (m *errPricingRepo) CreateAuditEntry(*workspace_pricing.PricingAuditEntry) error { return nil }
func (m *errPricingRepo) ListAuditEntries(*string, int, int) ([]workspace_pricing.PricingAuditEntry, error) {
	return nil, nil
}

func TestGetTemplateCostMicros_PricerError(t *testing.T) {
	pricer := workspace_pricing.NewPricer(&errPricingRepo{err: errors.New("db down")})
	uc := NewConsumeWhatsappTemplateUseCase(newWAMockBalanceRepo(), pricer, &allowAllSubscriptionChecker{})

	_, err := uc.GetTemplateCostMicros("ws1", "UTILITY")
	if err == nil {
		t.Fatal("expected error when pricer fails")
	}
}

func TestWhatsApp_Refund_PricerError(t *testing.T) {
	pricer := workspace_pricing.NewPricer(&errPricingRepo{err: errors.New("db down")})
	uc := NewConsumeWhatsappTemplateUseCase(newWAMockBalanceRepo(), pricer, &allowAllSubscriptionChecker{})

	err := uc.Refund("ws1", "ref1", "UTILITY")
	if err == nil {
		t.Fatal("expected error when pricer fails on refund")
	}
}

func TestWhatsApp_Execute_ZeroPrice(t *testing.T) {

	emptyRepo := &errPricingRepo{err: nil}
	pricer := workspace_pricing.NewPricer(emptyRepo)
	uc := NewConsumeWhatsappTemplateUseCase(newWAMockBalanceRepo(), pricer, &allowAllSubscriptionChecker{})

	// INVERTED. This asserted that a zero price returns (nil, nil) — a
	// success-shaped answer that charged nothing, which every caller read as
	// "billed" and none read as "this went out for free". An unpriced workspace
	// must STOP.
	tx, err := uc.Execute("ws1", "ref1", "UTILITY")
	if !errors.Is(err, balance.ErrPriceUnavailable) {
		t.Fatalf("an unpriced template must be refused, got err=%v", err)
	}
	if tx != nil {
		t.Error("nothing may be charged when there is no price")
	}
}

func TestWhatsApp_Execute_PricerError(t *testing.T) {
	pricer := workspace_pricing.NewPricer(&errPricingRepo{err: errors.New("db down")})
	uc := NewConsumeWhatsappTemplateUseCase(newWAMockBalanceRepo(), pricer, &allowAllSubscriptionChecker{})

	_, err := uc.Execute("ws1", "ref1", "UTILITY")
	if err == nil {
		t.Fatal("expected error when pricer fails on execute")
	}
}

func TestWhatsApp_Refund_ZeroPrice(t *testing.T) {
	emptyRepo := &errPricingRepo{err: nil}
	pricer := workspace_pricing.NewPricer(emptyRepo)
	uc := NewConsumeWhatsappTemplateUseCase(newWAMockBalanceRepo(), pricer, &allowAllSubscriptionChecker{})

	// INVERTED, for the mirror reason. A silent nil reported a refund that never
	// happened, and the caller then recorded the attempt as refunded — the
	// customer's money kept, with a record saying it was returned.
	if err := uc.Refund("ws1", "ref1", "UTILITY"); !errors.Is(err, balance.ErrPriceUnavailable) {
		t.Fatalf("a refund that cannot be priced must report failure, got err=%v", err)
	}
}

func TestCreditResource_RepoError(t *testing.T) {
	repo := newMemBalanceRepo()
	uc := NewCreditResourceUseCase(repo)

	_, err := uc.Execute(balance.CreditBalanceInput{WorkspaceID: "ws1", Amount: 50})
	if err == nil {
		t.Fatal("expected error when workspace has no balance")
	}
}

type errEnsureRepo struct{ memBalanceRepo }

func (r *errEnsureRepo) EnsureBalanceExists(string, string) (*balance.Balance, error) {
	return nil, errors.New("db error")
}

func TestGetOrCreateFullBalanceSummary_EnsureError(t *testing.T) {
	uc := NewGetOrCreateFullBalanceSummaryUseCase(&errEnsureRepo{})
	_, err := uc.Execute("ws1")
	if err == nil {
		t.Fatal("expected error when EnsureBalanceExists fails")
	}
}

func TestCreditBalance_ExchangeRate_DefaultFallback(t *testing.T) {
	repo := newMemBalanceRepo()
	repo.balances["ws1"] = &balance.Balance{ID: "b1", WorkspaceID: "ws1", Amount: 0}
	uc := NewCreditBalanceUseCase(repo)

	tx, err := uc.Execute(balance.CreditBalanceInput{
		WorkspaceID: "ws1",
		Amount:      1_000_000,
		ServiceType: balance.ServiceTopUp,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tx.ExchangeRateMicros != 6_000_000 {
		t.Errorf("ExchangeRateMicros = %d, want 6_000_000", tx.ExchangeRateMicros)
	}
}

func TestCreditBalance_ExchangeRate_Explicit(t *testing.T) {
	repo := newMemBalanceRepo()
	repo.balances["ws1"] = &balance.Balance{ID: "b1", WorkspaceID: "ws1", Amount: 0}
	uc := NewCreditBalanceUseCase(repo)

	tx, err := uc.Execute(balance.CreditBalanceInput{
		WorkspaceID:        "ws1",
		Amount:             500_000,
		ServiceType:        balance.ServiceTopUp,
		ExchangeRateMicros: 5_500_000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.ExchangeRateMicros != 5_500_000 {
		t.Errorf("ExchangeRateMicros = %d, want 5_500_000", tx.ExchangeRateMicros)
	}
}

func TestDebitBalance_ExchangeRate_DefaultFallback(t *testing.T) {
	repo := newMemBalanceRepo()
	repo.balances["ws1"] = &balance.Balance{ID: "b1", WorkspaceID: "ws1", Amount: 10_000_000}
	uc := NewDebitBalanceUseCase(repo)

	tx, err := uc.Execute(balance.DebitBalanceInput{
		WorkspaceID: "ws1",
		Amount:      100_000,
		ServiceType: balance.ServiceAI,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.ExchangeRateMicros != 6_000_000 {
		t.Errorf("ExchangeRateMicros = %d, want 6_000_000", tx.ExchangeRateMicros)
	}
}

func TestDebitBalance_ExchangeRate_Explicit(t *testing.T) {
	repo := newMemBalanceRepo()
	repo.balances["ws1"] = &balance.Balance{ID: "b1", WorkspaceID: "ws1", Amount: 10_000_000}
	uc := NewDebitBalanceUseCase(repo)

	tx, err := uc.Execute(balance.DebitBalanceInput{
		WorkspaceID:        "ws1",
		Amount:             100_000,
		ServiceType:        balance.ServiceAI,
		ExchangeRateMicros: 7_200_000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.ExchangeRateMicros != 7_200_000 {
		t.Errorf("ExchangeRateMicros = %d, want 7_200_000", tx.ExchangeRateMicros)
	}
}
