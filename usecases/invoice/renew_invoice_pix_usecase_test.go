package invoice_usecase

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	billing "vozko/domain/billing"
	"vozko/domain/invoice"
	"vozko/domain/payment"
	"vozko/domain/user"
)

type renewalStore struct {
	stubInvoiceRepo
	mu          sync.Mutex
	invoices    map[string]*invoice.Invoice
	superseded  map[invoice.SupersededCharge]string
	leases      map[string]time.Time
	completeErr error
	getErr      error
}

func newRenewalStore(invoices ...*invoice.Invoice) *renewalStore {
	s := &renewalStore{
		invoices:   map[string]*invoice.Invoice{},
		superseded: map[invoice.SupersededCharge]string{},
		leases:     map[string]time.Time{},
	}
	for _, inv := range invoices {
		s.invoices[inv.ID] = inv
	}
	return s
}

func (s *renewalStore) GetByID(id string) (*invoice.Invoice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getErr != nil {
		return nil, s.getErr
	}
	inv, ok := s.invoices[id]
	if !ok {
		return nil, invoice.ErrInvoiceNotFound
	}
	copied := *inv
	return &copied, nil
}

func (s *renewalStore) BeginRenewal(id string, previous invoice.SupersededCharge, leaseUntil, now time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inv := s.invoices[id]
	if inv == nil || inv.ExternalID != previous.ExternalID || !inv.AwaitingPayment() {
		return false, nil
	}
	if held, ok := s.leases[id]; ok && !held.Before(now) {
		return false, nil
	}
	s.leases[id] = leaseUntil
	s.superseded[previous] = id
	return true, nil
}

func (s *renewalStore) CompleteRenewal(id, previousExternalID string, charge invoice.ChargeAttachment) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.completeErr != nil {
		return false, s.completeErr
	}
	inv := s.invoices[id]
	if inv == nil || inv.ExternalID != previousExternalID || !inv.AwaitingPayment() {
		return false, nil
	}
	inv.ExternalID = charge.ExternalID
	inv.PixQrCode = charge.PixQrCode
	inv.PixCopy = charge.PixCopy
	inv.InvoiceUrl = charge.InvoiceUrl
	inv.ChargeExpiresAt = charge.ExpiresAt
	delete(s.leases, id)
	return true, nil
}

func (s *renewalStore) AbortRenewal(id string, previous invoice.SupersededCharge, previousStillPayable bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.leases, id)
	if previousStillPayable {
		delete(s.superseded, previous)
	}
	return nil
}

func (s *renewalStore) leaseHeld(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, held := s.leases[id]
	return held
}

func (s *renewalStore) isSuperseded(provider payment.Provider, externalID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.superseded[invoice.SupersededCharge{Provider: provider, ExternalID: externalID}]
	return ok
}

type renewGateway struct {
	mu        sync.Mutex
	provider  payment.Provider
	steps     []string
	requests  []payment.ChargeRequest
	cancelled []string
	cancelErr error
	getCharge *payment.Charge
	getErr    error
	createErr error
	expiresAt *time.Time
	onCreate  func()
	nextID    int
}

func (g *renewGateway) Provider() payment.Provider {
	if g.provider == "" {
		return payment.ProviderAsaas
	}
	return g.provider
}
func (g *renewGateway) Capabilities() payment.GatewayCapabilities {
	return payment.GatewayCapabilities{Split: true}
}

func (g *renewGateway) CreateCharge(_ context.Context, req payment.ChargeRequest) (*payment.Charge, error) {
	g.mu.Lock()
	g.steps = append(g.steps, "create")
	g.requests = append(g.requests, req)
	g.nextID++
	id := "pay_new_" + string(rune('0'+g.nextID))
	onCreate := g.onCreate
	g.mu.Unlock()
	if g.createErr != nil {
		return nil, g.createErr
	}
	if onCreate != nil {
		onCreate()
	}
	return &payment.Charge{
		ID: id, Provider: g.Provider(), Status: payment.StatusPending, Method: req.Method, Amount: req.Amount,
		ExternalReference: req.ExternalReference, PixQRCodeBase64: "new-qr", PixCopyPaste: "000201new", ExpiresAt: g.expiresAt,
	}, nil
}

func (g *renewGateway) GetCharge(_ context.Context, id string) (*payment.Charge, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.steps = append(g.steps, "get:"+id)
	if g.getErr != nil {
		return nil, g.getErr
	}
	if g.getCharge != nil {
		return g.getCharge, nil
	}
	return &payment.Charge{ID: id, Status: payment.StatusPending}, nil
}

func (g *renewGateway) RefundCharge(context.Context, string, float64, string) error {
	return errors.New("renewals never refund")
}

func (g *renewGateway) CancelCharge(_ context.Context, id string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.steps = append(g.steps, "cancel:"+id)
	if g.cancelErr != nil && id == "pay_old" {
		return g.cancelErr
	}
	g.cancelled = append(g.cancelled, id)
	return nil
}

func (g *renewGateway) createCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.requests)
}

type recordingAlerter struct {
	mu       sync.Mutex
	subjects []string
}

func (a *recordingAlerter) Alert(_ context.Context, subject, _ string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.subjects = append(a.subjects, subject)
	return nil
}

var renewNow = time.Date(2026, time.October, 25, 9, 0, 0, 0, billing.LocationBRT())

func overdueMonthlyInvoice() *invoice.Invoice {
	due := time.Date(2026, time.October, 23, 0, 0, 0, 0, billing.LocationBRT())
	expired := due
	qr, code := "old-qr", "000201old"
	return &invoice.Invoice{
		ID: "inv-1", WorkspaceID: "ws-1", UserID: "user-1", Purpose: invoice.PurposeMonthlyBilling,
		AmountBRL: 210, Status: invoice.StatusOverdue, BillingType: "PIX", Provider: payment.ProviderAsaas,
		ExternalID: "pay_old", PixQrCode: &qr, PixCopy: &code, ChargeExpiresAt: &expired,
		Description: "Cobrança mensal", DueDate: &due,
	}
}

func newRenewUC(store *renewalStore, gw *renewGateway, alerts *recordingAlerter) invoice.RenewPixUseCase {
	return newRenewInvoicePix(
		store, store,
		&stubUserRepo{user: &user.User{ID: "user-1", Username: "Tester", Email: "t@example.com", CPF: "12345678900"}},
		gw, nil, alerts,
		func() time.Time { return renewNow },
	)
}

func renewInput() invoice.RenewPixInput {
	return invoice.RenewPixInput{WorkspaceID: "ws-1", InvoiceID: "inv-1"}
}

func TestARenewalIssuesANewChargeForTheSameInvoiceAndAmount(t *testing.T) {
	store := newRenewalStore(overdueMonthlyInvoice())
	gw := &renewGateway{}

	got, err := newRenewUC(store, gw, &recordingAlerter{}).Execute(context.Background(), renewInput())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(gw.steps) != 2 || gw.steps[0] != "cancel:pay_old" || gw.steps[1] != "create" {
		t.Fatalf("the previous charge must be cancelled before the new one is created, got %v", gw.steps)
	}
	req := gw.requests[0]
	cutoff, _ := invoice.PayableUntil(invoice.PurposeMonthlyBilling, got.DueDate)
	if req.Amount != 210 || req.Method != payment.MethodPix || req.ExternalReference != invoice.ExternalReference("inv-1") {
		t.Fatalf("the new charge must bill the same invoice and amount by PIX, got %+v", req)
	}
	if req.IdempotencyKey != invoice.RenewalIdempotencyKey("inv-1", "pay_old") {
		t.Fatalf("idempotency key = %q", req.IdempotencyKey)
	}
	if !req.DueDate.Equal(cutoff) || !req.ExpiresAt.Equal(cutoff) {
		t.Fatalf("the new charge must run until the cutoff %s, got due %s expiry %s", cutoff, req.DueDate, req.ExpiresAt)
	}
	if req.Customer.Document != "12345678900" || req.Description == "" {
		t.Fatalf("the new charge must bill the invoice customer, got %+v", req.Customer)
	}

	if got.ExternalID != "pay_new_1" || got.PixCopy == nil || *got.PixCopy != "000201new" {
		t.Fatalf("the invoice must carry the new charge, got %+v", got)
	}
	if view := got.PaymentAt(renewNow); view.State != invoice.PaymentOpen {
		t.Fatalf("a renewed invoice must be payable, got %s", view.State)
	}
	if !store.isSuperseded(payment.ProviderAsaas, "pay_old") {
		t.Fatal("the replaced charge must be recorded as superseded")
	}
	if store.leaseHeld("inv-1") {
		t.Fatal("the renewal lease must be released")
	}
}

func TestTheProviderExpiryWinsOverTheRequestedOne(t *testing.T) {
	providerExpiry := time.Date(2026, time.October, 26, 12, 0, 0, 0, billing.LocationBRT())
	store := newRenewalStore(overdueMonthlyInvoice())
	gw := &renewGateway{expiresAt: &providerExpiry}

	got, err := newRenewUC(store, gw, &recordingAlerter{}).Execute(context.Background(), renewInput())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got.ChargeExpiresAt == nil || !got.ChargeExpiresAt.Equal(providerExpiry) {
		t.Fatalf("chargeExpiresAt = %v, want the provider's %s", got.ChargeExpiresAt, providerExpiry)
	}
}

func TestALiveChargeIsReturnedWithoutCallingTheProvider(t *testing.T) {
	inv := overdueMonthlyInvoice()
	inv.Status = invoice.StatusPending
	live := time.Date(2026, time.October, 27, 23, 0, 0, 0, billing.LocationBRT())
	inv.ChargeExpiresAt = &live
	store := newRenewalStore(inv)
	gw := &renewGateway{}

	got, err := newRenewUC(store, gw, &recordingAlerter{}).Execute(context.Background(), renewInput())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got.ExternalID != "pay_old" || len(gw.steps) != 0 {
		t.Fatalf("a live charge must be returned as is, got %s with provider calls %v", got.ExternalID, gw.steps)
	}
}

func TestASettledOrClosedInvoiceIsNeverRenewed(t *testing.T) {
	for _, status := range []invoice.Status{invoice.StatusPaid, invoice.StatusRefunded, invoice.StatusReversed, invoice.StatusCancelled, invoice.StatusExpired} {
		inv := overdueMonthlyInvoice()
		inv.Status = status
		gw := &renewGateway{}

		_, err := newRenewUC(newRenewalStore(inv), gw, &recordingAlerter{}).Execute(context.Background(), renewInput())
		if !errors.Is(err, invoice.ErrNotPayable) {
			t.Errorf("%s: err = %v, want ErrNotPayable", status, err)
		}
		if len(gw.steps) != 0 {
			t.Errorf("%s: no provider call may happen, got %v", status, gw.steps)
		}
	}
}

func TestAnInvoiceAfterTheCutoffIsNotRenewed(t *testing.T) {
	store := newRenewalStore(overdueMonthlyInvoice())
	gw := &renewGateway{}
	uc := newRenewInvoicePix(store, store, &stubUserRepo{user: &user.User{CPF: "1"}}, gw, nil, &recordingAlerter{},
		func() time.Time { return time.Date(2026, time.October, 28, 0, 0, 1, 0, billing.LocationBRT()) })

	if _, err := uc.Execute(context.Background(), renewInput()); !errors.Is(err, invoice.ErrNotPayable) {
		t.Fatalf("err = %v, want ErrNotPayable", err)
	}
	if len(gw.steps) != 0 {
		t.Fatalf("no provider call may happen, got %v", gw.steps)
	}
}

func TestAnInvoiceOfAnotherWorkspaceIsNotFound(t *testing.T) {
	gw := &renewGateway{}
	_, err := newRenewUC(newRenewalStore(overdueMonthlyInvoice()), gw, &recordingAlerter{}).
		Execute(context.Background(), invoice.RenewPixInput{WorkspaceID: "ws-2", InvoiceID: "inv-1"})
	if !errors.Is(err, invoice.ErrInvoiceNotFound) {
		t.Fatalf("err = %v, want ErrInvoiceNotFound", err)
	}
	if len(gw.steps) != 0 {
		t.Fatalf("no provider call may happen, got %v", gw.steps)
	}
}

func TestARenewalInProgressIsReported(t *testing.T) {
	store := newRenewalStore(overdueMonthlyInvoice())
	store.leases["inv-1"] = renewNow.Add(time.Minute)
	gw := &renewGateway{}

	_, err := newRenewUC(store, gw, &recordingAlerter{}).Execute(context.Background(), renewInput())
	if !errors.Is(err, invoice.ErrPixRenewInProgress) {
		t.Fatalf("err = %v, want ErrPixRenewInProgress", err)
	}
	if len(gw.steps) != 0 {
		t.Fatalf("no provider call may happen while another renewal runs, got %v", gw.steps)
	}
}

func TestACustomerWithoutADocumentIsRefusedBeforeAnyProviderCall(t *testing.T) {
	store := newRenewalStore(overdueMonthlyInvoice())
	gw := &renewGateway{}
	uc := newRenewInvoicePix(store, store, &stubUserRepo{user: &user.User{ID: "user-1"}}, gw, nil, &recordingAlerter{},
		func() time.Time { return renewNow })

	if _, err := uc.Execute(context.Background(), renewInput()); !errors.Is(err, invoice.ErrCustomerDocumentRequired) {
		t.Fatalf("err = %v, want ErrCustomerDocumentRequired", err)
	}
	if len(gw.steps) != 0 || store.leaseHeld("inv-1") {
		t.Fatalf("nothing may be touched, got provider calls %v", gw.steps)
	}
}

func TestAChargeOfAnotherProviderIsNotRenewed(t *testing.T) {
	inv := overdueMonthlyInvoice()
	inv.Provider = payment.ProviderMercadoPago
	gw := &renewGateway{provider: payment.ProviderAsaas}

	_, err := newRenewUC(newRenewalStore(inv), gw, &recordingAlerter{}).Execute(context.Background(), renewInput())
	if !errors.Is(err, invoice.ErrPixUnavailable) {
		t.Fatalf("err = %v, want ErrPixUnavailable", err)
	}
	if len(gw.steps) != 0 {
		t.Fatalf("the active provider must never touch another provider's charge, got %v", gw.steps)
	}
}

func TestACancelFailureKeepsThePreviousChargeCurrent(t *testing.T) {
	store := newRenewalStore(overdueMonthlyInvoice())
	gw := &renewGateway{cancelErr: errors.New("provider down"), getCharge: &payment.Charge{ID: "pay_old", Status: payment.StatusPending}}

	_, err := newRenewUC(store, gw, &recordingAlerter{}).Execute(context.Background(), renewInput())
	if !errors.Is(err, invoice.ErrPixUnavailable) {
		t.Fatalf("err = %v, want ErrPixUnavailable", err)
	}
	if gw.createCount() != 0 {
		t.Fatal("no new charge may exist while the previous one is still payable")
	}
	stored, _ := store.GetByID("inv-1")
	if stored.ExternalID != "pay_old" || store.isSuperseded(payment.ProviderAsaas, "pay_old") || store.leaseHeld("inv-1") {
		t.Fatalf("the invoice must stay on its previous charge, unmarked and unlocked, got %+v", stored)
	}
}

func TestAPaymentReceivedBeforeTheCancelIsNotReissued(t *testing.T) {
	store := newRenewalStore(overdueMonthlyInvoice())
	gw := &renewGateway{cancelErr: errors.New("cannot delete a received payment"), getCharge: &payment.Charge{ID: "pay_old", Status: payment.StatusReceived}}

	_, err := newRenewUC(store, gw, &recordingAlerter{}).Execute(context.Background(), renewInput())
	if !errors.Is(err, invoice.ErrAlreadyPaid) {
		t.Fatalf("err = %v, want ErrAlreadyPaid", err)
	}
	if gw.createCount() != 0 || store.isSuperseded(payment.ProviderAsaas, "pay_old") {
		t.Fatal("a paid charge stays the invoice's charge so its webhook settles it")
	}
}

func TestAnUnreadablePreviousChargeFailsClosed(t *testing.T) {
	store := newRenewalStore(overdueMonthlyInvoice())
	gw := &renewGateway{cancelErr: errors.New("timeout"), getErr: errors.New("timeout")}

	if _, err := newRenewUC(store, gw, &recordingAlerter{}).Execute(context.Background(), renewInput()); !errors.Is(err, invoice.ErrPixUnavailable) {
		t.Fatalf("err = %v, want ErrPixUnavailable", err)
	}
	if gw.createCount() != 0 {
		t.Fatal("no new charge may be created when the previous one cannot be checked")
	}
}

func TestAPreviousChargeAlreadyGoneIsReplaced(t *testing.T) {
	for name, gw := range map[string]*renewGateway{
		"cancelled at the provider": {cancelErr: errors.New("already deleted"), getCharge: &payment.Charge{ID: "pay_old", Status: payment.StatusCancelled}},
		"unknown to the provider":   {cancelErr: payment.ErrChargeNotFound},
		"read as not found":         {cancelErr: errors.New("gone"), getErr: payment.ErrChargeNotFound},
	} {
		store := newRenewalStore(overdueMonthlyInvoice())
		got, err := newRenewUC(store, gw, &recordingAlerter{}).Execute(context.Background(), renewInput())
		if err != nil {
			t.Fatalf("%s: Execute: %v", name, err)
		}
		if got.ExternalID == "pay_old" || gw.createCount() != 1 {
			t.Fatalf("%s: a charge that can no longer be paid must be replaced", name)
		}
	}
}

func TestACreateFailureLeavesTheInvoiceRenewable(t *testing.T) {
	store := newRenewalStore(overdueMonthlyInvoice())
	gw := &renewGateway{createErr: errors.New("provider down")}

	_, err := newRenewUC(store, gw, &recordingAlerter{}).Execute(context.Background(), renewInput())
	if !errors.Is(err, invoice.ErrPixUnavailable) {
		t.Fatalf("err = %v, want ErrPixUnavailable", err)
	}
	stored, _ := store.GetByID("inv-1")
	if stored.ExternalID != "pay_old" || stored.Status != invoice.StatusOverdue {
		t.Fatalf("the invoice must keep its state, got %+v", stored)
	}
	if !store.isSuperseded(payment.ProviderAsaas, "pay_old") {
		t.Fatal("the cancelled charge stays superseded so its late webhooks are ignored")
	}
	if store.leaseHeld("inv-1") {
		t.Fatal("the lease must be released so the customer can retry")
	}
	if view := stored.PaymentAt(renewNow); view.State != invoice.PaymentRenewable || view.PixCopy != nil {
		t.Fatalf("the invoice must stay renewable without exposing the dead code, got %+v", view)
	}
}

func TestARetryAfterACreateFailureReusesTheSameProviderKey(t *testing.T) {
	store := newRenewalStore(overdueMonthlyInvoice())
	gw := &renewGateway{createErr: errors.New("timeout")}
	uc := newRenewUC(store, gw, &recordingAlerter{})
	_, _ = uc.Execute(context.Background(), renewInput())

	gw.createErr = nil
	gw.cancelErr = errors.New("already deleted")
	gw.getCharge = &payment.Charge{ID: "pay_old", Status: payment.StatusCancelled}
	if _, err := uc.Execute(context.Background(), renewInput()); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if len(gw.requests) != 2 || gw.requests[0].IdempotencyKey != gw.requests[1].IdempotencyKey {
		t.Fatalf("a retry must reuse the provider key so a charge created before a timeout is returned, got %+v", gw.requests)
	}
}

func (s *renewalStore) settle(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.invoices[id].Status = invoice.StatusPaid
}

func TestAChargeIssuedWhileThePreviousOneWasPaidIsCancelled(t *testing.T) {
	store := newRenewalStore(overdueMonthlyInvoice())
	gw := &renewGateway{onCreate: func() { store.settle("inv-1") }}

	_, err := newRenewUC(store, gw, &recordingAlerter{}).Execute(context.Background(), renewInput())
	if !errors.Is(err, invoice.ErrAlreadyPaid) {
		t.Fatalf("err = %v, want ErrAlreadyPaid", err)
	}
	if len(gw.cancelled) != 2 || gw.cancelled[1] != "pay_new_1" {
		t.Fatalf("the orphan charge must be cancelled, got %v", gw.cancelled)
	}
	if store.leaseHeld("inv-1") {
		t.Fatal("the lease must be released")
	}
}

func TestAnOrphanChargeThatCannotBeCancelledRaisesAnAlert(t *testing.T) {
	store := newRenewalStore(overdueMonthlyInvoice())
	alerts := &recordingAlerter{}
	gw := &renewGateway{onCreate: func() { store.settle("inv-1") }}

	uc := newRenewInvoicePix(store, store,
		&stubUserRepo{user: &user.User{ID: "user-1", CPF: "12345678900"}},
		&orphanGateway{renewGateway: gw}, nil, alerts, func() time.Time { return renewNow })
	if _, err := uc.Execute(context.Background(), renewInput()); !errors.Is(err, invoice.ErrAlreadyPaid) {
		t.Fatalf("err = %v, want ErrAlreadyPaid", err)
	}
	if len(alerts.subjects) != 1 {
		t.Fatalf("an open orphan charge must reach the operators, got %v", alerts.subjects)
	}
}

type orphanGateway struct {
	*renewGateway
}

func (g *orphanGateway) CancelCharge(ctx context.Context, id string) error {
	if id != "pay_old" {
		return errors.New("provider down")
	}
	return g.renewGateway.CancelCharge(ctx, id)
}

func (s *renewalStore) attachOther(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inv := s.invoices[id]
	other, code := "pay_other", "000201other"
	live := renewNow.Add(time.Hour)
	inv.ExternalID = other
	inv.PixCopy = &code
	inv.ChargeExpiresAt = &live
}

func (s *renewalStore) breakStorage(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.completeErr = err
	s.getErr = err
}

func TestAChargeIssuedAfterAnotherRenewalWonIsCancelledAndTheWinnerReturned(t *testing.T) {
	store := newRenewalStore(overdueMonthlyInvoice())
	gw := &renewGateway{onCreate: func() { store.attachOther("inv-1") }}

	got, err := newRenewUC(store, gw, &recordingAlerter{}).Execute(context.Background(), renewInput())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got.ExternalID != "pay_other" {
		t.Fatalf("the charge that won must be returned, got %s", got.ExternalID)
	}
	if len(gw.cancelled) != 2 || gw.cancelled[1] != "pay_new_1" {
		t.Fatalf("only one payable charge may exist, so ours must be cancelled, got %v", gw.cancelled)
	}
}

func TestAnUnconfirmedAttachLeavesTheNewChargeAloneAndAlerts(t *testing.T) {
	store := newRenewalStore(overdueMonthlyInvoice())
	alerts := &recordingAlerter{}
	gw := &renewGateway{onCreate: func() { store.breakStorage(errors.New("connection reset")) }}

	_, err := newRenewUC(store, gw, alerts).Execute(context.Background(), renewInput())
	if !errors.Is(err, invoice.ErrPixUnavailable) {
		t.Fatalf("err = %v, want ErrPixUnavailable", err)
	}
	if len(gw.cancelled) != 1 {
		t.Fatalf("a charge that may be attached must never be cancelled blind, got %v", gw.cancelled)
	}
	if len(alerts.subjects) != 1 {
		t.Fatalf("the operators must check the unconfirmed charge, got %v", alerts.subjects)
	}
	if !store.leaseHeld("inv-1") {
		t.Fatal("the lease must stay until it expires so no second renewal races the unknown state")
	}
}
