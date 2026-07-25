package payment_usecase

import (
	"errors"
	"testing"

	"vozko/domain/balance"
	"vozko/domain/invoice"
	"vozko/domain/order"
	"vozko/domain/payment"
	"vozko/domain/shared"
	"vozko/domain/ticket"
	"vozko/domain/user"
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

type webhookInvoiceRepo struct {
	byExternal    map[string]*invoice.Invoice
	markPaidCalls []string
	statusUpdates []invoice.Status
}

func (r *webhookInvoiceRepo) Create(*invoice.Invoice) error            { return nil }
func (r *webhookInvoiceRepo) GetByID(string) (*invoice.Invoice, error) { return nil, nil }
func (r *webhookInvoiceRepo) GetByExternalID(externalID string) (*invoice.Invoice, error) {
	return r.byExternal[externalID], nil
}
func (r *webhookInvoiceRepo) GetByIdempotencyKey(string) (*invoice.Invoice, error) {
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
		if inv.Status == invoice.StatusPaid {
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
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, subscribe, credit, &webhookDebitBalance{}, nil)

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
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, subscribe, credit, &webhookDebitBalance{}, nil)

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

type mockPaymentRepo struct {
	payments map[string]*payment.Payment
	statuses map[string]payment.Status
}

func newMockPaymentRepo() *mockPaymentRepo {
	return &mockPaymentRepo{
		payments: make(map[string]*payment.Payment),
		statuses: make(map[string]payment.Status),
	}
}

func (r *mockPaymentRepo) CreatePayment(p *payment.Payment) error { r.payments[p.ID] = p; return nil }
func (r *mockPaymentRepo) GetPaymentByID(id string) (*payment.Payment, error) {
	p, ok := r.payments[id]
	if !ok {
		return nil, nil
	}
	return p, nil
}
func (r *mockPaymentRepo) UpdatePaymentStatus(id string, status payment.Status) error {
	r.statuses[id] = status
	return nil
}
func (r *mockPaymentRepo) ListPaymentsByUser(string) ([]payment.Payment, error) { return nil, nil }
func (r *mockPaymentRepo) RefundPayment(string, int64) error                    { return nil }
func (r *mockPaymentRepo) CancelPayment(string) error                           { return nil }

type errMockPaymentRepo struct {
	mockPaymentRepo
	getErr    error
	updateErr error
}

func (r *errMockPaymentRepo) GetPaymentByID(id string) (*payment.Payment, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	return r.mockPaymentRepo.GetPaymentByID(id)
}

func (r *errMockPaymentRepo) UpdatePaymentStatus(id string, status payment.Status) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	return r.mockPaymentRepo.UpdatePaymentStatus(id, status)
}

type mockOrderRepo struct {
	orders  map[string]*order.Order
	updated map[string]order.Status
}

func newMockOrderRepo() *mockOrderRepo {
	return &mockOrderRepo{
		orders:  make(map[string]*order.Order),
		updated: make(map[string]order.Status),
	}
}

func (r *mockOrderRepo) Create(*order.Order) error { return nil }
func (r *mockOrderRepo) CreateWithInventoryReservation(*order.Order, []order.InventoryUpdate) error {
	return nil
}
func (r *mockOrderRepo) Update(*order.Order) error { return nil }
func (r *mockOrderRepo) UpdateStatus(id string, status order.Status) error {
	r.updated[id] = status
	return nil
}
func (r *mockOrderRepo) GetByID(string, string) (*order.Order, error) { return nil, nil }
func (r *mockOrderRepo) GetByIDForSystem(id string) (*order.Order, error) {
	o, ok := r.orders[id]
	if !ok {
		return nil, nil
	}
	return o, nil
}
func (r *mockOrderRepo) ListByWorkspace(order.ListOrdersInput) (*shared.PaginatedResult[*order.Order], error) {
	return nil, nil
}
func (r *mockOrderRepo) GetExpiredOrders() ([]*order.Order, error)            { return nil, nil }
func (r *mockOrderRepo) CancelOrder(string) error                             { return nil }
func (r *mockOrderRepo) CancelExpiredOrdersAndReturnInventory([]string) error { return nil }

type errMockOrderRepo struct {
	mockOrderRepo
	updateStatusErr  error
	getByIDForSysErr error
}

func (r *errMockOrderRepo) UpdateStatus(id string, status order.Status) error {
	if r.updateStatusErr != nil {
		return r.updateStatusErr
	}
	return r.mockOrderRepo.UpdateStatus(id, status)
}

func (r *errMockOrderRepo) GetByIDForSystem(id string) (*order.Order, error) {
	if r.getByIDForSysErr != nil {
		return nil, r.getByIDForSysErr
	}
	return r.mockOrderRepo.GetByIDForSystem(id)
}

type mockEmailService struct {
	sent []string
	err  error
}

func (s *mockEmailService) SendEmail(to, subject, body string) error { return s.err }
func (s *mockEmailService) SendTemplate(to, subject, template string, data map[string]interface{}) error {
	s.sent = append(s.sent, to)
	return s.err
}

type mockUserRepo struct {
	users map[string]*user.User
}

func newMockUserRepo() *mockUserRepo {
	return &mockUserRepo{users: make(map[string]*user.User)}
}

func (r *mockUserRepo) WithTx(interface{}) user.UserRepository { return r }
func (r *mockUserRepo) Create(*user.User) error                { return nil }
func (r *mockUserRepo) Update(string, *user.User) error        { return nil }
func (r *mockUserRepo) Delete(string) error                    { return nil }
func (r *mockUserRepo) FindByID(id string) (*user.User, error) {
	u, ok := r.users[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return u, nil
}
func (r *mockUserRepo) FindByIDs([]string) ([]*user.User, error)  { return nil, nil }
func (r *mockUserRepo) FindByEmail(string) (*user.User, error)    { return nil, nil }
func (r *mockUserRepo) FindByDocument(string) (*user.User, error) { return nil, nil }
func (r *mockUserRepo) List(user.ListUsersInput) (*shared.PaginatedResult[*user.User], error) {
	return nil, nil
}
func (r *mockUserRepo) CountByRole(user.Role) (int64, error)      { return 0, nil }
func (r *mockUserRepo) GetUserRole(string) (string, error)        { return "", nil }
func (r *mockUserRepo) GetTokenVersion(string) (int, error)       { return 0, nil }
func (r *mockUserRepo) IncrementTokenVersion(string) (int, error) { return 0, nil }

type mockCreateTicket struct {
	calls int
	err   error
}

func (m *mockCreateTicket) Execute(orderID, userID string) (*ticket.Ticket, error) {
	m.calls++
	if m.err != nil {
		return nil, m.err
	}
	return &ticket.Ticket{}, nil
}

type errWebhookInvoiceRepo struct {
	webhookInvoiceRepo
	getByExtErr error
	markPaidErr error
	updateErr   error
}

func (r *errWebhookInvoiceRepo) GetByExternalID(externalID string) (*invoice.Invoice, error) {
	if r.getByExtErr != nil {
		return nil, r.getByExtErr
	}
	return r.webhookInvoiceRepo.GetByExternalID(externalID)
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
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{}}, nil, nil, nil, nil)
	if err := uc.Execute(nil); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestHandleAsaasWebhook_EmptyPaymentID(t *testing.T) {
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{}}, nil, nil, nil, nil)
	if err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_RECEIVED", Payment: payment.AsaasWebhookPayment{ID: ""}}); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestHandleAsaasWebhook_NonInvoice_PaymentCreated(t *testing.T) {
	payRepo := newMockPaymentRepo()
	orderID := "order-1"
	payRepo.payments["pay-ext"] = &payment.Payment{
		ID:      "pay-ext",
		OrderID: &orderID,
		Status:  payment.StatusPending,
		Amount:  100,
	}
	ordRepo := newMockOrderRepo()
	ordRepo.orders["order-1"] = &order.Order{ID: "order-1", UserID: "user-1", CustomerName: "Test"}
	invRepo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{}}
	uc := NewHandleAsaasWebhookUseCase(payRepo, ordRepo, nil, nil, nil, invRepo, nil, nil, nil, nil)

	err := uc.Execute(&payment.AsaasWebhookEvent{
		Event:   "PAYMENT_CREATED",
		Payment: payment.AsaasWebhookPayment{ID: "ext-pay", ExternalReference: "pay-ext"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if payRepo.statuses["pay-ext"] != payment.StatusPending {
		t.Fatalf("expected PENDING status, got %v", payRepo.statuses["pay-ext"])
	}
	if ordRepo.updated["order-1"] != order.StatusPending {
		t.Fatalf("expected order pending, got %v", ordRepo.updated["order-1"])
	}
}

func TestHandleAsaasWebhook_NonInvoice_PaymentReceived_SendsEmail(t *testing.T) {
	payRepo := newMockPaymentRepo()
	orderID := "order-1"
	payRepo.payments["pay-ext"] = &payment.Payment{
		ID:          "pay-ext",
		OrderID:     &orderID,
		Status:      payment.StatusPending,
		Amount:      200,
		BillingType: payment.BillingTypePix,
	}
	ordRepo := newMockOrderRepo()
	ordRepo.orders["order-1"] = &order.Order{ID: "order-1", UserID: "user-1", CustomerName: "Buyer"}
	userRepo := newMockUserRepo()
	userRepo.users["user-1"] = &user.User{ID: "user-1", Email: "buyer@test.com"}
	emailSvc := &mockEmailService{}
	invRepo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{}}
	createTicketMock := &mockCreateTicket{}

	uc := NewHandleAsaasWebhookUseCase(payRepo, ordRepo, emailSvc, userRepo, createTicketMock, invRepo, nil, nil, nil, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{
		Event:   "PAYMENT_RECEIVED",
		Payment: payment.AsaasWebhookPayment{ID: "ext-pay", ExternalReference: "pay-ext", Value: 200, BillingType: "PIX"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if payRepo.statuses["pay-ext"] != payment.StatusReceived {
		t.Fatalf("expected RECEIVED status")
	}
	if ordRepo.updated["order-1"] != order.StatusPaid {
		t.Fatalf("expected order paid")
	}
	if createTicketMock.calls != 1 {
		t.Fatalf("expected ticket creation, got %d calls", createTicketMock.calls)
	}
}

func TestHandleAsaasWebhook_NonInvoice_PaymentNotFound(t *testing.T) {
	payRepo := newMockPaymentRepo()
	invRepo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{}}
	uc := NewHandleAsaasWebhookUseCase(payRepo, nil, nil, nil, nil, invRepo, nil, nil, nil, nil)

	err := uc.Execute(&payment.AsaasWebhookEvent{
		Event:   "PAYMENT_RECEIVED",
		Payment: payment.AsaasWebhookPayment{ID: "ext-pay", ExternalReference: "pay-not-found"},
	})
	if !errors.Is(err, payment.ErrPaymentNotFound) {
		t.Fatalf("expected ErrPaymentNotFound, got %v", err)
	}
}

func TestHandleAsaasWebhook_NonInvoice_GetPaymentError(t *testing.T) {
	payRepo := &errMockPaymentRepo{
		mockPaymentRepo: *newMockPaymentRepo(),
		getErr:          errors.New("db error"),
	}
	invRepo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{}}
	uc := NewHandleAsaasWebhookUseCase(payRepo, nil, nil, nil, nil, invRepo, nil, nil, nil, nil)

	err := uc.Execute(&payment.AsaasWebhookEvent{
		Event:   "PAYMENT_RECEIVED",
		Payment: payment.AsaasWebhookPayment{ID: "ext-pay", ExternalReference: "pay-1"},
	})
	if err == nil || err.Error() != "db error" {
		t.Fatalf("expected db error, got %v", err)
	}
}

func TestHandleAsaasWebhook_NonInvoice_UpdateStatusError(t *testing.T) {
	payRepo := &errMockPaymentRepo{
		mockPaymentRepo: *newMockPaymentRepo(),
		updateErr:       errors.New("update fail"),
	}
	payRepo.payments["pay-1"] = &payment.Payment{ID: "pay-1", Status: payment.StatusPending}
	invRepo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{}}
	uc := NewHandleAsaasWebhookUseCase(payRepo, nil, nil, nil, nil, invRepo, nil, nil, nil, nil)

	err := uc.Execute(&payment.AsaasWebhookEvent{
		Event:   "PAYMENT_CREATED",
		Payment: payment.AsaasWebhookPayment{ID: "ext-pay", ExternalReference: "pay-1"},
	})
	if err == nil || err.Error() != "update fail" {
		t.Fatalf("expected update fail, got %v", err)
	}
}

func TestHandleAsaasWebhook_NonInvoice_OrderUpdateError(t *testing.T) {
	payRepo := newMockPaymentRepo()
	orderID := "order-1"
	payRepo.payments["pay-1"] = &payment.Payment{ID: "pay-1", OrderID: &orderID, Status: payment.StatusPending}
	ordRepo := &errMockOrderRepo{
		mockOrderRepo:   *newMockOrderRepo(),
		updateStatusErr: errors.New("order update fail"),
	}
	invRepo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{}}
	uc := NewHandleAsaasWebhookUseCase(payRepo, ordRepo, nil, nil, nil, invRepo, nil, nil, nil, nil)

	err := uc.Execute(&payment.AsaasWebhookEvent{
		Event:   "PAYMENT_RECEIVED",
		Payment: payment.AsaasWebhookPayment{ID: "ext-pay", ExternalReference: "pay-1"},
	})
	if err == nil || err.Error() != "order update fail" {
		t.Fatalf("expected order update fail, got %v", err)
	}
}

func TestHandleAsaasWebhook_NonInvoice_TicketAlreadyExists(t *testing.T) {
	payRepo := newMockPaymentRepo()
	orderID := "order-1"
	payRepo.payments["pay-1"] = &payment.Payment{ID: "pay-1", OrderID: &orderID, Status: payment.StatusPending}
	ordRepo := newMockOrderRepo()
	ordRepo.orders["order-1"] = &order.Order{ID: "order-1", UserID: "u-1"}
	invRepo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{}}
	ticketMock := &mockCreateTicket{err: ticket.ErrTicketAlreadyExists}
	uc := NewHandleAsaasWebhookUseCase(payRepo, ordRepo, nil, nil, ticketMock, invRepo, nil, nil, nil, nil)

	err := uc.Execute(&payment.AsaasWebhookEvent{
		Event:   "PAYMENT_RECEIVED",
		Payment: payment.AsaasWebhookPayment{ID: "ext-pay", ExternalReference: "pay-1"},
	})
	if err != nil {
		t.Fatalf("ErrTicketAlreadyExists should be tolerated, got %v", err)
	}
}

func TestHandleAsaasWebhook_NonInvoice_TicketCreateError(t *testing.T) {
	payRepo := newMockPaymentRepo()
	orderID := "order-1"
	payRepo.payments["pay-1"] = &payment.Payment{ID: "pay-1", OrderID: &orderID, Status: payment.StatusPending}
	ordRepo := newMockOrderRepo()
	ordRepo.orders["order-1"] = &order.Order{ID: "order-1", UserID: "u-1"}
	invRepo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{}}
	ticketMock := &mockCreateTicket{err: errors.New("ticket fail")}
	uc := NewHandleAsaasWebhookUseCase(payRepo, ordRepo, nil, nil, ticketMock, invRepo, nil, nil, nil, nil)

	err := uc.Execute(&payment.AsaasWebhookEvent{
		Event:   "PAYMENT_RECEIVED",
		Payment: payment.AsaasWebhookPayment{ID: "ext-pay", ExternalReference: "pay-1"},
	})
	if err == nil || err.Error() != "ticket fail" {
		t.Fatalf("expected ticket fail, got %v", err)
	}
}

func TestHandleAsaasWebhook_NonInvoice_NoExternalReference(t *testing.T) {
	invRepo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{}}
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, invRepo, nil, nil, nil, nil)

	err := uc.Execute(&payment.AsaasWebhookEvent{
		Event:   "PAYMENT_RECEIVED",
		Payment: payment.AsaasWebhookPayment{ID: "ext-pay"},
	})
	if err != nil {
		t.Fatalf("expected nil for no external ref, got %v", err)
	}
}

func TestHandleAsaasWebhook_NonInvoice_UnknownEvent(t *testing.T) {
	payRepo := newMockPaymentRepo()
	payRepo.payments["pay-1"] = &payment.Payment{ID: "pay-1", Status: payment.StatusPending}
	invRepo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{}}
	uc := NewHandleAsaasWebhookUseCase(payRepo, nil, nil, nil, nil, invRepo, nil, nil, nil, nil)

	err := uc.Execute(&payment.AsaasWebhookEvent{
		Event:   "SOMETHING_UNKNOWN",
		Payment: payment.AsaasWebhookPayment{ID: "ext-pay", ExternalReference: "pay-1"},
	})
	if err != nil {
		t.Fatalf("unknown event should not error, got %v", err)
	}
}

func TestHandleAsaasWebhook_NonInvoice_ReceivedInCash(t *testing.T) {
	payRepo := newMockPaymentRepo()
	orderID := "order-1"
	payRepo.payments["pay-1"] = &payment.Payment{
		ID: "pay-1", OrderID: &orderID, Status: payment.StatusPending,
		Amount: 300, BillingType: payment.BillingTypeBoleto,
	}
	ordRepo := newMockOrderRepo()
	ordRepo.orders["order-1"] = &order.Order{ID: "order-1", UserID: "u-1", CustomerName: "User"}
	invRepo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{}}

	uc := NewHandleAsaasWebhookUseCase(payRepo, ordRepo, nil, nil, nil, invRepo, nil, nil, nil, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{
		Event:   "PAYMENT_RECEIVED_IN_CASH",
		Payment: payment.AsaasWebhookPayment{ID: "ext-pay", ExternalReference: "pay-1"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if payRepo.statuses["pay-1"] != payment.StatusReceivedInCash {
		t.Fatalf("expected RECEIVED_IN_CASH, got %v", payRepo.statuses["pay-1"])
	}
}

func TestHandleAsaasWebhook_NonInvoice_Refund(t *testing.T) {
	payRepo := newMockPaymentRepo()
	payRepo.payments["pay-1"] = &payment.Payment{ID: "pay-1", Status: payment.StatusReceived}
	invRepo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{}}
	uc := NewHandleAsaasWebhookUseCase(payRepo, nil, nil, nil, nil, invRepo, nil, nil, nil, nil)

	err := uc.Execute(&payment.AsaasWebhookEvent{
		Event:   "PAYMENT_REFUNDED",
		Payment: payment.AsaasWebhookPayment{ID: "ext-pay", ExternalReference: "pay-1"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if payRepo.statuses["pay-1"] != payment.StatusRefunded {
		t.Fatalf("expected REFUNDED, got %v", payRepo.statuses["pay-1"])
	}
}

func TestHandleAsaasWebhook_NonInvoice_GetOrderForEmailError(t *testing.T) {
	payRepo := newMockPaymentRepo()
	orderID := "order-1"
	payRepo.payments["pay-1"] = &payment.Payment{
		ID: "pay-1", OrderID: &orderID, Status: payment.StatusPending,
	}
	ordRepo := &errMockOrderRepo{
		mockOrderRepo: *newMockOrderRepo(),
	}

	invRepo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{}}
	uc := NewHandleAsaasWebhookUseCase(payRepo, ordRepo, nil, nil, nil, invRepo, nil, nil, nil, nil)

	err := uc.Execute(&payment.AsaasWebhookEvent{
		Event:   "PAYMENT_RECEIVED",
		Payment: payment.AsaasWebhookPayment{ID: "ext-pay", ExternalReference: "pay-1", Value: 100, BillingType: "PIX"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleAsaasWebhook_TopUp_AmountUSDZero(t *testing.T) {
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeTopUp, AmountUSD: 0},
	}}
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, nil, nil, nil)
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
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, credit, nil, nil)
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
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, nil, nil, nil)
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
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, credit, nil, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_RECEIVED", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err == nil {
		t.Fatal("expected credit fail error")
	}

}

func TestHandleAsaasWebhook_TopUp_Overdue(t *testing.T) {
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeTopUp},
	}}
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, nil, nil, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_OVERDUE", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.statusUpdates) != 1 || repo.statusUpdates[0] != invoice.StatusOverdue {
		t.Fatalf("expected OVERDUE status update, got %v", repo.statusUpdates)
	}
}

func TestHandleAsaasWebhook_TopUp_Refunded_PaidInvoice(t *testing.T) {
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeTopUp, AmountUSD: 5000, WorkspaceID: "ws-1", Status: invoice.StatusPaid, AmountBRL: 30, ExchangeRate: 6},
	}}
	debit := &webhookDebitBalance{}
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, nil, debit, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_REFUNDED", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.statusUpdates) != 1 || repo.statusUpdates[0] != invoice.StatusRefunded {
		t.Fatalf("expected REFUNDED status, got %v", repo.statusUpdates)
	}
	if len(debit.calls) != 1 {
		t.Fatalf("expected one chargeback debit, got %d", len(debit.calls))
	}
	got := debit.calls[0]
	if !got.IsRefund {
		t.Fatalf("expected chargeback debit to carry IsRefund=true")
	}
	if got.ServiceType != balance.ServiceTopUp {
		t.Fatalf("expected service_type top_up, got %s", got.ServiceType)
	}
	if got.ExchangeRateMicros != 6_000_000 {
		t.Fatalf("expected exchange rate 6_000_000 from invoice, got %d", got.ExchangeRateMicros)
	}
}

func TestHandleAsaasWebhook_Subscription_Refunded_DebitsWithRefundFlag(t *testing.T) {
	planID := "plan-1"
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeSubscription, PlanDefinitionID: &planID, AmountUSD: 8000, WorkspaceID: "ws-1", Status: invoice.StatusPaid, AmountBRL: 48, ExchangeRate: 6},
	}}
	debit := &webhookDebitBalance{}
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, nil, debit, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_REFUNDED", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(debit.calls) != 1 {
		t.Fatalf("expected one subscription chargeback debit, got %d", len(debit.calls))
	}
	got := debit.calls[0]
	if !got.IsRefund || got.ServiceType != balance.ServiceTopUp || got.ExchangeRateMicros != 6_000_000 {
		t.Fatalf("unexpected subscription chargeback debit: %+v", got)
	}
}

func TestHandleAsaasWebhook_TopUp_Refunded_DebitError(t *testing.T) {
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeTopUp, AmountUSD: 5000, WorkspaceID: "ws-1", Status: invoice.StatusPaid},
	}}
	debit := &errWebhookDebitBalance{err: errors.New("debit fail")}
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, nil, debit, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_REFUNDED", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleAsaasWebhook_TopUp_Deleted(t *testing.T) {
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeTopUp},
	}}
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, nil, nil, nil)
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
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, nil, nil, nil)
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
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, nil, nil, nil)
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
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, subscribe, nil, nil, nil)
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
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, nil, nil, nil)
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
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, nil, nil, nil)
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
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, nil, nil, nil)
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
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, subscribe, nil, nil, nil)
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
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, subscribe, credit, nil, nil)
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
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, subscribe, credit, nil, nil)
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
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, nil, nil, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_OVERDUE", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.statusUpdates[0] != invoice.StatusOverdue {
		t.Fatalf("expected OVERDUE, got %v", repo.statusUpdates[0])
	}
}

func TestHandleAsaasWebhook_Subscription_Refunded(t *testing.T) {
	planID := "plan-1"
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeSubscription, PlanDefinitionID: &planID},
	}}
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, nil, nil, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_REFUNDED", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.statusUpdates[0] != invoice.StatusRefunded {
		t.Fatalf("expected REFUNDED, got %v", repo.statusUpdates[0])
	}
}

func TestHandleAsaasWebhook_Subscription_Deleted(t *testing.T) {
	planID := "plan-1"
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeSubscription, PlanDefinitionID: &planID},
	}}
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, nil, nil, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_DELETED", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.statusUpdates[0] != invoice.StatusCancelled {
		t.Fatalf("expected CANCELLED, got %v", repo.statusUpdates[0])
	}
}

func TestHandleAsaasWebhook_InvoiceLookupError(t *testing.T) {
	repo := &errWebhookInvoiceRepo{
		webhookInvoiceRepo: webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
			"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeTopUp},
		}},
		getByExtErr: errors.New("lookup fail"),
	}
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, nil, nil, nil)

	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_RECEIVED", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err != nil {
		t.Fatalf("expected nil (falls through to non-invoice), got %v", err)
	}
}

func TestMapAsaasEventToStatuses(t *testing.T) {
	tests := []struct {
		event     string
		payStatus *payment.Status
		ordStatus *order.Status
	}{
		{"PAYMENT_CREATED", ptr(payment.StatusPending), ptrO(order.StatusPending)},
		{"PAYMENT_CONFIRMED", ptr(payment.StatusPending), ptrO(order.StatusProcessing)},
		{"PAYMENT_APPROVED_BY_RISK_ANALYSIS", ptr(payment.StatusPending), ptrO(order.StatusProcessing)},
		{"PAYMENT_AUTHORIZED", ptr(payment.StatusPending), ptrO(order.StatusProcessing)},
		{"PAYMENT_RECEIVED", ptr(payment.StatusReceived), ptrO(order.StatusPaid)},
		{"PAYMENT_RECEIVED_IN_CASH_UNDONE", ptr(payment.StatusPending), ptrO(order.StatusProcessing)},
		{"PAYMENT_OVERDUE", ptr(payment.StatusOverdue), ptrO(order.StatusProcessing)},
		{"PAYMENT_REFUNDED", ptr(payment.StatusRefunded), ptrO(order.StatusRefunded)},
		{"PAYMENT_PARTIALLY_REFUNDED", ptr(payment.StatusRefunded), ptrO(order.StatusRefunded)},
		{"PAYMENT_REFUND_IN_PROGRESS", ptr(payment.StatusRefundInProgress), nil},
		{"PAYMENT_RECEIVED_IN_CASH", ptr(payment.StatusReceivedInCash), ptrO(order.StatusPaid)},
		{"PAYMENT_DELETED", ptr(payment.StatusCancelled), ptrO(order.StatusCancelled)},
		{"UNKNOWN_EVENT", nil, nil},
	}

	for _, tt := range tests {
		ps, os := mapAsaasEventToStatuses(tt.event)
		if tt.payStatus == nil && ps != nil {
			t.Errorf("%s: expected nil payStatus, got %v", tt.event, *ps)
		}
		if tt.payStatus != nil && (ps == nil || *ps != *tt.payStatus) {
			t.Errorf("%s: expected payStatus %v, got %v", tt.event, *tt.payStatus, ps)
		}
		if tt.ordStatus == nil && os != nil {
			t.Errorf("%s: expected nil ordStatus, got %v", tt.event, *os)
		}
		if tt.ordStatus != nil && (os == nil || *os != *tt.ordStatus) {
			t.Errorf("%s: expected ordStatus %v, got %v", tt.event, *tt.ordStatus, os)
		}
	}
}

func ptr(s payment.Status) *payment.Status { return &s }
func ptrO(s order.Status) *order.Status    { return &s }

func TestFormatPaymentMethod(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", "PIX"},
		{"PIX", "PIX"},
		{"pix", "PIX"},
		{"BOLETO", "Boleto"},
		{"boleto", "Boleto"},
		{"CREDIT_CARD", "Cartao de Credito"},
		{"credit_card", "Cartao de Credito"},
		{"OTHER", "OTHER"},
	}
	for _, tt := range tests {
		result := formatPaymentMethod(tt.input)
		if result != tt.expected {
			t.Errorf("formatPaymentMethod(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestSendPaymentConfirmationEmail_NilOrder(t *testing.T) {
	uc := &handleAsaasWebhookUseCase{emailService: &mockEmailService{}}
	uc.sendPaymentConfirmationEmail(nil, 100, "PIX")

}

func TestSendPaymentConfirmationEmail_NilEmailService(t *testing.T) {
	uc := &handleAsaasWebhookUseCase{}
	uc.sendPaymentConfirmationEmail(&order.Order{}, 100, "PIX")

}

func TestSendPaymentConfirmationEmail_NoEmail(t *testing.T) {
	userRepo := newMockUserRepo()
	uc := &handleAsaasWebhookUseCase{emailService: &mockEmailService{}, userRepo: userRepo}
	uc.sendPaymentConfirmationEmail(&order.Order{UserID: "u-1"}, 100, "PIX")
}

func TestSendPaymentConfirmationEmail_Success(t *testing.T) {
	userRepo := newMockUserRepo()
	userRepo.users["u-1"] = &user.User{ID: "u-1", Email: "test@test.com"}
	emailSvc := &mockEmailService{}
	uc := &handleAsaasWebhookUseCase{emailService: emailSvc, userRepo: userRepo}
	uc.sendPaymentConfirmationEmail(&order.Order{ID: "ord-1", UserID: "u-1", CustomerName: "Test"}, 100, "PIX")
	if len(emailSvc.sent) != 1 || emailSvc.sent[0] != "test@test.com" {
		t.Fatalf("expected email sent to test@test.com, got %v", emailSvc.sent)
	}
}

func TestSendPaymentConfirmationEmail_FallbackToCustomerName(t *testing.T) {
	userRepo := newMockUserRepo()
	emailSvc := &mockEmailService{}
	uc := &handleAsaasWebhookUseCase{emailService: emailSvc, userRepo: userRepo}
	uc.sendPaymentConfirmationEmail(&order.Order{ID: "ord-1", UserID: "u-1", CustomerName: "cust@email.com"}, 100, "BOLETO")
	if len(emailSvc.sent) != 1 || emailSvc.sent[0] != "cust@email.com" {
		t.Fatalf("expected fallback to customer name, got %v", emailSvc.sent)
	}
}

func TestSendPaymentConfirmationEmail_EmailError(t *testing.T) {
	userRepo := newMockUserRepo()
	userRepo.users["u-1"] = &user.User{ID: "u-1", Email: "test@test.com"}
	emailSvc := &mockEmailService{err: errors.New("smtp fail")}
	uc := &handleAsaasWebhookUseCase{emailService: emailSvc, userRepo: userRepo}
	uc.sendPaymentConfirmationEmail(&order.Order{ID: "ord-1", UserID: "u-1", CustomerName: "Test"}, 100, "PIX")

}

func TestGetUserEmail_EmptyUserID(t *testing.T) {
	uc := &handleAsaasWebhookUseCase{}
	if email := uc.getUserEmail(""); email != "" {
		t.Fatalf("expected empty, got %s", email)
	}
}

func TestGetUserEmail_NilUserRepo(t *testing.T) {
	uc := &handleAsaasWebhookUseCase{}
	if email := uc.getUserEmail("u-1"); email != "" {
		t.Fatalf("expected empty, got %s", email)
	}
}

type countingInvoiceRepo struct {
	webhookInvoiceRepo
	getByExtCalls int
	getByExtErr   error
	updateErr     error
}

func (r *countingInvoiceRepo) GetByExternalID(externalID string) (*invoice.Invoice, error) {
	r.getByExtCalls++
	if r.getByExtCalls >= 2 && r.getByExtErr != nil {
		return nil, r.getByExtErr
	}
	return r.webhookInvoiceRepo.GetByExternalID(externalID)
}

func (r *countingInvoiceRepo) UpdateStatus(id string, status invoice.Status) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	return r.webhookInvoiceRepo.UpdateStatus(id, status)
}

func TestHandleAsaasWebhook_HandleInvoicePayment_LookupError(t *testing.T) {
	repo := &countingInvoiceRepo{
		webhookInvoiceRepo: webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
			"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeTopUp},
		}},
		getByExtErr: errors.New("lookup fail"),
	}
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, nil, nil, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_RECEIVED", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err == nil || err.Error() != "invoice lookup failed: lookup fail" {
		t.Fatalf("expected lookup fail, got %v", err)
	}
}

type nilSecondCallInvoiceRepo struct {
	webhookInvoiceRepo
	getByExtCalls int
}

func (r *nilSecondCallInvoiceRepo) GetByExternalID(externalID string) (*invoice.Invoice, error) {
	r.getByExtCalls++
	if r.getByExtCalls >= 2 {
		return nil, nil
	}
	return r.webhookInvoiceRepo.GetByExternalID(externalID)
}

func TestHandleAsaasWebhook_HandleInvoicePayment_NilInvoice(t *testing.T) {
	repo := &nilSecondCallInvoiceRepo{
		webhookInvoiceRepo: webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
			"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeTopUp},
		}},
	}
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, nil, nil, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_RECEIVED", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestHandleAsaasWebhook_TopUp_Overdue_UpdateStatusError(t *testing.T) {
	repo := &errWebhookInvoiceRepo{
		webhookInvoiceRepo: webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
			"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeTopUp},
		}},
		updateErr: errors.New("update fail"),
	}
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, nil, nil, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_OVERDUE", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err == nil || err.Error() != "update fail" {
		t.Fatalf("expected update fail, got %v", err)
	}
}

func TestHandleAsaasWebhook_TopUp_Refunded_UpdateStatusError(t *testing.T) {
	repo := &errWebhookInvoiceRepo{
		webhookInvoiceRepo: webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
			"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeTopUp, Status: invoice.StatusPaid, AmountUSD: 1000, WorkspaceID: "ws-1"},
		}},
		updateErr: errors.New("update fail"),
	}
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, nil, &webhookDebitBalance{}, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_REFUNDED", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
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
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, nil, nil, nil)
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
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, nil, nil, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_OVERDUE", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err == nil || err.Error() != "update fail" {
		t.Fatalf("expected update fail, got %v", err)
	}
}

func TestHandleAsaasWebhook_Subscription_Refunded_UpdateStatusError(t *testing.T) {
	planID := "plan-1"
	repo := &errWebhookInvoiceRepo{
		webhookInvoiceRepo: webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
			"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeSubscription, PlanDefinitionID: &planID},
		}},
		updateErr: errors.New("update fail"),
	}
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, nil, nil, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_REFUNDED", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
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
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, nil, nil, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_DELETED", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err == nil || err.Error() != "update fail" {
		t.Fatalf("expected update fail, got %v", err)
	}
}

func TestHandleAsaasWebhook_NonInvoice_GetByIDForSystem_TicketError(t *testing.T) {
	payRepo := newMockPaymentRepo()
	orderID := "order-1"
	payRepo.payments["pay-1"] = &payment.Payment{ID: "pay-1", OrderID: &orderID, Status: payment.StatusPending}
	ordRepo := &errMockOrderRepo{
		mockOrderRepo:    *newMockOrderRepo(),
		getByIDForSysErr: errors.New("order fetch fail"),
	}
	invRepo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{}}
	uc := NewHandleAsaasWebhookUseCase(payRepo, ordRepo, nil, nil, &mockCreateTicket{}, invRepo, nil, nil, nil, nil)

	err := uc.Execute(&payment.AsaasWebhookEvent{
		Event:   "PAYMENT_RECEIVED",
		Payment: payment.AsaasWebhookPayment{ID: "ext", ExternalReference: "pay-1", Value: 100, BillingType: "PIX"},
	})
	if err == nil || err.Error() != "order fetch fail" {
		t.Fatalf("expected order fetch fail, got %v", err)
	}
}

func TestHandleAsaasWebhook_NonInvoice_GetByIDForSystem_EmailError(t *testing.T) {
	payRepo := newMockPaymentRepo()
	orderID := "order-1"
	payRepo.payments["pay-1"] = &payment.Payment{ID: "pay-1", OrderID: &orderID, Status: payment.StatusPending}

	ordRepo := &callCountingOrderRepo{
		mockOrderRepo: *newMockOrderRepo(),
		failOnCall:    1,
		err:           errors.New("email order fetch fail"),
	}
	invRepo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{}}
	uc := NewHandleAsaasWebhookUseCase(payRepo, ordRepo, nil, nil, nil, invRepo, nil, nil, nil, nil)

	err := uc.Execute(&payment.AsaasWebhookEvent{
		Event:   "PAYMENT_RECEIVED",
		Payment: payment.AsaasWebhookPayment{ID: "ext", ExternalReference: "pay-1", Value: 100, BillingType: "PIX"},
	})
	if err == nil || err.Error() != "email order fetch fail" {
		t.Fatalf("expected email order fetch fail, got %v", err)
	}
}

type callCountingOrderRepo struct {
	mockOrderRepo
	callCount  int
	failOnCall int
	err        error
}

func (r *callCountingOrderRepo) GetByIDForSystem(id string) (*order.Order, error) {
	r.callCount++
	if r.callCount >= r.failOnCall {
		return nil, r.err
	}
	return r.mockOrderRepo.GetByIDForSystem(id)
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

func TestHandleAsaasWebhook_Subscription_Refunded_PaidInvoice_DebitsBalance(t *testing.T) {
	planID := "plan-1"
	debit := &trackingDebitBalance{}
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeSubscription, PlanDefinitionID: &planID, AmountUSD: 5000, AmountBRL: 30.0, WorkspaceID: "ws-1", Status: invoice.StatusPaid},
	}}
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, nil, debit, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_REFUNDED", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(debit.calls) != 1 {
		t.Fatalf("expected one debit call for refund reversal, got %d", len(debit.calls))
	}
	if debit.calls[0].WorkspaceID != "ws-1" || debit.calls[0].Amount != 5000 {
		t.Fatalf("unexpected debit input: %+v", debit.calls[0])
	}
	if repo.statusUpdates[0] != invoice.StatusRefunded {
		t.Fatalf("expected REFUNDED status, got %v", repo.statusUpdates[0])
	}
}

func TestHandleAsaasWebhook_Subscription_Refunded_UnpaidInvoice_NoDebit(t *testing.T) {
	planID := "plan-1"
	debit := &trackingDebitBalance{}
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeSubscription, PlanDefinitionID: &planID, AmountUSD: 5000, WorkspaceID: "ws-1", Status: invoice.StatusPending},
	}}
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, nil, debit, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_REFUNDED", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(debit.calls) != 0 {
		t.Fatalf("should not debit for unpaid invoice, got %d calls", len(debit.calls))
	}
}

func TestHandleAsaasWebhook_Subscription_Refunded_ZeroAmount_NoDebit(t *testing.T) {
	planID := "plan-1"
	debit := &trackingDebitBalance{}
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeSubscription, PlanDefinitionID: &planID, AmountUSD: 0, WorkspaceID: "ws-1", Status: invoice.StatusPaid},
	}}
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, nil, debit, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_REFUNDED", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(debit.calls) != 0 {
		t.Fatalf("should not debit for zero amount, got %d calls", len(debit.calls))
	}
}

func TestHandleAsaasWebhook_Subscription_Refunded_DebitError_StillUpdatesStatus(t *testing.T) {
	planID := "plan-1"
	debit := &errWebhookDebitBalance{err: errors.New("debit fail")}
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeSubscription, PlanDefinitionID: &planID, AmountUSD: 5000, WorkspaceID: "ws-1", Status: invoice.StatusPaid},
	}}
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, nil, debit, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_REFUNDED", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.statusUpdates) != 1 || repo.statusUpdates[0] != invoice.StatusRefunded {
		t.Fatalf("expected REFUNDED status despite debit failure, got %v", repo.statusUpdates)
	}
}

func TestHandleAsaasWebhook_Subscription_PartialRefund_PaidInvoice_DebitsBalance(t *testing.T) {
	planID := "plan-1"
	debit := &trackingDebitBalance{}
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeSubscription, PlanDefinitionID: &planID, AmountUSD: 3000, AmountBRL: 18.0, WorkspaceID: "ws-1", Status: invoice.StatusPaid},
	}}
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, nil, debit, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_PARTIALLY_REFUNDED", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(debit.calls) != 1 {
		t.Fatalf("expected one debit call for partial refund, got %d", len(debit.calls))
	}
}

func TestHandleAsaasWebhook_Subscription_Refunded_UpdateStatusError_WithDebit(t *testing.T) {
	planID := "plan-1"
	repo := &errWebhookInvoiceRepo{
		webhookInvoiceRepo: webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
			"pay-1": {ID: "inv-1", ExternalID: "pay-1", Purpose: invoice.PurposeSubscription, PlanDefinitionID: &planID, AmountUSD: 5000, WorkspaceID: "ws-1", Status: invoice.StatusPaid},
		}},
		updateErr: errors.New("update fail"),
	}
	debit := &trackingDebitBalance{}
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, nil, debit, nil)
	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_REFUNDED", Payment: payment.AsaasWebhookPayment{ID: "pay-1"}})
	if err == nil || err.Error() != "update fail" {
		t.Fatalf("expected update fail, got %v", err)
	}

	if len(debit.calls) != 1 {
		t.Fatalf("expected debit to be attempted before status update, got %d calls", len(debit.calls))
	}
}
