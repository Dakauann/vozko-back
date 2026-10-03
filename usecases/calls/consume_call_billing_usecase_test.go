package calls_usecase

import (
	"errors"
	"sync"
	"testing"
	"time"

	"vozko/domain/balance"
	"vozko/domain/calls/billing"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

type memoryBillingRecords struct {
	billing.Repository
	mu      sync.Mutex
	records map[string]*billing.CallBillingRecord
}

func (m *memoryBillingRecords) GetByCallID(callID string) (*billing.CallBillingRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.records[callID]; ok {
		copied := *r
		return &copied, nil
	}
	return nil, billing.ErrBillingRecordNotFound
}

func (m *memoryBillingRecords) Update(record *billing.CallBillingRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := *record
	m.records[record.CallID] = &copied
	return nil
}

type recordingLedger struct {
	balance.Repository
	mu     sync.Mutex
	debits []balance.DebitBalanceInput
}

func (l *recordingLedger) ExistsTransactionByReferenceID(referenceID string) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, d := range l.debits {
		if d.ReferenceID != nil && *d.ReferenceID == referenceID {
			return true, nil
		}
	}
	return false, nil
}

func (l *recordingLedger) DebitBalance(input balance.DebitBalanceInput) (*balance.Transaction, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.debits = append(l.debits, input)
	return &balance.Transaction{ID: "tx-" + *input.ReferenceID}, nil
}

type catalogDefaults struct {
	workspace_pricing.Repository
}

func (catalogDefaults) ListDefaultPricingItems() ([]workspace_pricing.PricingItem, error) {
	return workspace_pricing.DefaultPricingCatalog, nil
}

type planWithSIPMinutes struct{}

func (planWithSIPMinutes) ListForWorkspace(string) ([]workspace_pricing.PricingItem, error) {
	return []workspace_pricing.PricingItem{{
		Category:    workspace_pricing.CategoryTelephony,
		Service:     workspace_pricing.TelephonyServiceSIPCalls,
		Metric:      "per_minute",
		CostMicros:  1_000,
		PriceMicros: 4_000,
		Currency:    "USD",
	}}, nil
}

func newBillingConsumer() (*ConsumeCallBillingUseCase, *recordingLedger) {
	ledger := &recordingLedger{}
	pricer := workspace_pricing.NewPricer(catalogDefaults{}, workspace_pricing.WithPlanPricingProvider(planWithSIPMinutes{}))
	return NewConsumeCallBillingUseCase(nil, &memoryBillingRecords{records: map[string]*billing.CallBillingRecord{}}, ledger, pricer, nil), ledger
}

func completedCall(callID, channel string, talk time.Duration) billing.CallCompletedEvent {
	answered := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	end := answered.Add(talk)
	return billing.CallCompletedEvent{
		CallID:      callID,
		WorkspaceID: "ws-1",
		CallSource:  billing.CallSourceWebSocket,
		Channel:     channel,
		CallStart:   answered,
		CallEnd:     end,
		DurationSec: billing.BillableSeconds(answered, end),
	}
}

func TestSIPCallsAreDebitedInWholeMinutesAtThePlanRate(t *testing.T) {
	cases := []struct {
		talk    time.Duration
		minutes int64
	}{
		{400 * time.Millisecond, 1},
		{60 * time.Second, 1},
		{61 * time.Second, 2},
		{2*time.Minute + time.Second, 3},
	}
	for _, tc := range cases {
		consumer, ledger := newBillingConsumer()
		if err := consumer.processEvent(completedCall("sip-out-1", workspace_pricing.TelephonyChannelSIP, tc.talk)); err != nil {
			t.Fatalf("talk %v: processEvent() error = %v", tc.talk, err)
		}
		if len(ledger.debits) != 1 {
			t.Fatalf("talk %v: debits = %d, want 1", tc.talk, len(ledger.debits))
		}
		debit := ledger.debits[0]
		if debit.Amount != tc.minutes*4_000 || debit.CostMicros != tc.minutes*1_000 {
			t.Errorf("talk %v debited %d (cost %d), want %d minutes at the plan rate", tc.talk, debit.Amount, debit.CostMicros, tc.minutes)
		}
		if debit.ServiceType != balance.ServiceVoiceCall || debit.ReferenceID == nil || *debit.ReferenceID != "sip-out-1" {
			t.Errorf("debit = %+v, want a voice call debit referenced by the call id", debit)
		}
	}
}

func TestARedeliveredBillingEventIsNotChargedTwice(t *testing.T) {
	consumer, ledger := newBillingConsumer()
	event := completedCall("sip-out-2", workspace_pricing.TelephonyChannelSIP, 90*time.Second)
	for i := 0; i < 3; i++ {
		if err := consumer.processEvent(event); err != nil {
			t.Fatalf("delivery %d error = %v", i, err)
		}
	}
	if len(ledger.debits) != 1 {
		t.Fatalf("debits after redelivery = %d, want 1", len(ledger.debits))
	}
}

func TestQueuedWhatsAppEventsWithoutAChannelKeepTheirWhatsAppPrice(t *testing.T) {
	consumer, ledger := newBillingConsumer()
	if err := consumer.processEvent(completedCall("wa-call-123", "", 30*time.Second)); err != nil {
		t.Fatalf("processEvent() error = %v", err)
	}
	whatsappMinute := int64(0)
	for _, item := range workspace_pricing.DefaultPricingCatalog {
		if item.Service == workspace_pricing.TelephonyServiceWhatsAppCalls {
			whatsappMinute = item.PriceMicros
		}
	}
	if len(ledger.debits) != 1 || ledger.debits[0].Amount != whatsappMinute {
		t.Fatalf("debits = %+v, want one WhatsApp minute (%d)", ledger.debits, whatsappMinute)
	}
}

func TestAnEventWithoutAKnownChannelFailsInsteadOfGuessingAPrice(t *testing.T) {
	consumer, ledger := newBillingConsumer()
	err := consumer.processEvent(completedCall("mystery-1", "", 30*time.Second))
	if !errors.Is(err, workspace_pricing.ErrPricingItemNotFound) {
		t.Fatalf("processEvent() error = %v, want ErrPricingItemNotFound so the event is retried and flagged", err)
	}
	if len(ledger.debits) != 0 {
		t.Fatal("a call with an unknown channel was charged")
	}
}
