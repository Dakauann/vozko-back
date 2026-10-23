package payment_usecase

import (
	"errors"
	"slices"
	"testing"
	"time"

	"vozko/domain/balance"
	"vozko/domain/invoice"
	"vozko/domain/payment"
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

type webhookInvoiceRepo struct {
	byExternal    map[string]*invoice.Invoice
	bySuperseded  map[string]*invoice.Invoice
	byID          map[string]*invoice.Invoice
	attached      map[string]string
	markPaidCalls []string
	statusUpdates []invoice.Status
}

func (r *webhookInvoiceRepo) Reserve(inv *invoice.Invoice) (*invoice.Invoice, bool, error) {
	return inv, true, nil
}
func (r *webhookInvoiceRepo) AttachCharge(id string, charge invoice.ChargeAttachment) error {
	if r.attached == nil {
		r.attached = map[string]string{}
	}
	r.attached[id] = charge.ExternalID
	return nil
}
func (r *webhookInvoiceRepo) GetByID(id string) (*invoice.Invoice, error) {
	if inv, ok := r.byID[id]; ok {
		return inv, nil
	}
	return nil, invoice.ErrInvoiceNotFound
}
func (r *webhookInvoiceRepo) GetByExternalID(provider payment.Provider, externalID string) (*invoice.Invoice, error) {
	inv := r.byExternal[externalID]
	if inv != nil && inv.Provider != "" && provider != "" && inv.Provider != provider {
		return nil, nil
	}
	return inv, nil
}
func (r *webhookInvoiceRepo) GetBySupersededCharge(provider payment.Provider, externalID string) (*invoice.Invoice, error) {
	inv := r.bySuperseded[externalID]
	if inv != nil && inv.Provider != "" && provider != "" && inv.Provider != provider {
		return nil, nil
	}
	return inv, nil
}
func (r *webhookInvoiceRepo) CancelStaleReservations(time.Time) (int64, error) { return 0, nil }
func (r *webhookInvoiceRepo) OpenObligations(time.Time) ([]payment.Obligations, error) {
	return nil, nil
}
func (r *webhookInvoiceRepo) ListUnpaidByPurpose(invoice.Purpose, string, int) ([]invoice.Invoice, error) {
	return nil, nil
}
func (r *webhookInvoiceRepo) UpdateStatus(id string, status invoice.Status) error {
	r.statusUpdates = append(r.statusUpdates, status)
	if inv, ok := r.byExternalByID(id); ok {
		inv.Status = status
	}
	return nil
}
func (r *webhookInvoiceRepo) MarkPaid(id string, amountUSD int64) (bool, error) {
	r.markPaidCalls = append(r.markPaidCalls, id)
	if inv, ok := r.byExternalByID(id); ok {
		if slices.Contains(invoice.SettledStatuses(), string(inv.Status)) {
			return false, nil
		}
		inv.Status = invoice.StatusPaid
		inv.AmountUSD = amountUSD
		return true, nil
	}
	return false, nil
}
func (r *webhookInvoiceRepo) ListByWorkspace(string, int, int) ([]invoice.Invoice, int64, error) {
	return nil, 0, nil
}

func (r *webhookInvoiceRepo) byExternalByID(id string) (*invoice.Invoice, bool) {
	for _, inv := range r.byExternal {
		if inv.ID == id {
			return inv, true
		}
	}
	if inv, ok := r.byID[id]; ok {
		return inv, true
	}
	return nil, false
}

type webhookSubscribeWorkspacePlan struct {
	workspaceID  string
	planID       string
	billingCycle workspace_plan.BillingCycle
	calls        int
	err          error
}

func (u *webhookSubscribeWorkspacePlan) Execute(workspaceID, planID string, billingCycle workspace_plan.BillingCycle) (*workspace_plan.WorkspaceSubscriptionDetails, error) {
	u.calls++
	u.workspaceID = workspaceID
	u.planID = planID
	u.billingCycle = billingCycle
	if u.err != nil {
		return nil, u.err
	}
	return &workspace_plan.WorkspaceSubscriptionDetails{Subscription: &workspace_plan.WorkspaceSubscription{WorkspaceID: workspaceID, PlanDefinitionID: planID, PlanName: "Starter", MaxCallChannels: 3}}, nil
}

type webhookCreditBalance struct {
	calls []balance.CreditBalanceInput
}

func (u *webhookCreditBalance) Execute(input balance.CreditBalanceInput) (*balance.Transaction, error) {
	u.calls = append(u.calls, input)
	return &balance.Transaction{}, nil
}

type webhookDebitBalance struct {
	calls []balance.DebitBalanceInput
}

func (u *webhookDebitBalance) Execute(input balance.DebitBalanceInput) (*balance.Transaction, error) {
	u.calls = append(u.calls, input)
	return &balance.Transaction{}, nil
}

func TestHandleAsaasWebhook_SubscriptionInvoiceActivatesPlan(t *testing.T) {
	planID := "plan-1"
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"pay-1": {
			ID:               "inv-1",
			ExternalID:       "pay-1",
			WorkspaceID:      "ws-1",
			Purpose:          invoice.PurposeSubscription,
			PlanDefinitionID: &planID,
			AmountBRL:        200,
			AmountUSD:        33_333_333,
			ExchangeRate:     6,
		},
	}}
	subscribe := &webhookSubscribeWorkspacePlan{}
	credit := &webhookCreditBalance{}
	uc := NewHandlePaymentWebhookUseCase(repo, subscribe, credit, &webhookDebitBalance{}, nil)

	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_RECEIVED", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if subscribe.calls != 1 {
		t.Fatalf("expected one subscription activation, got %d", subscribe.calls)
	}
	if subscribe.workspaceID != "ws-1" || subscribe.planID != "plan-1" {
		t.Fatalf("expected activation for ws-1/plan-1, got %s/%s", subscribe.workspaceID, subscribe.planID)
	}

	if len(credit.calls) != 1 {
		t.Fatalf("expected one balance credit for subscription invoice, got %d calls", len(credit.calls))
	}
	if credit.calls[0].WorkspaceID != "ws-1" || credit.calls[0].Amount != 33_333_333 {
		t.Fatalf("unexpected credit input: %+v", credit.calls[0])
	}
}

func TestHandleAsaasWebhook_TopUpInvoiceCreditsBalance(t *testing.T) {
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"pay-2": {
			ID:           "inv-2",
			ExternalID:   "pay-2",
			WorkspaceID:  "ws-2",
			Purpose:      invoice.PurposeTopUp,
			AmountBRL:    50,
			AmountUSD:    8000000,
			ExchangeRate: 6,
		},
	}}
	subscribe := &webhookSubscribeWorkspacePlan{}
	credit := &webhookCreditBalance{}
	uc := NewHandlePaymentWebhookUseCase(repo, subscribe, credit, &webhookDebitBalance{}, nil)

	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_RECEIVED", Payment: payment.AsaasWebhookPayment{ID: "pay-2"}})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if len(credit.calls) != 1 {
		t.Fatalf("expected one balance credit call, got %d", len(credit.calls))
	}
	if subscribe.calls != 0 {
		t.Fatalf("expected no subscription activation for top-up invoice, got %d", subscribe.calls)
	}
	if credit.calls[0].WorkspaceID != "ws-2" || credit.calls[0].Amount != 8000000 {
		t.Fatalf("unexpected credit input: %+v", credit.calls[0])
	}
}

type errWebhookInvoiceRepo struct {
	webhookInvoiceRepo
	getByExtErr error
	markPaidErr error
	updateErr   error
}

func (r *errWebhookInvoiceRepo) GetByExternalID(provider payment.Provider, externalID string) (*invoice.Invoice, error) {
	if r.getByExtErr != nil {
		return nil, r.getByExtErr
	}
	return r.webhookInvoiceRepo.GetByExternalID(provider, externalID)
}

func (r *errWebhookInvoiceRepo) MarkPaid(id string, amountUSD int64) (bool, error) {
	if r.markPaidErr != nil {
		return false, r.markPaidErr
	}
	return r.webhookInvoiceRepo.MarkPaid(id, amountUSD)
}

func (r *errWebhookInvoiceRepo) UpdateStatus(id string, status invoice.Status) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	return r.webhookInvoiceRepo.UpdateStatus(id, status)
}

type errWebhookCreditBalance struct {
	err error
}

func (u *errWebhookCreditBalance) Execute(balance.CreditBalanceInput) (*balance.Transaction, error) {
	return nil, u.err
}

type errWebhookDebitBalance struct {
	err error
}

func (u *errWebhookDebitBalance) Execute(balance.DebitBalanceInput) (*balance.Transaction, error) {
	return nil, u.err
}

func TestHandleAsaasWebhook_NilEvent(t *testing.T) {
	uc := NewHandlePaymentWebhookUseCase(&webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{}}, nil, nil, nil, nil)
	if err := uc.Execute(nil); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestHandleAsaasWebhook_EmptyPaymentID(t *testing.T) {
	uc := NewHandlePaymentWebhookUseCase(&webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{}}, nil, nil, nil, nil)
	if err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_RECEIVED", Payment: payment.AsaasWebhookPayment{ID: ""}}); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestHandleAsaasWebhook_NonInvoice_NoExternalReference(t *testing.T) {
	invRepo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{}}
	uc := NewHandlePaymentWebhookUseCase(invRepo, nil, nil, nil, nil)

	err := uc.Execute(&payment.AsaasWebhookEvent{
		Event:   "PAYMENT_RECEIVED",
		Payment: payment.AsaasWebhookPayment{ID: "ext-pay"},
	})
	if err != nil {
		t.Fatalf("expected nil for no external ref, got %v", err)
	}
}

func TestHandleAsaasWebhook_TopUp_AmountUSDZero(t *testing.T) {
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeTopUp, AmountUSD: 0},
	}}
	uc := NewHandlePaymentWebhookUseCase(repo, nil, nil, nil, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_RECEIVED", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err != nil {
		t.Fatalf("expected nil for zero amount, got %v", err)
	}
}

func TestHandleAsaasWebhook_TopUp_AlreadyPaid(t *testing.T) {
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeTopUp, AmountUSD: 1000, Status: invoice.StatusPaid},
	}}
	credit := &webhookCreditBalance{}
	uc := NewHandlePaymentWebhookUseCase(repo, nil, credit, nil, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_RECEIVED", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err != nil {
		t.Fatalf("expected nil for already paid, got %v", err)
	}
	if len(credit.calls) != 0 {
		t.Fatal("should not credit when already paid")
	}
}

func TestHandleAsaasWebhook_TopUp_MarkPaidError(t *testing.T) {
	repo := &errWebhookInvoiceRepo{
		webhookInvoiceRepo: webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
			"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeTopUp, AmountUSD: 1000},
		}},
		markPaidErr: errors.New("mark paid fail"),
	}
	uc := NewHandlePaymentWebhookUseCase(repo, nil, nil, nil, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_RECEIVED", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err == nil {
		t.Fatal("expected mark paid error")
	}
}

func TestHandleAsaasWebhook_TopUp_CreditBalanceError(t *testing.T) {
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeTopUp, AmountUSD: 1000, WorkspaceID: "ws-1"},
	}}
	credit := &errWebhookCreditBalance{err: errors.New("credit fail")}
	uc := NewHandlePaymentWebhookUseCase(repo, nil, credit, nil, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_RECEIVED", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err == nil {
		t.Fatal("expected credit fail error")
	}

}

func TestHandleAsaasWebhook_TopUp_Overdue(t *testing.T) {
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeTopUp},
	}}
	uc := NewHandlePaymentWebhookUseCase(repo, nil, nil, nil, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_OVERDUE", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.statusUpdates) != 1 || repo.statusUpdates[0] != invoice.StatusOverdue {
		t.Fatalf("expected OVERDUE status update, got %v", repo.statusUpdates)
	}
}

func TestHandleAsaasWebhook_TopUp_Deleted(t *testing.T) {
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeTopUp},
	}}
	uc := NewHandlePaymentWebhookUseCase(repo, nil, nil, nil, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_DELETED", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.statusUpdates[0] != invoice.StatusCancelled {
		t.Fatalf("expected CANCELLED, got %v", repo.statusUpdates[0])
	}
}

func TestHandleAsaasWebhook_TopUp_UnknownEvent(t *testing.T) {
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeTopUp},
	}}
	uc := NewHandlePaymentWebhookUseCase(repo, nil, nil, nil, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_CREATED", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleAsaasWebhook_Subscription_MarkPaidError(t *testing.T) {
	planID := "plan-1"
	repo := &errWebhookInvoiceRepo{
		webhookInvoiceRepo: webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
			"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeSubscription, PlanDefinitionID: &planID, AmountUSD: 1000},
		}},
		markPaidErr: errors.New("mark fail"),
	}
	uc := NewHandlePaymentWebhookUseCase(repo, nil, nil, nil, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_RECEIVED", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestHandleAsaasWebhook_Subscription_AlreadyPaid(t *testing.T) {
	planID := "plan-1"
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeSubscription, PlanDefinitionID: &planID, AmountUSD: 1000, Status: invoice.StatusPaid},
	}}
	subscribe := &webhookSubscribeWorkspacePlan{}
	uc := NewHandlePaymentWebhookUseCase(repo, subscribe, nil, nil, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_RECEIVED", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if subscribe.calls != 0 {
		t.Fatal("should not subscribe when already paid")
	}
}

func TestHandleAsaasWebhook_Subscription_NilPlanDefinitionID(t *testing.T) {
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeSubscription, PlanDefinitionID: nil, AmountUSD: 1000},
	}}
	uc := NewHandlePaymentWebhookUseCase(repo, nil, nil, nil, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_RECEIVED", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err == nil {
		t.Fatal("expected error for nil plan definition")
	}
}

func TestHandleAsaasWebhook_Subscription_EmptyPlanDefinitionID(t *testing.T) {
	empty := " "
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeSubscription, PlanDefinitionID: &empty, AmountUSD: 1000},
	}}
	uc := NewHandlePaymentWebhookUseCase(repo, nil, nil, nil, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_RECEIVED", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err == nil {
		t.Fatal("expected error for whitespace plan definition")
	}
}

func TestHandleAsaasWebhook_Subscription_NilActivator(t *testing.T) {
	planID := "plan-1"
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeSubscription, PlanDefinitionID: &planID, AmountUSD: 1000},
	}}
	uc := NewHandlePaymentWebhookUseCase(repo, nil, nil, nil, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_RECEIVED", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err == nil {
		t.Fatal("expected error for nil activator")
	}
}

func TestHandleAsaasWebhook_Subscription_ActivateError(t *testing.T) {
	planID := "plan-1"
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeSubscription, PlanDefinitionID: &planID, AmountUSD: 1000, WorkspaceID: "ws-1"},
	}}
	subscribe := &webhookSubscribeWorkspacePlan{err: errors.New("activate fail")}
	uc := NewHandlePaymentWebhookUseCase(repo, subscribe, nil, nil, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_RECEIVED", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err == nil {
		t.Fatal("expected activate error")
	}
}

func TestHandleAsaasWebhook_Subscription_CreditBalanceError(t *testing.T) {
	planID := "plan-1"
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeSubscription, PlanDefinitionID: &planID, AmountUSD: 1000, WorkspaceID: "ws-1"},
	}}
	subscribe := &webhookSubscribeWorkspacePlan{}
	credit := &errWebhookCreditBalance{err: errors.New("credit fail")}
	uc := NewHandlePaymentWebhookUseCase(repo, subscribe, credit, nil, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_RECEIVED", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err == nil {
		t.Fatal("expected credit fail error")
	}
}

func TestHandleAsaasWebhook_Subscription_ZeroAmount_NoCredit(t *testing.T) {
	planID := "plan-1"
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeSubscription, PlanDefinitionID: &planID, AmountUSD: 0, WorkspaceID: "ws-1"},
	}}
	subscribe := &webhookSubscribeWorkspacePlan{}
	credit := &webhookCreditBalance{}
	uc := NewHandlePaymentWebhookUseCase(repo, subscribe, credit, nil, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_RECEIVED", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(credit.calls) != 0 {
		t.Fatal("should not credit when amount is 0")
	}
}

func TestHandleAsaasWebhook_Subscription_Overdue(t *testing.T) {
	planID := "plan-1"
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeSubscription, PlanDefinitionID: &planID},
	}}
	uc := NewHandlePaymentWebhookUseCase(repo, nil, nil, nil, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_OVERDUE", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.statusUpdates[0] != invoice.StatusOverdue {
		t.Fatalf("expected OVERDUE, got %v", repo.statusUpdates[0])
	}
}

func TestHandleAsaasWebhook_Subscription_Deleted(t *testing.T) {
	planID := "plan-1"
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeSubscription, PlanDefinitionID: &planID},
	}}
	uc := NewHandlePaymentWebhookUseCase(repo, nil, nil, nil, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_DELETED", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.statusUpdates[0] != invoice.StatusCancelled {
		t.Fatalf("expected CANCELLED, got %v", repo.statusUpdates[0])
	}
}

func TestHandleAsaasWebhook_TopUp_Overdue_UpdateStatusError(t *testing.T) {
	repo := &errWebhookInvoiceRepo{
		webhookInvoiceRepo: webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
			"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeTopUp},
		}},
		updateErr: errors.New("update fail"),
	}
	uc := NewHandlePaymentWebhookUseCase(repo, nil, nil, nil, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_OVERDUE", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err == nil || err.Error() != "update fail" {
		t.Fatalf("expected update fail, got %v", err)
	}
}

func TestHandleAsaasWebhook_TopUp_Deleted_UpdateStatusError(t *testing.T) {
	repo := &errWebhookInvoiceRepo{
		webhookInvoiceRepo: webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
			"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeTopUp},
		}},
		updateErr: errors.New("update fail"),
	}
	uc := NewHandlePaymentWebhookUseCase(repo, nil, nil, nil, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_DELETED", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err == nil || err.Error() != "update fail" {
		t.Fatalf("expected update fail, got %v", err)
	}
}

func TestHandleAsaasWebhook_Subscription_Overdue_UpdateStatusError(t *testing.T) {
	planID := "plan-1"
	repo := &errWebhookInvoiceRepo{
		webhookInvoiceRepo: webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
			"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeSubscription, PlanDefinitionID: &planID},
		}},
		updateErr: errors.New("update fail"),
	}
	uc := NewHandlePaymentWebhookUseCase(repo, nil, nil, nil, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_OVERDUE", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err == nil || err.Error() != "update fail" {
		t.Fatalf("expected update fail, got %v", err)
	}
}

func TestHandleAsaasWebhook_Subscription_Deleted_UpdateStatusError(t *testing.T) {
	planID := "plan-1"
	repo := &errWebhookInvoiceRepo{
		webhookInvoiceRepo: webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
			"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeSubscription, PlanDefinitionID: &planID},
		}},
		updateErr: errors.New("update fail"),
	}
	uc := NewHandlePaymentWebhookUseCase(repo, nil, nil, nil, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_DELETED", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err == nil || err.Error() != "update fail" {
		t.Fatalf("expected update fail, got %v", err)
	}
}

type trackingDebitBalance struct {
	calls []balance.DebitBalanceInput
	err   error
}

func (u *trackingDebitBalance) Execute(input balance.DebitBalanceInput) (*balance.Transaction, error) {
	u.calls = append(u.calls, input)
	if u.err != nil {
		return nil, u.err
	}
	return &balance.Transaction{}, nil
}

type failingInvoiceLookup struct{ webhookInvoiceRepo }

func (failingInvoiceLookup) GetByExternalID(payment.Provider, string) (*invoice.Invoice, error) {
	return nil, errors.New("connection reset")
}

func TestHandleWebhook_AFailedInvoiceLookupIsRetriedNotDropped(t *testing.T) {
	uc := NewHandlePaymentWebhookUseCase(&failingInvoiceLookup{}, nil, &webhookCreditBalance{}, &webhookDebitBalance{}, nil)

	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_RECEIVED", Payment: payment.AsaasWebhookPayment{ID: "pay_1"}})

	if err == nil {
		t.Fatal("a paid charge whose invoice could not be read was acknowledged; it must fail so the provider retries")
	}
}
