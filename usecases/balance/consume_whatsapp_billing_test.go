package balance_usecase

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"vozko/domain/balance"
	"vozko/domain/shared"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

type waMockBalanceRepo struct {
	mu      sync.Mutex
	debits  []waDebitCall
	credits []waCreditCall
}

type waDebitCall struct {
	workspaceID  string
	amount       int64
	costMicros   int64
	profitMicros int64
	serviceType  balance.ServiceType
	referenceID  *string
}

type waCreditCall struct {
	workspaceID  string
	amount       int64
	costMicros   int64
	profitMicros int64
	serviceType  balance.ServiceType
	referenceID  *string
	isRefund     bool
}

func newWAMockBalanceRepo() *waMockBalanceRepo {
	return &waMockBalanceRepo{}
}

func (m *waMockBalanceRepo) Create(b *balance.Balance) error                        { return nil }
func (m *waMockBalanceRepo) GetByWorkspaceID(wsID string) (*balance.Balance, error) { return nil, nil }
func (m *waMockBalanceRepo) EnsureBalanceExists(wsID, currency string) (*balance.Balance, error) {
	return nil, nil
}
func (m *waMockBalanceRepo) DebitBalance(params balance.DebitBalanceInput) (*balance.Transaction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.debits = append(m.debits, waDebitCall{
		workspaceID:  params.WorkspaceID,
		amount:       params.Amount,
		costMicros:   params.CostMicros,
		profitMicros: params.ProfitMicros,
		serviceType:  params.ServiceType,
		referenceID:  params.ReferenceID,
	})
	return &balance.Transaction{
		ID:           "tx-" + params.WorkspaceID,
		Amount:       params.Amount,
		CostMicros:   params.CostMicros,
		ProfitMicros: params.ProfitMicros,
	}, nil
}
func (m *waMockBalanceRepo) CreditBalance(params balance.CreditBalanceInput) (*balance.Transaction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.credits = append(m.credits, waCreditCall{
		workspaceID:  params.WorkspaceID,
		amount:       params.Amount,
		costMicros:   params.CostMicros,
		profitMicros: params.ProfitMicros,
		serviceType:  params.ServiceType,
		referenceID:  params.ReferenceID,
		isRefund:     params.IsRefund,
	})
	return &balance.Transaction{
		ID:           "tx-refund-" + params.WorkspaceID,
		Amount:       params.Amount,
		CostMicros:   params.CostMicros,
		ProfitMicros: params.ProfitMicros,
		IsRefund:     params.IsRefund,
	}, nil
}
func (m *waMockBalanceRepo) HasSufficientBalance(wsID string, amount int64) (bool, error) {
	return true, nil
}
func (m *waMockBalanceRepo) GetFullBalanceSummary(wsID string) (*balance.FullBalanceSummary, error) {
	return nil, nil
}
func (m *waMockBalanceRepo) GetTransaction(txID string) (*balance.Transaction, error) {
	return nil, nil
}
func (m *waMockBalanceRepo) ListTransactions(input balance.ListTransactionsInput) (*shared.PaginatedResult[*balance.Transaction], error) {
	return nil, nil
}
func (m *waMockBalanceRepo) ExistsTransactionByReferenceID(refID string) (bool, error) {
	return false, nil
}
func (m *waMockBalanceRepo) AggregateDailyCosts(date time.Time) ([]balance.DailyCostRow, error) {
	return nil, nil
}

type waMockPricingRepo struct{}

func (m *waMockPricingRepo) ListDefaultPricingItems() ([]workspace_pricing.PricingItem, error) {
	return workspace_pricing.DefaultPricingCatalog, nil
}
func (m *waMockPricingRepo) GetPricingItem(id string) (*workspace_pricing.PricingItem, error) {
	return nil, nil
}
func (m *waMockPricingRepo) UpsertPricingItem(item *workspace_pricing.PricingItem) error { return nil }
func (m *waMockPricingRepo) DeletePricingItem(id string) error                           { return nil }
func (m *waMockPricingRepo) SeedDefaults(items []workspace_pricing.PricingItem) error    { return nil }
func (m *waMockPricingRepo) CreateAuditEntry(entry *workspace_pricing.PricingAuditEntry) error {
	return nil
}
func (m *waMockPricingRepo) ListAuditEntries(wsID *string, l, o int) ([]workspace_pricing.PricingAuditEntry, error) {
	return nil, nil
}

func newWATestPricer() workspace_pricing.Pricer {
	return workspace_pricing.NewPricer(&waMockPricingRepo{})
}

func findWACatalogItem(service string) workspace_pricing.PricingItem {
	for _, item := range workspace_pricing.DefaultPricingCatalog {
		if item.Category == workspace_pricing.CategoryWhatsApp && item.Service == service {
			return item
		}
	}
	panic("WhatsApp catalog item not found: " + service)
}

func TestWhatsAppBilling_UtilityProfitStored(t *testing.T) {
	balanceRepo := newWAMockBalanceRepo()
	pricer := newWATestPricer()
	uc := NewConsumeWhatsappTemplateUseCase(balanceRepo, pricer, &allowAllSubscriptionChecker{})

	_, err := uc.Execute("ws-wa", "entry-utility-1", "UTILITY")
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	balanceRepo.mu.Lock()
	defer balanceRepo.mu.Unlock()

	if len(balanceRepo.debits) != 1 {
		t.Fatalf("expected 1 debit, got %d", len(balanceRepo.debits))
	}
	d := balanceRepo.debits[0]

	catalog := findWACatalogItem("utility")

	if d.serviceType != balance.ServiceWhatsAppCampaign {
		t.Errorf("service = %q, want %q", d.serviceType, balance.ServiceWhatsAppCampaign)
	}
	if d.amount != catalog.PriceMicros {
		t.Errorf("charge = %d, want %d (utility catalog price)", d.amount, catalog.PriceMicros)
	}
	if d.costMicros != catalog.CostMicros {
		t.Errorf("cost = %d, want %d (utility catalog cost)", d.costMicros, catalog.CostMicros)
	}
	expectedProfit := catalog.PriceMicros - catalog.CostMicros
	if d.profitMicros != expectedProfit {
		t.Errorf("profit = %d, want %d", d.profitMicros, expectedProfit)
	}
	if d.profitMicros <= 0 {
		t.Error("profit must be positive for utility template")
	}
}

func TestWhatsAppBilling_MarketingProfitStored(t *testing.T) {
	balanceRepo := newWAMockBalanceRepo()
	pricer := newWATestPricer()
	uc := NewConsumeWhatsappTemplateUseCase(balanceRepo, pricer, &allowAllSubscriptionChecker{})

	_, err := uc.Execute("ws-wa", "entry-marketing-1", "MARKETING")
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	balanceRepo.mu.Lock()
	defer balanceRepo.mu.Unlock()

	if len(balanceRepo.debits) != 1 {
		t.Fatalf("expected 1 debit, got %d", len(balanceRepo.debits))
	}
	d := balanceRepo.debits[0]

	catalog := findWACatalogItem("marketing")

	if d.amount != catalog.PriceMicros {
		t.Errorf("charge = %d, want %d (marketing catalog price)", d.amount, catalog.PriceMicros)
	}
	if d.costMicros != catalog.CostMicros {
		t.Errorf("cost = %d, want %d", d.costMicros, catalog.CostMicros)
	}
	if d.profitMicros != catalog.PriceMicros-catalog.CostMicros {
		t.Errorf("profit = %d, want %d", d.profitMicros, catalog.PriceMicros-catalog.CostMicros)
	}

	utilityCatalog := findWACatalogItem("utility")
	if d.amount <= utilityCatalog.PriceMicros {
		t.Errorf("marketing charge (%d) should exceed utility charge (%d)", d.amount, utilityCatalog.PriceMicros)
	}
}

func TestWhatsAppBilling_RefundIsMarkedAndNegatesProfit(t *testing.T) {
	balanceRepo := newWAMockBalanceRepo()
	pricer := newWATestPricer()
	uc := NewConsumeWhatsappTemplateUseCase(balanceRepo, pricer, &allowAllSubscriptionChecker{})

	_, err := uc.Execute("ws-wa-refund", "entry-wa-ref-1", "UTILITY")
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	err = uc.Refund("ws-wa-refund", "entry-wa-ref-1", "UTILITY")
	if err != nil {
		t.Fatalf("Refund() error: %v", err)
	}

	balanceRepo.mu.Lock()
	defer balanceRepo.mu.Unlock()

	if len(balanceRepo.credits) != 1 {
		t.Fatalf("expected 1 refund credit, got %d", len(balanceRepo.credits))
	}
	c := balanceRepo.credits[0]

	if !c.isRefund {
		t.Error("refund credit must have isRefund = true")
	}
	if c.profitMicros >= 0 {
		t.Errorf("refund profit = %d, want < 0 (reversal)", c.profitMicros)
	}

	if len(balanceRepo.debits) != 1 {
		t.Fatalf("expected 1 original debit")
	}
	origProfit := balanceRepo.debits[0].profitMicros
	if c.profitMicros != -origProfit {
		t.Errorf("refund profit = %d, want -%d", c.profitMicros, origProfit)
	}
	if c.referenceID == nil || *c.referenceID != "refund:entry-wa-ref-1" {
		ref := ""
		if c.referenceID != nil {
			ref = *c.referenceID
		}
		t.Errorf("referenceID = %q, want \"refund:entry-wa-ref-1\"", ref)
	}
}

func TestWhatsAppBilling_MarketingRefundNegatesProfit(t *testing.T) {
	balanceRepo := newWAMockBalanceRepo()
	pricer := newWATestPricer()
	uc := NewConsumeWhatsappTemplateUseCase(balanceRepo, pricer, &allowAllSubscriptionChecker{})

	_, _ = uc.Execute("ws-wa-mkt-refund", "entry-mkt-1", "MARKETING")
	err := uc.Refund("ws-wa-mkt-refund", "entry-mkt-1", "MARKETING")
	if err != nil {
		t.Fatalf("Refund() error: %v", err)
	}

	balanceRepo.mu.Lock()
	defer balanceRepo.mu.Unlock()

	c := balanceRepo.credits[0]
	d := balanceRepo.debits[0]

	if !c.isRefund {
		t.Error("isRefund must be true")
	}
	if c.profitMicros != -d.profitMicros {
		t.Errorf("marketing refund profit = %d, want %d", c.profitMicros, -d.profitMicros)
	}
}

func TestWhatsAppBilling_AuthenticationProfitStored(t *testing.T) {
	balanceRepo := newWAMockBalanceRepo()
	pricer := newWATestPricer()
	uc := NewConsumeWhatsappTemplateUseCase(balanceRepo, pricer, &allowAllSubscriptionChecker{})

	_, err := uc.Execute("ws-wa-auth", "entry-auth-1", "AUTHENTICATION")
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	balanceRepo.mu.Lock()
	defer balanceRepo.mu.Unlock()

	if len(balanceRepo.debits) != 1 {
		t.Fatalf("expected 1 debit, got %d", len(balanceRepo.debits))
	}
	d := balanceRepo.debits[0]
	catalog := findWACatalogItem("authentication")

	if d.serviceType != balance.ServiceWhatsAppCampaign {
		t.Errorf("service = %q, want %q", d.serviceType, balance.ServiceWhatsAppCampaign)
	}
	if d.amount != catalog.PriceMicros {
		t.Errorf("charge = %d, want %d (authentication catalog price)", d.amount, catalog.PriceMicros)
	}
	if d.costMicros != catalog.CostMicros {
		t.Errorf("cost = %d, want %d", d.costMicros, catalog.CostMicros)
	}
	expectedProfit := catalog.PriceMicros - catalog.CostMicros
	if d.profitMicros != expectedProfit {
		t.Errorf("profit = %d, want %d", d.profitMicros, expectedProfit)
	}
	if d.profitMicros <= 0 {
		t.Error("profit must be positive for authentication template")
	}
}

func TestWhatsAppBilling_AuthenticationRefundNegatesProfit(t *testing.T) {
	balanceRepo := newWAMockBalanceRepo()
	pricer := newWATestPricer()
	uc := NewConsumeWhatsappTemplateUseCase(balanceRepo, pricer, &allowAllSubscriptionChecker{})

	_, _ = uc.Execute("ws-wa-auth-refund", "entry-auth-ref-1", "AUTHENTICATION")
	err := uc.Refund("ws-wa-auth-refund", "entry-auth-ref-1", "AUTHENTICATION")
	if err != nil {
		t.Fatalf("Refund() error: %v", err)
	}

	balanceRepo.mu.Lock()
	defer balanceRepo.mu.Unlock()

	if len(balanceRepo.credits) != 1 {
		t.Fatalf("expected 1 refund credit, got %d", len(balanceRepo.credits))
	}
	c := balanceRepo.credits[0]
	d := balanceRepo.debits[0]

	if !c.isRefund {
		t.Error("isRefund must be true")
	}
	if c.profitMicros != -d.profitMicros {
		t.Errorf("authentication refund profit = %d, want %d", c.profitMicros, -d.profitMicros)
	}
	if c.referenceID == nil || *c.referenceID != "refund:entry-auth-ref-1" {
		ref := ""
		if c.referenceID != nil {
			ref = *c.referenceID
		}
		t.Errorf("referenceID = %q, want \"refund:entry-auth-ref-1\"", ref)
	}
}

func TestWhatsAppBilling_UnsupportedCategoryRejectsDebit(t *testing.T) {
	balanceRepo := newWAMockBalanceRepo()
	pricer := newWATestPricer()
	uc := NewConsumeWhatsappTemplateUseCase(balanceRepo, pricer, &allowAllSubscriptionChecker{})

	_, err := uc.Execute("ws-wa", "entry-bad-cat", "SERVICE")
	if err == nil {
		t.Fatal("expected error for unsupported category SERVICE")
	}

	balanceRepo.mu.Lock()
	defer balanceRepo.mu.Unlock()
	if len(balanceRepo.debits) != 0 {
		t.Errorf("no debit should occur for unsupported category, got %d", len(balanceRepo.debits))
	}
}

func TestWhatsAppBilling_UnsupportedCategoryRejectsRefund(t *testing.T) {
	balanceRepo := newWAMockBalanceRepo()
	pricer := newWATestPricer()
	uc := NewConsumeWhatsappTemplateUseCase(balanceRepo, pricer, &allowAllSubscriptionChecker{})

	err := uc.Refund("ws-wa", "entry-bad-cat", "SERVICE")
	if err == nil {
		t.Fatal("expected error for unsupported category SERVICE on refund")
	}

	balanceRepo.mu.Lock()
	defer balanceRepo.mu.Unlock()
	if len(balanceRepo.credits) != 0 {
		t.Errorf("no credit should occur for unsupported category, got %d", len(balanceRepo.credits))
	}
}

func TestWhatsAppBilling_EmptyCategoryRejectsDebit(t *testing.T) {
	balanceRepo := newWAMockBalanceRepo()
	pricer := newWATestPricer()
	uc := NewConsumeWhatsappTemplateUseCase(balanceRepo, pricer, &allowAllSubscriptionChecker{})

	_, err := uc.Execute("ws-wa", "entry-empty", "")
	if err == nil {
		t.Fatal("expected error for empty category")
	}
}

func TestWhatsAppBilling_LowercaseCategoryWorks(t *testing.T) {
	balanceRepo := newWAMockBalanceRepo()
	pricer := newWATestPricer()
	uc := NewConsumeWhatsappTemplateUseCase(balanceRepo, pricer, &allowAllSubscriptionChecker{})

	_, err := uc.Execute("ws-wa", "entry-lower", "utility")
	if err != nil {
		t.Fatalf("Execute() with lowercase category should succeed: %v", err)
	}

	balanceRepo.mu.Lock()
	defer balanceRepo.mu.Unlock()
	if len(balanceRepo.debits) != 1 {
		t.Fatalf("expected 1 debit for lowercase category, got %d", len(balanceRepo.debits))
	}
	catalog := findWACatalogItem("utility")
	if balanceRepo.debits[0].amount != catalog.PriceMicros {
		t.Errorf("charge = %d, want %d", balanceRepo.debits[0].amount, catalog.PriceMicros)
	}
}

func TestWhatsAppBilling_MixedCaseCategoryWorks(t *testing.T) {
	balanceRepo := newWAMockBalanceRepo()
	pricer := newWATestPricer()
	uc := NewConsumeWhatsappTemplateUseCase(balanceRepo, pricer, &allowAllSubscriptionChecker{})

	_, err := uc.Execute("ws-wa", "entry-mixed", "Marketing")
	if err != nil {
		t.Fatalf("Execute() with mixed-case category should succeed: %v", err)
	}

	balanceRepo.mu.Lock()
	defer balanceRepo.mu.Unlock()
	if len(balanceRepo.debits) != 1 {
		t.Fatalf("expected 1 debit for mixed-case category, got %d", len(balanceRepo.debits))
	}
}

func TestGetTemplateCostMicros_Authentication(t *testing.T) {
	pricer := newWATestPricer()
	uc := NewConsumeWhatsappTemplateUseCase(newWAMockBalanceRepo(), pricer, &allowAllSubscriptionChecker{})

	cost, err := uc.GetTemplateCostMicros("ws1", "AUTHENTICATION")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	catalog := findWACatalogItem("authentication")
	if cost != catalog.PriceMicros {
		t.Errorf("cost = %d, want %d", cost, catalog.PriceMicros)
	}
}

func TestGetTemplateCostMicros_Marketing(t *testing.T) {
	pricer := newWATestPricer()
	uc := NewConsumeWhatsappTemplateUseCase(newWAMockBalanceRepo(), pricer, &allowAllSubscriptionChecker{})

	cost, err := uc.GetTemplateCostMicros("ws1", "MARKETING")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	catalog := findWACatalogItem("marketing")
	if cost != catalog.PriceMicros {
		t.Errorf("cost = %d, want %d", cost, catalog.PriceMicros)
	}
}

func TestGetTemplateCostMicros_UnsupportedCategoryFails(t *testing.T) {
	pricer := newWATestPricer()
	uc := NewConsumeWhatsappTemplateUseCase(newWAMockBalanceRepo(), pricer, &allowAllSubscriptionChecker{})

	_, err := uc.GetTemplateCostMicros("ws1", "UNKNOWN_CATEGORY")
	if err == nil {
		t.Fatal("expected error for unsupported category")
	}
}

func TestWhatsAppBilling_DebitRepoError(t *testing.T) {
	uc := NewConsumeWhatsappTemplateUseCase(&waErrBalanceRepo{debitErr: fmt.Errorf("db write failed")}, newWATestPricer(), &allowAllSubscriptionChecker{})

	_, err := uc.Execute("ws1", "ref1", "UTILITY")
	if err == nil {
		t.Fatal("expected error when debit repo fails")
	}
}

func TestWhatsAppBilling_CreditRepoError(t *testing.T) {
	uc := NewConsumeWhatsappTemplateUseCase(&waErrBalanceRepo{creditErr: fmt.Errorf("db write failed")}, newWATestPricer(), &allowAllSubscriptionChecker{})

	err := uc.Refund("ws1", "ref1", "UTILITY")
	if err == nil {
		t.Fatal("expected error when credit repo fails")
	}
}

type waErrBalanceRepo struct {
	debitErr  error
	creditErr error
}

func (m *waErrBalanceRepo) Create(*balance.Balance) error                     { return nil }
func (m *waErrBalanceRepo) GetByWorkspaceID(string) (*balance.Balance, error) { return nil, nil }
func (m *waErrBalanceRepo) EnsureBalanceExists(string, string) (*balance.Balance, error) {
	return nil, nil
}
func (m *waErrBalanceRepo) HasSufficientBalance(string, int64) (bool, error) { return true, nil }
func (m *waErrBalanceRepo) GetFullBalanceSummary(string) (*balance.FullBalanceSummary, error) {
	return nil, nil
}
func (m *waErrBalanceRepo) GetTransaction(string) (*balance.Transaction, error) { return nil, nil }
func (m *waErrBalanceRepo) ListTransactions(balance.ListTransactionsInput) (*shared.PaginatedResult[*balance.Transaction], error) {
	return nil, nil
}
func (m *waErrBalanceRepo) ExistsTransactionByReferenceID(string) (bool, error) { return false, nil }
func (m *waErrBalanceRepo) AggregateDailyCosts(time.Time) ([]balance.DailyCostRow, error) {
	return nil, nil
}
func (m *waErrBalanceRepo) DebitBalance(balance.DebitBalanceInput) (*balance.Transaction, error) {
	return nil, m.debitErr
}
func (m *waErrBalanceRepo) CreditBalance(balance.CreditBalanceInput) (*balance.Transaction, error) {
	return nil, m.creditErr
}
