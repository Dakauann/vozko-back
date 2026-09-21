package template_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/whatsapp/template"
)

func newSweep(t *testing.T, attempts *fakeAttempts, billing *countingBilling, ledger *fakeLedger) template.ReconcileSendAttemptsUseCase {
	t.Helper()
	return NewReconcileSendAttemptsUseCase(attempts, billing, ledger, nil)
}

func staleAttempt(id string, status template.SendAttemptStatus) template.SendAttempt {
	return template.SendAttempt{
		ID:             id,
		WorkspaceID:    "ws-1",
		IdempotencyKey: "key-" + id,
		Category:       "UTILITY",
		ChargedMicros:  5_000,
		Status:         status,
		UpdatedAt:      time.Now().UTC().Add(-3 * time.Hour),
	}
}

func TestReconcile_ChargedRow_IsRefundedAndMarked(t *testing.T) {
	attempts := newFakeAttempts()
	billing := &countingBilling{cost: 5_000}
	ledger := &fakeLedger{refs: map[string]bool{}}
	attempts.seed(staleAttempt("att-1", template.SendAttemptCharged))

	n, err := newSweep(t, attempts, billing, ledger).Execute(context.Background())
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if n != 1 {
		t.Fatalf("reconciled %d, want 1", n)
	}
	if billing.refundCount() != 1 {
		t.Fatalf("refunded %d times, want 1", billing.refundCount())
	}
	if got := attempts.statusOf("att-1"); got != template.SendAttemptRefunded {
		t.Fatalf("status = %q, want refunded — an unmarked row is swept again forever", got)
	}
}

func TestReconcile_UnknownRow_IsRefundedAndMarked(t *testing.T) {
	attempts := newFakeAttempts()
	billing := &countingBilling{cost: 5_000}
	attempts.seed(staleAttempt("att-1", template.SendAttemptUnknown))

	if _, err := newSweep(t, attempts, billing, &fakeLedger{refs: map[string]bool{}}).Execute(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if got := attempts.statusOf("att-1"); got != template.SendAttemptRefunded {
		t.Fatalf("status = %q, want refunded", got)
	}
}

func TestReconcile_RunTwice_RefundsOnce(t *testing.T) {
	attempts := newFakeAttempts()
	billing := &countingBilling{cost: 5_000}
	ledger := &fakeLedger{refs: map[string]bool{}}
	attempts.seed(staleAttempt("att-1", template.SendAttemptCharged))

	sweep := newSweep(t, attempts, billing, ledger)
	_, _ = sweep.Execute(context.Background())
	for _, ref := range billing.refunds {
		ledger.refs["refund:"+ref] = true
	}
	_, _ = sweep.Execute(context.Background())

	if got := billing.refundCount(); got != 1 {
		t.Fatalf("refunded %d times across two sweeps, want 1", got)
	}
}

func TestReconcile_AttemptThatGainedAMessageID_IsLeftAlone(t *testing.T) {
	attempts := newFakeAttempts()
	billing := &countingBilling{cost: 5_000}

	stale := staleAttempt("att-1", template.SendAttemptCharged)
	stale.ProviderMessageID = "wamid.1"
	attempts.seed(stale)

	n, err := newSweep(t, attempts, billing, &fakeLedger{refs: map[string]bool{}}).Execute(context.Background())
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if n != 0 || billing.refundCount() != 0 {
		t.Fatal("a send with a provider message id was delivered; refunding it means paying Meta and crediting the customer")
	}
}

func TestReconcile_TerminalRows_AreNotTouched(t *testing.T) {
	attempts := newFakeAttempts()
	billing := &countingBilling{cost: 5_000}

	attempts.seed(staleAttempt("sent-1", template.SendAttemptSent))
	attempts.seed(staleAttempt("refunded-1", template.SendAttemptRefunded))
	attempts.seed(staleAttempt("rejected-1", template.SendAttemptRejected))

	n, _ := newSweep(t, attempts, billing, &fakeLedger{refs: map[string]bool{}}).Execute(context.Background())
	if n != 0 || billing.refundCount() != 0 {
		t.Fatalf("reconciled %d and refunded %d, want 0 and 0", n, billing.refundCount())
	}
}

func TestReconcile_FreshRow_IsNotRefundedYet(t *testing.T) {
	attempts := newFakeAttempts()
	billing := &countingBilling{cost: 5_000}

	fresh := staleAttempt("att-1", template.SendAttemptCharged)
	fresh.UpdatedAt = time.Now().UTC()
	attempts.seed(fresh)

	n, _ := newSweep(t, attempts, billing, &fakeLedger{refs: map[string]bool{}}).Execute(context.Background())
	if n != 0 || billing.refundCount() != 0 {
		t.Fatal("a fresh attempt may still be in flight; refunding it races the send")
	}
}

func TestReconcile_RefundFailure_DoesNotMarkRefunded(t *testing.T) {
	attempts := newFakeAttempts()
	billing := &countingBilling{cost: 5_000, refundErr: errors.New("ledger unavailable")}
	attempts.seed(staleAttempt("att-1", template.SendAttemptCharged))

	n, _ := newSweep(t, attempts, billing, &fakeLedger{refs: map[string]bool{}}).Execute(context.Background())
	if n != 0 {
		t.Fatalf("counted %d reconciled, but nothing was credited", n)
	}
	if got := attempts.statusOf("att-1"); got == template.SendAttemptRefunded {
		t.Fatal("a failed refund must leave the row unsettled so the next sweep retries it")
	}
}

func TestReconcile_RefundsUnderTheStoredCategory(t *testing.T) {
	attempts := newFakeAttempts()
	billing := &countingBilling{cost: 5_000}

	stale := staleAttempt("att-1", template.SendAttemptCharged)
	stale.Category = "AUTHENTICATION"
	attempts.seed(stale)

	if _, err := newSweep(t, attempts, billing, &fakeLedger{refs: map[string]bool{}}).Execute(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(billing.refundCategories) != 1 || billing.refundCategories[0] != "AUTHENTICATION" {
		t.Fatalf("refund categories = %v, want the stored AUTHENTICATION", billing.refundCategories)
	}
}

func TestReconcile_RefundsUnderThePrefixedAttemptReference(t *testing.T) {
	attempts := newFakeAttempts()
	billing := &countingBilling{cost: 5_000}
	attempts.seed(staleAttempt("att-1", template.SendAttemptCharged))

	if _, err := newSweep(t, attempts, billing, &fakeLedger{refs: map[string]bool{}}).Execute(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(billing.refunds) != 1 || billing.refunds[0] != "waba:att-1" {
		t.Fatalf("refund references = %v, want [waba:att-1]", billing.refunds)
	}
}

func TestReconcile_AlreadyCreditedInLedger_MarksWithoutSecondCredit(t *testing.T) {
	attempts := newFakeAttempts()
	billing := &countingBilling{cost: 5_000}
	ledger := &fakeLedger{refs: map[string]bool{"refund:waba:att-1": true}}
	attempts.seed(staleAttempt("att-1", template.SendAttemptCharged))

	if _, err := newSweep(t, attempts, billing, ledger).Execute(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if billing.refundCount() != 0 {
		t.Fatal("the credit already exists; a second one is money given away")
	}
	if got := attempts.statusOf("att-1"); got != template.SendAttemptRefunded {
		t.Fatalf("status = %q, want refunded so the row stops being swept", got)
	}
}

func TestReconcile_WithoutBilling_Refuses(t *testing.T) {
	uc := NewReconcileSendAttemptsUseCase(newFakeAttempts(), nil, nil, nil)
	if _, err := uc.Execute(context.Background()); !errors.Is(err, template.ErrBillingNotConfigured) {
		t.Fatalf("want ErrBillingNotConfigured, got %v", err)
	}
}
