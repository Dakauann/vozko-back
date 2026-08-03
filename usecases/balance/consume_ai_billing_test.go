package balance_usecase

import (
	"encoding/json"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"vozko/domain/ai"
	"vozko/domain/balance"
	"vozko/domain/messaging"
	"vozko/domain/shared"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

type mockBalanceRepo struct {
	mu             sync.Mutex
	balance        int64
	debits         []aiDebitCall
	existingRefIDs map[string]bool
	debitErr       error
	existsErr      error
}

type aiDebitCall struct {
	workspaceID   string
	amount        int64
	serviceType   balance.ServiceType
	referenceID   *string
	allowNegative bool
}

func newMockBalanceRepo(initialBalance int64) *mockBalanceRepo {
	return &mockBalanceRepo{balance: initialBalance, existingRefIDs: make(map[string]bool)}
}

func (m *mockBalanceRepo) Create(b *balance.Balance) error                        { return nil }
func (m *mockBalanceRepo) GetByWorkspaceID(wsID string) (*balance.Balance, error) { return nil, nil }
func (m *mockBalanceRepo) EnsureBalanceExists(wsID, currency string) (*balance.Balance, error) {
	return nil, nil
}
func (m *mockBalanceRepo) CreditBalance(params balance.CreditBalanceInput) (*balance.Transaction, error) {
	return nil, nil
}
func (m *mockBalanceRepo) DebitBalance(params balance.DebitBalanceInput) (*balance.Transaction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.debitErr != nil {
		return nil, m.debitErr
	}
	m.debits = append(m.debits, aiDebitCall{workspaceID: params.WorkspaceID, amount: params.Amount, serviceType: params.ServiceType, referenceID: params.ReferenceID, allowNegative: params.AllowNegative})
	m.balance -= params.Amount
	if params.ReferenceID != nil {
		m.existingRefIDs[*params.ReferenceID] = true
	}
	return &balance.Transaction{ID: "tx-" + params.WorkspaceID, WorkspaceID: params.WorkspaceID, Amount: params.Amount, CostMicros: params.CostMicros, ProfitMicros: params.ProfitMicros}, nil
}
func (m *mockBalanceRepo) HasSufficientBalance(wsID string, amount int64) (bool, error) {
	return m.balance >= amount, nil
}
func (m *mockBalanceRepo) GetFullBalanceSummary(wsID string) (*balance.FullBalanceSummary, error) {
	return nil, nil
}
func (m *mockBalanceRepo) GetTransaction(txID string) (*balance.Transaction, error) { return nil, nil }
func (m *mockBalanceRepo) ListTransactions(input balance.ListTransactionsInput) (*shared.PaginatedResult[*balance.Transaction], error) {
	return nil, nil
}
func (m *mockBalanceRepo) ExistsTransactionByReferenceID(refID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.existsErr != nil {
		return false, m.existsErr
	}
	return m.existingRefIDs[refID], nil
}
func (m *mockBalanceRepo) AggregateDailyCosts(date time.Time) ([]balance.DailyCostRow, error) {
	return nil, nil
}

type mockPricingRepo struct{}

func (m *mockPricingRepo) ListDefaultPricingItems() ([]workspace_pricing.PricingItem, error) {
	return workspace_pricing.DefaultPricingCatalog, nil
}
func (m *mockPricingRepo) GetPricingItem(id string) (*workspace_pricing.PricingItem, error) {
	return nil, nil
}
func (m *mockPricingRepo) UpsertPricingItem(item *workspace_pricing.PricingItem) error { return nil }
func (m *mockPricingRepo) DeletePricingItem(id string) error                           { return nil }
func (m *mockPricingRepo) SeedDefaults(items []workspace_pricing.PricingItem) error    { return nil }
func (m *mockPricingRepo) CreateAuditEntry(entry *workspace_pricing.PricingAuditEntry) error {
	return nil
}
func (m *mockPricingRepo) ListAuditEntries(wsID *string, l, o int) ([]workspace_pricing.PricingAuditEntry, error) {
	return nil, nil
}

type mockLLMFetcher struct{}

func (f *mockLLMFetcher) FetchLLMPriceMicros(model string) (int64, int64, error) {
	prices := map[string][2]int64{
		"gpt-4o-mini":      {150_000, 600_000},
		"claude-sonnet-4":  {3_000_000, 15_000_000},
		"gpt-4.1":          {2_000_000, 8_000_000},
		"gemini-2.5-flash": {300_000, 2_500_000},
	}
	if p, ok := prices[model]; ok {
		return p[0], p[1], nil
	}
	return 0, 0, nil
}

func newAITestPricer() workspace_pricing.Pricer {
	return workspace_pricing.NewPricer(&mockPricingRepo{}, workspace_pricing.WithLLMPriceFetcher(&mockLLMFetcher{}))
}

type mockSub struct {
	handlers map[string]func([]byte, messaging.MessageAck)
}

func newMockSub() *mockSub {
	return &mockSub{handlers: make(map[string]func([]byte, messaging.MessageAck))}
}

func (m *mockSub) Subscribe(topic string, handler func([]byte, messaging.MessageAck)) error {
	m.handlers[topic] = handler
	return nil
}

func (m *mockSub) DeleteQueue(topic string) error {
	delete(m.handlers, topic)
	return nil
}

func (m *mockSub) ValidateConnection() error {
	return nil
}

func (m *mockSub) GetQueueLength(topic string) (int, error) {
	return 0, nil
}

type mockAck struct {
	mu            sync.Mutex
	acked         bool
	nacked        bool
	requeued      bool
	deliveryCount int
}

func (m *mockAck) Ack() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.acked = true
	return nil
}
func (m *mockAck) Nack(requeue bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nacked = true
	m.requeued = requeue
	return nil
}
func (m *mockAck) DeliveryCount() int { return m.deliveryCount }

type stubBillingMetrics struct {
	mu      sync.Mutex
	reasons []string
}

func (s *stubBillingMetrics) IncBillingSkipped(reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reasons = append(s.reasons, reason)
}

func (s *stubBillingMetrics) count(reason string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, r := range s.reasons {
		if r == reason {
			n++
		}
	}
	return n
}

func makeEvent(requestID, workspaceID, model string, promptTokens, completionTokens int) ai.AICompletedEvent {
	return ai.AICompletedEvent{
		RequestID:        requestID,
		WorkspaceID:      workspaceID,
		Model:            model,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
	}
}

func fireAndWait(t *testing.T, sub *mockSub, event ai.AICompletedEvent, ack *mockAck) {
	t.Helper()
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	handler := sub.handlers[ai.TopicAIBillingCompleted]
	if handler == nil {
		t.Fatal("no handler registered for AI billing topic")
	}
	handler(data, ack)
	time.Sleep(200 * time.Millisecond)
}

func TestAIBilling_HappyPath(t *testing.T) {
	balanceRepo := newMockBalanceRepo(1_000_000)
	pricer := newAITestPricer()
	sub := newMockSub()

	consumer := NewConsumeAIBillingUseCase(sub, balanceRepo, pricer, nil)
	if err := consumer.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	event := makeEvent("req-1", "ws-1", "gpt-4o-mini", 1000, 500)
	ack := &mockAck{deliveryCount: 1}
	fireAndWait(t, sub, event, ack)

	if !ack.acked {
		t.Error("expected ACK on success")
	}

	balanceRepo.mu.Lock()
	defer balanceRepo.mu.Unlock()

	if len(balanceRepo.debits) != 1 {
		t.Fatalf("expected 1 debit, got %d", len(balanceRepo.debits))
	}

	d := balanceRepo.debits[0]
	if d.workspaceID != "ws-1" {
		t.Errorf("workspace = %q, want ws-1", d.workspaceID)
	}
	if d.serviceType != balance.ServiceAI {
		t.Errorf("service = %q, want %q", d.serviceType, balance.ServiceAI)
	}
	if d.referenceID == nil || *d.referenceID != "req-1" {
		t.Error("referenceID should be req-1")
	}
	if d.amount <= 0 {
		t.Error("debit amount should be positive")
	}
}

func TestAIBilling_Idempotency(t *testing.T) {
	balanceRepo := newMockBalanceRepo(1_000_000)

	balanceRepo.existingRefIDs["req-dup"] = true
	pricer := newAITestPricer()
	sub := newMockSub()

	consumer := NewConsumeAIBillingUseCase(sub, balanceRepo, pricer, nil)
	_ = consumer.Start()

	event := makeEvent("req-dup", "ws-1", "gpt-4o-mini", 1000, 500)
	ack := &mockAck{deliveryCount: 1}
	fireAndWait(t, sub, event, ack)

	if !ack.acked {
		t.Error("expected ACK for duplicate (idempotent skip)")
	}

	balanceRepo.mu.Lock()
	defer balanceRepo.mu.Unlock()

	if len(balanceRepo.debits) != 0 {
		t.Errorf("expected 0 debits for duplicate, got %d", len(balanceRepo.debits))
	}
}

func TestAIBilling_BadMessage(t *testing.T) {
	sub := newMockSub()

	consumer := NewConsumeAIBillingUseCase(sub, newMockBalanceRepo(0), newAITestPricer(), nil)
	_ = consumer.Start()

	ack := &mockAck{deliveryCount: 1}
	handler := sub.handlers[ai.TopicAIBillingCompleted]
	handler([]byte("invalid json"), ack)

	time.Sleep(100 * time.Millisecond)

	if !ack.nacked {
		t.Error("expected NACK for bad message")
	}
	if ack.requeued {
		t.Error("bad message should not be requeued")
	}
}

func TestAIBilling_ZeroTokens(t *testing.T) {
	balanceRepo := newMockBalanceRepo(1_000_000)
	sub := newMockSub()

	consumer := NewConsumeAIBillingUseCase(sub, balanceRepo, newAITestPricer(), nil)
	_ = consumer.Start()

	event := makeEvent("req-zero", "ws-1", "gpt-4o-mini", 0, 0)
	ack := &mockAck{deliveryCount: 1}
	fireAndWait(t, sub, event, ack)

	if !ack.acked {
		t.Error("expected ACK for zero-token event")
	}

	balanceRepo.mu.Lock()
	defer balanceRepo.mu.Unlock()

	if len(balanceRepo.debits) != 0 {
		t.Errorf("expected 0 debits for zero tokens, got %d", len(balanceRepo.debits))
	}
}

func TestAIBilling_EmptyWorkspaceID(t *testing.T) {
	sub := newMockSub()

	consumer := NewConsumeAIBillingUseCase(sub, newMockBalanceRepo(0), newAITestPricer(), nil)
	_ = consumer.Start()

	event := makeEvent("req-empty-ws", "", "gpt-4o-mini", 100, 100)
	ack := &mockAck{deliveryCount: 1}
	fireAndWait(t, sub, event, ack)

	if !ack.acked {
		t.Error("expected ACK for empty workspace (skip)")
	}
}

func TestAIBilling_EmptyRequestID(t *testing.T) {
	sub := newMockSub()

	consumer := NewConsumeAIBillingUseCase(sub, newMockBalanceRepo(0), newAITestPricer(), nil)
	_ = consumer.Start()

	event := makeEvent("", "ws-1", "gpt-4o-mini", 100, 100)
	ack := &mockAck{deliveryCount: 1}
	fireAndWait(t, sub, event, ack)

	if !ack.acked {
		t.Error("expected ACK for empty requestID (skip)")
	}
}

func TestAIBilling_RetryExhaustion(t *testing.T) {
	balanceRepo := newMockBalanceRepo(1_000_000)
	balanceRepo.debitErr = errors.New("database down")
	sub := newMockSub()
	metrics := &stubBillingMetrics{}

	consumer := NewConsumeAIBillingUseCase(sub, balanceRepo, newAITestPricer(), metrics)
	_ = consumer.Start()

	event := makeEvent("req-retry", "ws-1", "gpt-4o-mini", 1000, 500)

	ack1 := &mockAck{deliveryCount: 1}
	fireAndWait(t, sub, event, ack1)

	ack1.mu.Lock()
	if !ack1.nacked {
		t.Error("expected NACK on debit failure")
	}
	if !ack1.requeued {
		t.Error("first failure should requeue")
	}
	ack1.mu.Unlock()

	ack2 := &mockAck{deliveryCount: messaging.MaxRetries}
	fireAndWait(t, sub, event, ack2)

	ack2.mu.Lock()
	if !ack2.nacked {
		t.Error("expected NACK at max retries")
	}
	if ack2.requeued {
		t.Error("should not requeue at max retries (DLQ)")
	}
	ack2.mu.Unlock()

	// A permanently dropped event is lost revenue, it must increment the metric.
	if got := metrics.count("permanent_drop"); got != 1 {
		t.Errorf("expected 1 permanent_drop skip metric, got %d", got)
	}
}

func TestAIBilling_CostCalculation(t *testing.T) {
	balanceRepo := newMockBalanceRepo(1_000_000_000)
	pricer := newAITestPricer()
	sub := newMockSub()

	consumer := NewConsumeAIBillingUseCase(sub, balanceRepo, pricer, nil)
	_ = consumer.Start()

	event := makeEvent("req-cost", "ws-1", "gpt-4o-mini", 1_000_000, 1_000_000)
	ack := &mockAck{deliveryCount: 1}
	fireAndWait(t, sub, event, ack)

	balanceRepo.mu.Lock()
	defer balanceRepo.mu.Unlock()

	if len(balanceRepo.debits) != 1 {
		t.Fatalf("expected 1 debit, got %d", len(balanceRepo.debits))
	}

	expectedCostMicros := int64(900_000)
	if balanceRepo.debits[0].amount != expectedCostMicros {
		t.Errorf("debit = %d micros, want %d", balanceRepo.debits[0].amount, expectedCostMicros)
	}
}

// End-to-end money proof for a reasoning-heavy turn: OpenRouter folds reasoning
// tokens into completion_tokens, so the published event carries the FULL completion
// count (1000, of which 700 were thinking). The debit must charge for all 1000,
// i.e. the user pays for the thinking. (If reasoning were dropped, completion would
// be 300 and the charge would be ~432 µ instead of 936 µ.)
func TestAIBilling_ChargesFullCompletionIncludingReasoning(t *testing.T) {
	balanceRepo := newMockBalanceRepo(1_000_000)
	pricer := newAITestPricer()
	sub := newMockSub()

	consumer := NewConsumeAIBillingUseCase(sub, balanceRepo, pricer, nil)
	_ = consumer.Start()

	// prompt=1200, completion=1000 (incl. 700 reasoning), exactly what the adapter
	// publishes for a thinking model (see openrouter billing_stream_test.go).
	event := makeEvent("req-reasoning", "ws-1", "gpt-4o-mini", 1200, 1000)
	ack := &mockAck{deliveryCount: 1}
	fireAndWait(t, sub, event, ack)

	balanceRepo.mu.Lock()
	defer balanceRepo.mu.Unlock()

	if len(balanceRepo.debits) != 1 {
		t.Fatalf("expected 1 debit, got %d", len(balanceRepo.debits))
	}

	// gpt-4o-mini: 150_000 µ/M input, 600_000 µ/M output (mockLLMFetcher), +20% markup.
	inputCost := 1200.0 / 1_000_000 * 150_000  // 180
	outputCost := 1000.0 / 1_000_000 * 600_000 // 600, the full completion, reasoning included
	expectedMicros := int64(math.Ceil((inputCost + outputCost) * 1.20))

	if balanceRepo.debits[0].amount != expectedMicros {
		t.Fatalf("debit = %d µ, want %d µ (full completion incl. reasoning)", balanceRepo.debits[0].amount, expectedMicros)
	}

	// Sanity: dropping the 700 reasoning tokens would charge meaningfully less.
	reasoningDroppedMicros := int64(math.Ceil((inputCost + 300.0/1_000_000*600_000) * 1.20))
	if balanceRepo.debits[0].amount <= reasoningDroppedMicros {
		t.Fatalf("charge %d µ did not include reasoning (drop-reasoning would be %d µ)", balanceRepo.debits[0].amount, reasoningDroppedMicros)
	}
}

func TestAIBilling_DebitError_NACKsWithRequeue(t *testing.T) {
	balanceRepo := newMockBalanceRepo(1_000_000)
	balanceRepo.debitErr = errors.New("connection refused")
	sub := newMockSub()

	consumer := NewConsumeAIBillingUseCase(sub, balanceRepo, newAITestPricer(), nil)
	_ = consumer.Start()

	event := makeEvent("req-fail", "ws-1", "gpt-4o-mini", 1000, 500)
	ack := &mockAck{deliveryCount: 1}
	fireAndWait(t, sub, event, ack)

	ack.mu.Lock()
	defer ack.mu.Unlock()

	if !ack.nacked {
		t.Error("expected NACK on debit error")
	}
	if !ack.requeued {
		t.Error("first failure should requeue for retry")
	}
}

func TestAIBilling_IdempotencyCheckError_NACKs(t *testing.T) {
	balanceRepo := newMockBalanceRepo(1_000_000)
	balanceRepo.existsErr = errors.New("database timeout")
	sub := newMockSub()

	consumer := NewConsumeAIBillingUseCase(sub, balanceRepo, newAITestPricer(), nil)
	_ = consumer.Start()

	event := makeEvent("req-exists-fail", "ws-1", "gpt-4o-mini", 1000, 500)
	ack := &mockAck{deliveryCount: 1}
	fireAndWait(t, sub, event, ack)

	ack.mu.Lock()
	defer ack.mu.Unlock()

	if !ack.nacked {
		t.Error("expected NACK when idempotency check fails")
	}
	if !ack.requeued {
		t.Error("idempotency check failure should requeue")
	}
}

func TestAIBilling_SubCentPrecision(t *testing.T) {

	balanceRepo := newMockBalanceRepo(1_000_000_000)
	pricer := newAITestPricer()
	sub := newMockSub()

	consumer := NewConsumeAIBillingUseCase(sub, balanceRepo, pricer, nil)
	_ = consumer.Start()

	event := makeEvent("req-subcent", "ws-1", "gpt-4o-mini", 3015, 27)
	ack := &mockAck{deliveryCount: 1}
	fireAndWait(t, sub, event, ack)

	if !ack.acked {
		t.Fatal("expected ACK")
	}

	balanceRepo.mu.Lock()
	defer balanceRepo.mu.Unlock()

	if len(balanceRepo.debits) != 1 {
		t.Fatalf("expected 1 debit, got %d", len(balanceRepo.debits))
	}

	baseCost := 3015.0/1_000_000*150_000 + 27.0/1_000_000*600_000
	expectedMicros := int64(math.Ceil(baseCost * 1.20))

	if balanceRepo.debits[0].amount != expectedMicros {
		t.Errorf("debit = %d micros, want %d ($%.6f)", balanceRepo.debits[0].amount, expectedMicros, float64(expectedMicros)/1_000_000)
	}

	if balanceRepo.debits[0].amount >= 10_000 {
		t.Errorf("debit %d micros should be well under 10,000 (old 1-cent minimum)", balanceRepo.debits[0].amount)
	}
}

func TestAIBilling_MarginApplied_AllModels(t *testing.T) {
	balanceRepo := newMockBalanceRepo(1_000_000_000)
	pricer := newAITestPricer()
	sub := newMockSub()

	consumer := NewConsumeAIBillingUseCase(sub, balanceRepo, pricer, nil)
	_ = consumer.Start()

	tests := []struct {
		model            string
		inputPerMillion  float64
		outputPerMillion float64
		promptTokens     int
		completionTokens int
	}{
		{"gpt-4o-mini", 150_000, 600_000, 10_000, 5_000},
		{"claude-sonnet-4", 3_000_000, 15_000_000, 1_000, 500},
		{"gpt-4.1", 2_000_000, 8_000_000, 5_000, 2_000},
		{"gemini-2.5-flash", 300_000, 2_500_000, 50_000, 10_000},
	}

	for i, tt := range tests {
		event := makeEvent("req-margin-"+tt.model, "ws-1", tt.model, tt.promptTokens, tt.completionTokens)
		ack := &mockAck{deliveryCount: 1}
		fireAndWait(t, sub, event, ack)

		if !ack.acked {
			t.Errorf("[%s] expected ACK", tt.model)
			continue
		}

		balanceRepo.mu.Lock()
		if len(balanceRepo.debits) != i+1 {
			t.Errorf("[%s] expected %d debits, got %d", tt.model, i+1, len(balanceRepo.debits))
			balanceRepo.mu.Unlock()
			continue
		}

		baseCost := float64(tt.promptTokens)/1_000_000*tt.inputPerMillion + float64(tt.completionTokens)/1_000_000*tt.outputPerMillion
		expectedMicros := int64(math.Ceil(baseCost * 1.20))
		actual := balanceRepo.debits[i].amount

		if actual != expectedMicros {
			t.Errorf("[%s] debit = %d micros, want %d (base=%.2f, +20%%=%.2f)",
				tt.model, actual, expectedMicros, baseCost, baseCost*1.20)
		}
		balanceRepo.mu.Unlock()
	}
}

func TestAIBilling_ExactDollarAmount(t *testing.T) {

	balanceRepo := newMockBalanceRepo(1_000_000_000)
	pricer := newAITestPricer()
	sub := newMockSub()

	consumer := NewConsumeAIBillingUseCase(sub, balanceRepo, pricer, nil)
	_ = consumer.Start()

	event := makeEvent("req-dollar", "ws-1", "claude-sonnet-4", 100_000, 50_000)
	ack := &mockAck{deliveryCount: 1}
	fireAndWait(t, sub, event, ack)

	balanceRepo.mu.Lock()
	defer balanceRepo.mu.Unlock()

	baseCost := 100_000.0/1_000_000*3_000_000 + 50_000.0/1_000_000*15_000_000
	expectedMicros := int64(math.Ceil(baseCost * 1.20))

	if balanceRepo.debits[0].amount != expectedMicros {
		t.Errorf("debit = %d micros ($%.4f), want %d ($%.4f)",
			balanceRepo.debits[0].amount, float64(balanceRepo.debits[0].amount)/1_000_000,
			expectedMicros, float64(expectedMicros)/1_000_000)
	}
}

func TestAIBilling_ProfitPersisted(t *testing.T) {
	balanceRepo := newMockBalanceRepo(1_000_000_000)
	pricer := newAITestPricer()
	sub := newMockSub()

	consumer := NewConsumeAIBillingUseCase(sub, balanceRepo, pricer, nil)
	_ = consumer.Start()

	event := makeEvent("req-profit", "ws-1", "gpt-4o-mini", 1_000_000, 1_000_000)
	ack := &mockAck{deliveryCount: 1}
	fireAndWait(t, sub, event, ack)

	if !ack.acked {
		t.Fatal("expected ACK")
	}

	balanceRepo.mu.Lock()
	defer balanceRepo.mu.Unlock()

	if len(balanceRepo.debits) != 1 {
		t.Fatalf("expected 1 debit, got %d", len(balanceRepo.debits))
	}
	d := balanceRepo.debits[0]

	expectedCostMicros := int64(150_000 + 600_000)
	if d.amount-d.amount/6 > expectedCostMicros+1000 {
		t.Log("provider cost sanity check passed")
	}

	profitMicros := d.amount - expectedCostMicros

	if profitMicros <= 0 {
		t.Errorf("profit should be positive, charge=%d cost=%d", d.amount, expectedCostMicros)
	}
	if profitMicros >= d.amount {
		t.Errorf("profit should be less than full charge, profit=%d charge=%d", profitMicros, d.amount)
	}
}

func TestAIBilling_AllowNegative_DebitsEvenWithZeroBalance(t *testing.T) {

	balanceRepo := newMockBalanceRepo(0)
	pricer := newAITestPricer()
	sub := newMockSub()

	consumer := NewConsumeAIBillingUseCase(sub, balanceRepo, pricer, nil)
	_ = consumer.Start()

	event := makeEvent("req-negative", "ws-broke", "gpt-4o-mini", 1000, 500)
	ack := &mockAck{deliveryCount: 1}
	fireAndWait(t, sub, event, ack)

	if !ack.acked {
		t.Fatal("expected ACK, AI already consumed, must debit regardless")
	}

	balanceRepo.mu.Lock()
	defer balanceRepo.mu.Unlock()

	if len(balanceRepo.debits) != 1 {
		t.Fatalf("expected 1 debit, got %d", len(balanceRepo.debits))
	}

	d := balanceRepo.debits[0]
	if !d.allowNegative {
		t.Error("AllowNegative must be true, AI tokens were already consumed, billing cannot be rejected")
	}
	if d.amount <= 0 {
		t.Error("debit amount must be positive")
	}
	if balanceRepo.balance >= 0 {
		t.Errorf("balance should have gone negative, got %d", balanceRepo.balance)
	}
}

func TestAIBilling_UnknownModel_ZeroPrice(t *testing.T) {

	balanceRepo := newMockBalanceRepo(1_000_000)
	pricer := newAITestPricer()
	sub := newMockSub()
	metrics := &stubBillingMetrics{}

	consumer := NewConsumeAIBillingUseCase(sub, balanceRepo, pricer, metrics)
	_ = consumer.Start()

	event := makeEvent("req-unknown", "ws-1", "totally-unknown-model", 1000, 500)
	ack := &mockAck{deliveryCount: 1}
	fireAndWait(t, sub, event, ack)

	if !ack.acked {
		t.Error("expected ACK for zero-price (unknown model)")
	}

	balanceRepo.mu.Lock()
	defer balanceRepo.mu.Unlock()

	if len(balanceRepo.debits) != 0 {
		t.Errorf("expected 0 debits for zero-price model, got %d", len(balanceRepo.debits))
	}
	// The skip must be observable, an unpriced model billing $0 is a revenue leak
	// that has to surface on a metric, not vanish silently.
	if got := metrics.count("zero_price"); got != 1 {
		t.Errorf("expected 1 zero_price skip metric, got %d", got)
	}
}

type errLLMFetcher struct{ err error }

func (f *errLLMFetcher) FetchLLMPriceMicros(string) (int64, int64, error) {
	return 0, 0, f.err
}

type errDefaultsPricingRepo struct{ err error }

func (m *errDefaultsPricingRepo) ListDefaultPricingItems() ([]workspace_pricing.PricingItem, error) {
	return nil, m.err
}
func (m *errDefaultsPricingRepo) GetPricingItem(string) (*workspace_pricing.PricingItem, error) {
	return nil, nil
}
func (m *errDefaultsPricingRepo) UpsertPricingItem(*workspace_pricing.PricingItem) error { return nil }
func (m *errDefaultsPricingRepo) DeletePricingItem(string) error                         { return nil }
func (m *errDefaultsPricingRepo) SeedDefaults([]workspace_pricing.PricingItem) error     { return nil }
func (m *errDefaultsPricingRepo) CreateAuditEntry(*workspace_pricing.PricingAuditEntry) error {
	return nil
}
func (m *errDefaultsPricingRepo) ListAuditEntries(*string, int, int) ([]workspace_pricing.PricingAuditEntry, error) {
	return nil, nil
}

func TestAIBilling_PricerError_NACKs(t *testing.T) {
	balanceRepo := newMockBalanceRepo(1_000_000)

	errRepo := &errDefaultsPricingRepo{err: errors.New("db down")}
	pricer := workspace_pricing.NewPricer(errRepo, workspace_pricing.WithLLMPriceFetcher(&mockLLMFetcher{}))
	sub := newMockSub()

	consumer := NewConsumeAIBillingUseCase(sub, balanceRepo, pricer, nil)
	_ = consumer.Start()

	event := makeEvent("req-price-err", "ws-1", "gpt-4o-mini", 1000, 500)
	ack := &mockAck{deliveryCount: 1}
	fireAndWait(t, sub, event, ack)

	ack.mu.Lock()
	defer ack.mu.Unlock()

	if !ack.nacked {
		t.Error("expected NACK when pricer fails")
	}
}

type panicBalanceRepo struct{ mockBalanceRepo }

func (m *panicBalanceRepo) ExistsTransactionByReferenceID(string) (bool, error) { return false, nil }
func (m *panicBalanceRepo) AggregateDailyCosts(time.Time) ([]balance.DailyCostRow, error) {
	return nil, nil
}
func (m *panicBalanceRepo) DebitBalance(balance.DebitBalanceInput) (*balance.Transaction, error) {
	panic("simulated panic in debit")
}

func TestAIBilling_Panic_Recovery(t *testing.T) {
	repo := &panicBalanceRepo{}
	pricer := newAITestPricer()
	sub := newMockSub()

	consumer := NewConsumeAIBillingUseCase(sub, repo, pricer, nil)
	_ = consumer.Start()

	event := makeEvent("req-panic", "ws-1", "gpt-4o-mini", 1000, 500)
	ack := &mockAck{deliveryCount: 1}
	fireAndWait(t, sub, event, ack)

	ack.mu.Lock()
	defer ack.mu.Unlock()

	if !ack.nacked {
		t.Error("expected NACK after panic recovery")
	}
	if ack.requeued {
		t.Error("panic should not requeue (dropped)")
	}
}
