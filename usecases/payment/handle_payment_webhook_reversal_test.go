package payment_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vozko/domain/balance"
	"vozko/domain/invoice"
	"vozko/domain/notification"
	"vozko/domain/payment"
	"vozko/domain/workspace"
	"vozko/usecases/notification/notificationtest"
)

type ledgerBook struct {
	net      map[string]int64
	debits   []balance.DebitBalanceInput
	credits  []balance.CreditBalanceInput
	debitErr error
	readErr  error
}

func newLedgerBook() *ledgerBook { return &ledgerBook{net: map[string]int64{}} }

func (b *ledgerBook) key(workspaceID, reference string) string { return workspaceID + "|" + reference }

func (b *ledgerBook) NetCreditUnder(workspaceID, referenceID string) (int64, error) {
	if b.readErr != nil {
		return 0, b.readErr
	}
	return b.net[b.key(workspaceID, referenceID)], nil
}

type bookDebit struct{ book *ledgerBook }

func (d bookDebit) Execute(in balance.DebitBalanceInput) (*balance.Transaction, error) {
	if d.book.debitErr != nil {
		return nil, d.book.debitErr
	}
	d.book.debits = append(d.book.debits, in)
	d.book.net[d.book.key(in.WorkspaceID, *in.ReferenceID)] -= in.Amount
	return &balance.Transaction{}, nil
}

type bookCredit struct{ book *ledgerBook }

func (c bookCredit) Execute(in balance.CreditBalanceInput) (*balance.Transaction, error) {
	c.book.credits = append(c.book.credits, in)
	c.book.net[c.book.key(in.WorkspaceID, *in.ReferenceID)] += in.Amount
	return &balance.Transaction{}, nil
}

type fakeLocks struct {
	keys []string
	err  error
}

func (l *fakeLocks) Acquire(_ context.Context, key string) (func() (bool, error), error) {
	if l.err != nil {
		return nil, l.err
	}
	l.keys = append(l.keys, key)
	return func() (bool, error) { return true, nil }, nil
}

type fakeAlerts struct{ subjects []string }

func (a *fakeAlerts) Alert(_ context.Context, subject, _ string) error {
	a.subjects = append(a.subjects, subject)
	return nil
}

type reversalRig struct {
	repo      *webhookInvoiceRepo
	book      *ledgerBook
	locks     *fakeLocks
	alerts    *fakeAlerts
	publisher *notificationtest.Publisher
	uc        *handlePaymentWebhookUseCase
}

func newReversalRig(inv *invoice.Invoice) *reversalRig {
	rig := &reversalRig{
		repo:      &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{inv.ExternalID: inv}},
		book:      newLedgerBook(),
		locks:     &fakeLocks{},
		alerts:    &fakeAlerts{},
		publisher: &notificationtest.Publisher{},
	}
	rig.uc = NewHandlePaymentWebhookUseCase(rig.repo, nil, bookCredit{rig.book}, bookDebit{rig.book}, nil).
		WithNotifications(rig.publisher, "https://app.vozko.test").
		WithReversals(rig.book, rig.locks, rig.alerts)
	return rig
}

func (r *reversalRig) credit(inv *invoice.Invoice, micros int64) {
	r.book.net[r.book.key(inv.WorkspaceID, inv.ID)] += micros
}

func (r *reversalRig) reversed(inv *invoice.Invoice) int64 {
	return -r.book.net[r.book.key(inv.WorkspaceID, invoice.ReversalReference(inv.ID))]
}

func (r *reversalRig) send(t *testing.T, event string, reversedValue float64) error {
	t.Helper()
	return r.uc.Execute(&payment.WebhookEvent{
		Event:    event,
		Provider: payment.ProviderAsaas,
		Payment:  payment.WebhookPayment{ID: "pay-1", ReversedValue: reversedValue},
	})
}

func paidPixTopUp() *invoice.Invoice {
	return &invoice.Invoice{
		ID: "inv-1", ExternalID: "pay-1", WorkspaceID: "ws-1", Purpose: invoice.PurposeTopUp,
		Status: invoice.StatusPaid, BillingType: "PIX", AmountBRL: 50, AmountUSD: 10_000_000, ExchangeRate: 5,
		Provider: payment.ProviderAsaas,
	}
}

func TestAChargebackOfSpentBalanceIsDebitedIntoNegative(t *testing.T) {
	inv := paidPixTopUp()
	inv.BillingType = "CREDIT_CARD"
	rig := newReversalRig(inv)
	rig.credit(inv, 10_000_000)

	if err := rig.send(t, payment.EventPaymentChargebackRequested, 0); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(rig.book.debits) != 1 {
		t.Fatalf("expected one debit, got %d", len(rig.book.debits))
	}
	d := rig.book.debits[0]
	if d.Amount != 10_000_000 || !d.AllowNegative || !d.IsRefund || *d.ReferenceID != invoice.ReversalReference(inv.ID) {
		t.Fatalf("debit = %+v; the whole credit comes back even if it was already spent", d)
	}
	if inv.Status != invoice.StatusReversed {
		t.Fatalf("status = %s, want REVERSED", inv.Status)
	}
}

func TestAPixMedReturnIsDebitedLikeAChargeback(t *testing.T) {
	inv := paidPixTopUp()
	rig := newReversalRig(inv)
	rig.credit(inv, 10_000_000)

	if err := rig.send(t, payment.EventPaymentRefunded, 0); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if rig.reversed(inv) != 10_000_000 {
		t.Fatalf("reversed = %d, want the whole credit", rig.reversed(inv))
	}
	draft := rig.publisher.Only(t)
	if !strings.Contains(strings.ToLower(draft.Title), "pix") {
		t.Fatalf("a PIX return must be named as one, got %q", draft.Title)
	}
}

func TestEveryReversalStageMovesTheMoneyOnce(t *testing.T) {
	inv := paidPixTopUp()
	inv.BillingType = "CREDIT_CARD"
	rig := newReversalRig(inv)
	rig.credit(inv, 10_000_000)

	for _, event := range []string{
		payment.EventPaymentChargebackRequested,
		payment.EventPaymentChargebackRequested,
		payment.EventPaymentChargebackDispute,
		payment.EventPaymentRefunded,
	} {
		if err := rig.send(t, event, 0); err != nil {
			t.Fatalf("%s: %v", event, err)
		}
	}
	if len(rig.book.debits) != 1 || rig.reversed(inv) != 10_000_000 {
		t.Fatalf("debits = %d, reversed = %d; opened, redelivered, disputed and final must take the credit back once", len(rig.book.debits), rig.reversed(inv))
	}
}

func TestAPartialReturnTakesBackItsShareThenOnlyTheRest(t *testing.T) {
	inv := paidPixTopUp()
	rig := newReversalRig(inv)
	rig.credit(inv, 10_000_000)

	if err := rig.send(t, payment.EventPaymentPartiallyRefunded, 20); err != nil {
		t.Fatal(err)
	}
	if rig.reversed(inv) != 4_000_000 {
		t.Fatalf("after R$ 20 of R$ 50, reversed = %d, want 4000000", rig.reversed(inv))
	}
	if err := rig.send(t, payment.EventPaymentRefunded, 50); err != nil {
		t.Fatal(err)
	}
	if rig.reversed(inv) != 10_000_000 || len(rig.book.debits) != 2 || rig.book.debits[1].Amount != 6_000_000 {
		t.Fatalf("the full return must only add the difference, reversed = %d, debits = %+v", rig.reversed(inv), rig.book.debits)
	}
}

func TestAPartialReturnWithoutItsAmountTakesBackEverythingAndAlerts(t *testing.T) {
	inv := paidPixTopUp()
	rig := newReversalRig(inv)
	rig.credit(inv, 10_000_000)

	if err := rig.send(t, payment.EventPaymentPartiallyRefunded, 0); err != nil {
		t.Fatal(err)
	}
	if rig.reversed(inv) != 10_000_000 {
		t.Fatalf("an unknown share must take back everything, reversed = %d", rig.reversed(inv))
	}
	if !rig.alerted("partial") {
		t.Fatalf("ops must review a partial return whose amount the provider did not send, alerts = %v", rig.alerts.subjects)
	}
}

func TestAReversalOfANeverCreditedInvoiceDebitsNothingAndAlerts(t *testing.T) {
	inv := paidPixTopUp()
	inv.Status = invoice.StatusPending
	rig := newReversalRig(inv)

	if err := rig.send(t, payment.EventPaymentRefunded, 0); err != nil {
		t.Fatal(err)
	}
	if len(rig.book.debits) != 0 {
		t.Fatal("nothing was credited, so nothing may be debited")
	}
	if inv.Status != invoice.StatusReversed {
		t.Fatalf("status = %s; a later payment event must not credit a reversed charge", inv.Status)
	}
	if !rig.alerted("never credited") {
		t.Fatalf("alerts = %v", rig.alerts.subjects)
	}
}

func TestAPaymentArrivingAfterAReversalOfANeverCreditedInvoiceCreditsNothing(t *testing.T) {
	inv := paidPixTopUp()
	inv.Status = invoice.StatusReversed
	rig := newReversalRig(inv)

	if err := rig.send(t, payment.EventPaymentReceived, 0); err != nil {
		t.Fatal(err)
	}
	if len(rig.book.credits) != 0 || len(rig.repo.markPaidCalls) != 0 {
		t.Fatalf("credits = %d, markPaid = %d; nothing was reversed, so nothing is released", len(rig.book.credits), len(rig.repo.markPaidCalls))
	}
}

func TestALegacyRefundedInvoiceIsNotDebitedAgain(t *testing.T) {
	inv := paidPixTopUp()
	inv.Status = invoice.StatusRefunded
	rig := newReversalRig(inv)

	if err := rig.send(t, payment.EventPaymentRefunded, 0); err != nil {
		t.Fatal(err)
	}
	if len(rig.book.debits) != 0 || inv.Status != invoice.StatusRefunded {
		t.Fatalf("debits = %d status = %s; the old refund path already took it back", len(rig.book.debits), inv.Status)
	}
}

func TestAFailedReversalDebitIsReturnedForRetry(t *testing.T) {
	inv := paidPixTopUp()
	rig := newReversalRig(inv)
	rig.credit(inv, 10_000_000)
	rig.book.debitErr = errors.New("db down")

	if err := rig.send(t, payment.EventPaymentChargebackRequested, 0); err == nil {
		t.Fatal("a failed debit must be returned so the event is retried, never only logged")
	}
	if inv.Status == invoice.StatusReversed {
		t.Fatal("the invoice must not look reversed while the money was not taken back")
	}
}

func TestAReversalWithoutItsLockIsRetried(t *testing.T) {
	inv := paidPixTopUp()
	rig := newReversalRig(inv)
	rig.credit(inv, 10_000_000)
	rig.locks.err = errors.New("redis down")

	if err := rig.send(t, payment.EventPaymentChargebackRequested, 0); err == nil {
		t.Fatal("without the per-invoice lock two stages could race; the event must be retried")
	}
	if len(rig.book.debits) != 0 {
		t.Fatal("nothing may move without the lock")
	}
}

func TestAReleasedReversalCreditsBackOnce(t *testing.T) {
	inv := paidPixTopUp()
	inv.BillingType = "CREDIT_CARD"
	rig := newReversalRig(inv)
	rig.credit(inv, 10_000_000)

	steps := []string{
		payment.EventPaymentChargebackRequested,
		payment.EventPaymentAwaitingChargebackReversal,
		payment.EventPaymentReceived,
		payment.EventPaymentReceived,
		payment.EventPaymentChargebackReversed,
	}
	for _, event := range steps {
		if err := rig.send(t, event, 0); err != nil {
			t.Fatalf("%s: %v", event, err)
		}
	}
	if len(rig.book.credits) != 1 || rig.reversed(inv) != 0 {
		t.Fatalf("credits = %d, reversed = %d; a won dispute gives the credit back exactly once", len(rig.book.credits), rig.reversed(inv))
	}
	if inv.Status != invoice.StatusPaid {
		t.Fatalf("status = %s, want PAID again", inv.Status)
	}
}

func TestAMercadoPagoReleaseWorksWithoutAReceivedEvent(t *testing.T) {
	inv := paidPixTopUp()
	inv.BillingType = "CREDIT_CARD"
	rig := newReversalRig(inv)
	rig.credit(inv, 10_000_000)

	for _, event := range []string{payment.EventPaymentChargebackRequested, payment.EventPaymentChargebackReversed} {
		if err := rig.send(t, event, 0); err != nil {
			t.Fatal(err)
		}
	}
	if rig.reversed(inv) != 0 || inv.Status != invoice.StatusPaid {
		t.Fatalf("reversed = %d status = %s", rig.reversed(inv), inv.Status)
	}
}

func TestAReversalTakesBackWhatEachPurposeCredited(t *testing.T) {
	planID := "plan-1"
	for _, inv := range []*invoice.Invoice{
		{ID: "inv-s", ExternalID: "pay-1", WorkspaceID: "ws-1", Purpose: invoice.PurposeSubscription, PlanDefinitionID: &planID, Status: invoice.StatusPaid, BillingType: "PIX", AmountBRL: 48},
		{ID: "inv-m", ExternalID: "pay-1", WorkspaceID: "ws-1", Purpose: invoice.PurposeMonthlyBilling, Status: invoice.StatusPaid, BillingType: "PIX", AmountBRL: 60, AmountUSD: 10_000, CreditableUSD: 4_000},
	} {
		t.Run(string(inv.Purpose), func(t *testing.T) {
			rig := newReversalRig(inv)
			rig.credit(inv, 4_000)

			if err := rig.send(t, payment.EventPaymentRefunded, 0); err != nil {
				t.Fatal(err)
			}
			if rig.reversed(inv) != 4_000 {
				t.Fatalf("reversed = %d; only what the ledger credited comes back, never the invoice total", rig.reversed(inv))
			}
		})
	}
}

func TestReversalsAreRefusedWhenTheirDependenciesAreMissing(t *testing.T) {
	inv := paidPixTopUp()
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{"pay-1": inv}}
	uc := NewHandlePaymentWebhookUseCase(repo, nil, &webhookCreditBalance{}, &webhookDebitBalance{}, nil)

	err := uc.Execute(&payment.WebhookEvent{Event: payment.EventPaymentChargebackRequested, Provider: payment.ProviderAsaas, Payment: payment.WebhookPayment{ID: "pay-1"}})
	if err == nil {
		t.Fatal("a reversal with no ledger, lock or alerts wired must fail and be retried, not be skipped")
	}
}

func TestEveryReversalNotifiesTheBillingHoldersAndOps(t *testing.T) {
	inv := paidPixTopUp()
	inv.BillingType = "CREDIT_CARD"
	rig := newReversalRig(inv)
	rig.credit(inv, 10_000_000)

	if err := rig.send(t, payment.EventPaymentChargebackRequested, 0); err != nil {
		t.Fatal(err)
	}
	draft := rig.publisher.Only(t)
	if draft.Category != notification.CategoryBilling || !draft.Email || draft.Severity != notification.SeverityWarning {
		t.Fatalf("draft = %+v", draft)
	}
	if !draft.Audience.Equal(notification.Holding(workspace.ResourceBalance, workspace.ActionRead)) {
		t.Fatalf("audience = %+v", draft.Audience)
	}
	if !strings.Contains(draft.Body, "R$ 50,00") || !strings.Contains(strings.ToLower(draft.Title), "contesta") {
		t.Fatalf("title %q body %q", draft.Title, draft.Body)
	}
	if !rig.alerted("chargeback") {
		t.Fatalf("every chargeback is contested, so ops must hear about it, alerts = %v", rig.alerts.subjects)
	}
}

func TestAnUnknownPaymentEventIsNeverDroppedSilently(t *testing.T) {
	inv := paidPixTopUp()
	rig := newReversalRig(inv)

	err := rig.send(t, "PAYMENT_SOMETHING_NEW", 0)
	if !errors.Is(err, payment.ErrUnmappedEvent) {
		t.Fatalf("expected ErrUnmappedEvent so the event is retried, got %v", err)
	}
	if !rig.alerted("unmapped") {
		t.Fatalf("alerts = %v", rig.alerts.subjects)
	}
}

func TestAnInvoiceWithAnUnknownPurposeIsRefusedAndAlerted(t *testing.T) {
	inv := paidPixTopUp()
	inv.Status = invoice.StatusPending
	inv.Purpose = "SOMETHING"
	rig := newReversalRig(inv)

	if err := rig.send(t, payment.EventPaymentReceived, 0); !errors.Is(err, invoice.ErrUnknownPurpose) {
		t.Fatalf("expected ErrUnknownPurpose, got %v", err)
	}
	if len(rig.book.credits) != 0 {
		t.Fatal("an unknown purpose must never become a top-up credit")
	}
	if !rig.alerted("purpose") {
		t.Fatalf("alerts = %v", rig.alerts.subjects)
	}
}

func TestAPaymentForAReservationIsFoundByItsExternalReference(t *testing.T) {
	inv := paidPixTopUp()
	inv.Status = invoice.StatusPending
	inv.ExternalID = ""
	rig := newReversalRig(inv)
	rig.repo.byID = map[string]*invoice.Invoice{inv.ID: inv}

	err := rig.uc.Execute(&payment.WebhookEvent{
		Event: payment.EventPaymentReceived, Provider: payment.ProviderAsaas,
		Payment: payment.WebhookPayment{ID: "pay-late", ExternalReference: invoice.ExternalReference(inv.ID)},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(rig.book.credits) != 1 {
		t.Fatalf("a charge whose id was never saved must still be credited, credits = %d", len(rig.book.credits))
	}
	if rig.repo.attached[inv.ID] != "pay-late" {
		t.Fatalf("the charge id must be attached so later events match directly, got %q", rig.repo.attached[inv.ID])
	}
}

func TestAPaymentForAnotherChargeOfTheSameInvoiceIsNotCreditedTwice(t *testing.T) {
	inv := paidPixTopUp()
	inv.Status = invoice.StatusPending
	rig := newReversalRig(inv)
	rig.repo.byID = map[string]*invoice.Invoice{inv.ID: inv}

	err := rig.uc.Execute(&payment.WebhookEvent{
		Event: payment.EventPaymentReceived, Provider: payment.ProviderAsaas,
		Payment: payment.WebhookPayment{ID: "pay-second", ExternalReference: invoice.ExternalReference(inv.ID)},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(rig.book.credits) != 0 || !rig.alerted("second charge") {
		t.Fatalf("credits = %d alerts = %v; ops must sort out a second paid charge by hand", len(rig.book.credits), rig.alerts.subjects)
	}
}

func TestTheLookupIsScopedToTheEventProvider(t *testing.T) {
	inv := paidPixTopUp()
	inv.Status = invoice.StatusPending
	rig := newReversalRig(inv)

	err := rig.uc.Execute(&payment.WebhookEvent{
		Event: payment.EventPaymentReceived, Provider: payment.ProviderMercadoPago,
		Payment: payment.WebhookPayment{ID: "pay-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rig.book.credits) != 0 {
		t.Fatal("a Mercado Pago event must never settle an Asaas charge that happens to share its id")
	}
}

func (r *reversalRig) alerted(fragment string) bool {
	for _, subject := range r.alerts.subjects {
		if strings.Contains(strings.ToLower(subject), fragment) {
			return true
		}
	}
	return false
}

func TestAReversalWhoseStatusCannotBeSavedIsRetriedWithoutTakingTwice(t *testing.T) {
	inv := paidPixTopUp()
	rig := newReversalRig(inv)
	rig.credit(inv, 10_000_000)
	failing := &errWebhookInvoiceRepo{webhookInvoiceRepo: *rig.repo, updateErr: errors.New("db blip")}
	failing.byExternal = map[string]*invoice.Invoice{"pay-1": inv}
	rig.uc.invoiceRepo = failing

	if err := rig.send(t, payment.EventPaymentRefunded, 0); err == nil {
		t.Fatal("a reversal whose status could not be saved must be retried")
	}
	rig.uc.invoiceRepo = rig.repo
	if err := rig.send(t, payment.EventPaymentRefunded, 0); err != nil {
		t.Fatal(err)
	}
	if len(rig.book.debits) != 1 || rig.reversed(inv) != 10_000_000 {
		t.Fatalf("debits = %d reversed = %d; the retry reads the ledger and takes nothing twice", len(rig.book.debits), rig.reversed(inv))
	}
}
