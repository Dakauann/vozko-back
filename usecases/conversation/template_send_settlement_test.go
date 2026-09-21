package conversation_usecase

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"vozko/domain/balance"
	"vozko/domain/conversation"
	whatsapp_template "vozko/domain/whatsapp/template"
)

type settlementAttempts struct {
	mu       sync.Mutex
	byID     map[string]*whatsapp_template.SendAttempt
	byWamid  map[string]*whatsapp_template.SendAttempt
	sentIDs  []string
	refunded []string
}

func newSettlementAttempts() *settlementAttempts {
	return &settlementAttempts{
		byID:    map[string]*whatsapp_template.SendAttempt{},
		byWamid: map[string]*whatsapp_template.SendAttempt{},
	}
}

func (s *settlementAttempts) add(a whatsapp_template.SendAttempt, wamid string) *whatsapp_template.SendAttempt {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := a
	s.byID[stored.ID] = &stored
	if wamid != "" {
		s.byWamid[wamid] = &stored
	}
	return &stored
}

func (s *settlementAttempts) CreateIfAbsent(context.Context, *whatsapp_template.SendAttempt) (*whatsapp_template.SendAttempt, bool, error) {
	return nil, false, nil
}

func (s *settlementAttempts) FindByID(_ context.Context, id string) (*whatsapp_template.SendAttempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a, ok := s.byID[id]; ok {
		copied := *a
		return &copied, nil
	}
	return nil, nil
}

func (s *settlementAttempts) FindByIdempotencyKey(context.Context, string, string) (*whatsapp_template.SendAttempt, error) {
	return nil, nil
}

func (s *settlementAttempts) FindByProviderMessageID(_ context.Context, _ string, wamid string) (*whatsapp_template.SendAttempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a, ok := s.byWamid[wamid]; ok {
		copied := *a
		return &copied, nil
	}
	return nil, nil
}

func (s *settlementAttempts) MarkCharged(context.Context, string, int64, time.Time) error { return nil }

func (s *settlementAttempts) MarkSent(_ context.Context, id, wamid string, _ int, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.byID[id]
	if !ok {
		return whatsapp_template.ErrSendAttemptConflict
	}
	if !a.Status.CanTransitionTo(whatsapp_template.SendAttemptSent) {
		return whatsapp_template.ErrSendAttemptConflict
	}
	a.Status = whatsapp_template.SendAttemptSent
	a.ProviderMessageID = wamid
	s.sentIDs = append(s.sentIDs, id)
	return nil
}

func (s *settlementAttempts) MarkRejected(context.Context, string, int, string, int) error {
	return nil
}
func (s *settlementAttempts) MarkUnknown(context.Context, string, string, int) error { return nil }

func (s *settlementAttempts) MarkRefunded(_ context.Context, id string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.byID[id]
	if !ok {
		return whatsapp_template.ErrSendAttemptConflict
	}
	if !a.Status.CanTransitionTo(whatsapp_template.SendAttemptRefunded) {
		return whatsapp_template.ErrSendAttemptConflict
	}
	a.Status = whatsapp_template.SendAttemptRefunded
	s.refunded = append(s.refunded, id)
	return nil
}

func (s *settlementAttempts) ListNeedingReconciliation(context.Context, time.Time, int) ([]*whatsapp_template.SendAttempt, error) {
	return nil, nil
}

func (s *settlementAttempts) statusOf(id string) whatsapp_template.SendAttemptStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a, ok := s.byID[id]; ok {
		return a.Status
	}
	return ""
}

func (s *settlementAttempts) markedRefunded() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.refunded)
}

type settlementBilling struct {
	mu         sync.Mutex
	refunds    []string
	categories []string
	err        error
}

func (b *settlementBilling) Execute(string, string, string) (*balance.Transaction, error) {
	return &balance.Transaction{}, nil
}

func (b *settlementBilling) Refund(_ string, ref string, category string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.err != nil {
		return b.err
	}
	b.refunds = append(b.refunds, ref)
	b.categories = append(b.categories, category)
	return nil
}

func (b *settlementBilling) GetTemplateCostMicros(string, string) (int64, error) { return 5_000, nil }

func (b *settlementBilling) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.refunds)
}

type settlementLedger struct {
	balance.Repository
	mu   sync.Mutex
	refs map[string]bool
}

func (l *settlementLedger) ExistsTransactionByReferenceID(ref string) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.refs[ref], nil
}

func newSettlementUC(attempts *settlementAttempts, billing *settlementBilling, ledger *settlementLedger) *handleWhatsAppMessageUseCase {
	return &handleWhatsAppMessageUseCase{
		templateSendAttempts:    attempts,
		consumeWhatsappTemplate: billing,
		balanceLedger:           ledger,
	}
}

func chargedAttempt(id string) whatsapp_template.SendAttempt {
	return whatsapp_template.SendAttempt{
		ID:            id,
		WorkspaceID:   "ws-1",
		Category:      "UTILITY",
		ChargedMicros: 5_000,
		Status:        whatsapp_template.SendAttemptCharged,
	}
}

func TestSettlement_SuccessStatus_PromotesToSentSoTheSweepSkipsIt(t *testing.T) {
	for _, status := range []string{"sent", "delivered", "read"} {
		t.Run(status, func(t *testing.T) {
			attempts := newSettlementAttempts()
			attempts.add(chargedAttempt("att-1"), "")
			uc := newSettlementUC(attempts, &settlementBilling{}, &settlementLedger{refs: map[string]bool{}})

			deliveryStatus, ok := mapWhatsAppWebhookDeliveryStatus(status)
			if !ok {
				t.Fatalf("%q should map to a delivery status", status)
			}
			uc.settleTemplateSendAttempt(
				conversation.WhatsAppStatus{ID: "wamid.1", Status: status, BizOpaqueCallbackData: "att-1"},
				deliveryStatus, true,
			)

			if got := attempts.statusOf("att-1"); got != whatsapp_template.SendAttemptSent {
				t.Fatalf("status = %q, want sent — otherwise the sweep refunds a delivered message", got)
			}
		})
	}
}

func TestSettlement_PromotesAnUnknownAttempt(t *testing.T) {
	attempts := newSettlementAttempts()
	unknown := chargedAttempt("att-1")
	unknown.Status = whatsapp_template.SendAttemptUnknown
	attempts.add(unknown, "")
	uc := newSettlementUC(attempts, &settlementBilling{}, &settlementLedger{refs: map[string]bool{}})

	uc.settleTemplateSendAttempt(
		conversation.WhatsAppStatus{ID: "wamid.1", Status: "delivered", BizOpaqueCallbackData: "att-1"},
		conversation.DeliveryStatusDelivered, true,
	)

	if got := attempts.statusOf("att-1"); got != whatsapp_template.SendAttemptSent {
		t.Fatalf("status = %q, want sent — an unknown outcome that demonstrably arrived is settled", got)
	}
}

func TestSettlement_AlreadyRefunded_IsNotPromoted(t *testing.T) {
	attempts := newSettlementAttempts()
	refunded := chargedAttempt("att-1")
	refunded.Status = whatsapp_template.SendAttemptRefunded
	attempts.add(refunded, "")
	uc := newSettlementUC(attempts, &settlementBilling{}, &settlementLedger{refs: map[string]bool{}})

	uc.settleTemplateSendAttempt(
		conversation.WhatsAppStatus{ID: "wamid.1", Status: "delivered", BizOpaqueCallbackData: "att-1"},
		conversation.DeliveryStatusDelivered, true,
	)

	if got := attempts.statusOf("att-1"); got != whatsapp_template.SendAttemptRefunded {
		t.Fatalf("status = %q, want refunded to stay terminal", got)
	}
}

func TestSettlement_FailedStatus_RefundsViaCorrelationIDWithNoWamidStored(t *testing.T) {
	attempts := newSettlementAttempts()
	attempts.add(chargedAttempt("att-1"), "")
	billing := &settlementBilling{}
	uc := newSettlementUC(attempts, billing, &settlementLedger{refs: map[string]bool{}})

	uc.settleTemplateSendAttempt(
		conversation.WhatsAppStatus{ID: "wamid.unknown", Status: "failed", BizOpaqueCallbackData: "att-1"},
		conversation.DeliveryStatusFailed, true,
	)

	if billing.count() != 1 {
		t.Fatalf("refunded %d times, want 1 — the correlation id is the only way to find this attempt", billing.count())
	}
	if billing.refunds[0] != "waba:att-1" {
		t.Fatalf("refund reference = %q, want waba:att-1", billing.refunds[0])
	}
	if got := attempts.statusOf("att-1"); got != whatsapp_template.SendAttemptRefunded {
		t.Fatalf("status = %q, want refunded", got)
	}
}

func TestSettlement_FailedStatus_FallsBackToTheMessageID(t *testing.T) {
	attempts := newSettlementAttempts()
	attempts.add(chargedAttempt("att-1"), "wamid.1")
	billing := &settlementBilling{}
	uc := newSettlementUC(attempts, billing, &settlementLedger{refs: map[string]bool{}})

	uc.settleTemplateSendAttempt(
		conversation.WhatsAppStatus{ID: "wamid.1", Status: "failed"},
		conversation.DeliveryStatusFailed, true,
	)

	if billing.count() != 1 {
		t.Fatalf("refunded %d times, want 1", billing.count())
	}
}

func TestSettlement_FailedStatusForASentAttempt_IsRefunded(t *testing.T) {
	attempts := newSettlementAttempts()
	sent := chargedAttempt("att-1")
	sent.Status = whatsapp_template.SendAttemptSent
	attempts.add(sent, "wamid.1")
	billing := &settlementBilling{}
	uc := newSettlementUC(attempts, billing, &settlementLedger{refs: map[string]bool{}})

	uc.settleTemplateSendAttempt(
		conversation.WhatsAppStatus{ID: "wamid.1", Status: "failed", BizOpaqueCallbackData: "att-1"},
		conversation.DeliveryStatusFailed, true,
	)

	if billing.count() != 1 {
		t.Fatalf("refunded %d times, want 1 — an accepted send that failed delivery was never received", billing.count())
	}
	if got := attempts.statusOf("att-1"); got != whatsapp_template.SendAttemptRefunded {
		t.Fatalf("status = %q, want refunded — an unmarked refund is replayed as a charge", got)
	}
	if attempts.markedRefunded() != 1 {
		t.Fatal("the refund must be recorded on the attempt, not only in the ledger")
	}
}

func TestSettlement_FailedStatusTwice_RefundsOnce(t *testing.T) {
	attempts := newSettlementAttempts()
	attempts.add(chargedAttempt("att-1"), "wamid.1")
	billing := &settlementBilling{}
	ledger := &settlementLedger{refs: map[string]bool{}}
	uc := newSettlementUC(attempts, billing, ledger)

	status := conversation.WhatsAppStatus{ID: "wamid.1", Status: "failed", BizOpaqueCallbackData: "att-1"}
	uc.settleTemplateSendAttempt(status, conversation.DeliveryStatusFailed, true)
	ledger.refs["refund:waba:att-1"] = true
	uc.settleTemplateSendAttempt(status, conversation.DeliveryStatusFailed, true)

	if billing.count() != 1 {
		t.Fatalf("refunded %d times across a redelivered webhook, want 1", billing.count())
	}
}

func TestSettlement_RefundsUnderTheStoredCategoryNotMetasPricing(t *testing.T) {
	attempts := newSettlementAttempts()
	stored := chargedAttempt("att-1")
	stored.Category = "UTILITY"
	attempts.add(stored, "wamid.1")
	billing := &settlementBilling{}
	uc := newSettlementUC(attempts, billing, &settlementLedger{refs: map[string]bool{}})

	uc.settleTemplateSendAttempt(
		conversation.WhatsAppStatus{
			ID: "wamid.1", Status: "failed", BizOpaqueCallbackData: "att-1",
			Pricing: &conversation.WhatsAppPricing{Category: "MARKETING"},
		},
		conversation.DeliveryStatusFailed, true,
	)

	if len(billing.categories) != 1 || billing.categories[0] != "UTILITY" {
		t.Fatalf("refund categories = %v, want the stored UTILITY — refunding at Meta's re-categorised price loses the difference", billing.categories)
	}
}

func TestSettlement_RefundFailure_LeavesTheAttemptUnsettled(t *testing.T) {
	attempts := newSettlementAttempts()
	attempts.add(chargedAttempt("att-1"), "wamid.1")
	billing := &settlementBilling{err: errors.New("ledger down")}
	uc := newSettlementUC(attempts, billing, &settlementLedger{refs: map[string]bool{}})

	uc.settleTemplateSendAttempt(
		conversation.WhatsAppStatus{ID: "wamid.1", Status: "failed", BizOpaqueCallbackData: "att-1"},
		conversation.DeliveryStatusFailed, true,
	)

	if got := attempts.statusOf("att-1"); got == whatsapp_template.SendAttemptRefunded {
		t.Fatal("a failed credit must leave the attempt for the sweep to retry")
	}
}

func TestSettlement_UnknownAttempt_DoesNothing(t *testing.T) {
	attempts := newSettlementAttempts()
	billing := &settlementBilling{}
	uc := newSettlementUC(attempts, billing, &settlementLedger{refs: map[string]bool{}})

	uc.settleTemplateSendAttempt(
		conversation.WhatsAppStatus{ID: "wamid.someone-else", Status: "failed"},
		conversation.DeliveryStatusFailed, true,
	)

	if billing.count() != 0 {
		t.Fatal("no attempt matched; nothing may be credited")
	}
}

func TestSettlement_WithoutAttemptsRepository_IsInert(t *testing.T) {
	uc := &handleWhatsAppMessageUseCase{}
	uc.settleTemplateSendAttempt(
		conversation.WhatsAppStatus{ID: "wamid.1", Status: "failed"},
		conversation.DeliveryStatusFailed, true,
	)
}
