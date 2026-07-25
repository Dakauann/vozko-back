package invoice_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/affiliate"
	"vozko/domain/invoice"
	"vozko/domain/shared"
	"vozko/domain/user"
	workspace_plan "vozko/domain/workspace/workspace_plan"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
	"vozko/infra/asaas"
)

type stubInvoiceRepo struct {
	created *invoice.Invoice
}

func (r *stubInvoiceRepo) Create(inv *invoice.Invoice) error {
	r.created = inv
	return nil
}

func (r *stubInvoiceRepo) GetByID(string) (*invoice.Invoice, error)         { return nil, nil }
func (r *stubInvoiceRepo) GetByExternalID(string) (*invoice.Invoice, error) { return nil, nil }
func (r *stubInvoiceRepo) GetByIdempotencyKey(key string) (*invoice.Invoice, error) {
	if key != "" && r.created != nil && r.created.IdempotencyKey == key {
		return r.created, nil
	}
	return nil, nil
}
func (r *stubInvoiceRepo) ListUnpaidByPurpose(invoice.Purpose, string, int) ([]invoice.Invoice, error) {
	return nil, nil
}
func (r *stubInvoiceRepo) UpdateStatus(string, invoice.Status) error { return nil }
func (r *stubInvoiceRepo) MarkPaid(string, int64) (bool, error)      { return false, nil }
func (r *stubInvoiceRepo) ListByWorkspace(string, int, int) ([]invoice.Invoice, int64, error) {
	return nil, 0, nil
}

type stubUserRepo struct {
	user *user.User
}

func (r *stubUserRepo) WithTx(interface{}) user.UserRepository    { return r }
func (r *stubUserRepo) Create(*user.User) error                   { return nil }
func (r *stubUserRepo) Update(string, *user.User) error           { return nil }
func (r *stubUserRepo) Delete(string) error                       { return nil }
func (r *stubUserRepo) FindByID(string) (*user.User, error)       { return r.user, nil }
func (r *stubUserRepo) FindByIDs([]string) ([]*user.User, error)  { return nil, nil }
func (r *stubUserRepo) FindByEmail(string) (*user.User, error)    { return nil, nil }
func (r *stubUserRepo) FindByDocument(string) (*user.User, error) { return nil, nil }
func (r *stubUserRepo) List(user.ListUsersInput) (*shared.PaginatedResult[*user.User], error) {
	return nil, nil
}
func (r *stubUserRepo) CountByRole(user.Role) (int64, error)      { return 0, nil }
func (r *stubUserRepo) GetUserRole(string) (string, error)        { return "", nil }
func (r *stubUserRepo) GetTokenVersion(string) (int, error)       { return 0, nil }
func (r *stubUserRepo) IncrementTokenVersion(string) (int, error) { return 0, nil }

type stubAsaasService struct {
	createCalls int
}

func (s *stubAsaasService) GetOrCreateCustomer(string, string) (*asaas.AsaasCustomer, error) {
	return &asaas.AsaasCustomer{ID: "cust-1"}, nil
}

func (s *stubAsaasService) CreatePayment(string, string, *asaas.AsaasPayment) (*asaas.AsaasPayment, error) {
	s.createCalls++
	return &asaas.AsaasPayment{ID: "pay-1", InvoiceUrl: "https://asaas.test/invoices/pay-1"}, nil
}

func (s *stubAsaasService) GetPaymentQrCode(string) (string, string, error) {
	return "qr-code", "pix-copy", nil
}

func (s *stubAsaasService) GetPayment(string) (*asaas.AsaasPayment, error) { return nil, nil }
func (s *stubAsaasService) RefundPayment(string, int64, string) error      { return nil }
func (s *stubAsaasService) DeletePayment(string) error                     { return nil }
func (s *stubAsaasService) ValidateWalletID(string, string, string) error  { return nil }

type stubPricingRepo struct{}

func (r *stubPricingRepo) ListDefaultPricingItems() ([]workspace_pricing.PricingItem, error) {
	return []workspace_pricing.PricingItem{{
		Category:    workspace_pricing.CategoryExchangeRate,
		Service:     "usd_to_brl",
		Metric:      "spot",
		PriceMicros: 6000000,
	}}, nil
}

func (r *stubPricingRepo) GetPricingItem(string) (*workspace_pricing.PricingItem, error) {
	return nil, nil
}
func (r *stubPricingRepo) UpsertPricingItem(*workspace_pricing.PricingItem) error      { return nil }
func (r *stubPricingRepo) DeletePricingItem(string) error                              { return nil }
func (r *stubPricingRepo) SeedDefaults([]workspace_pricing.PricingItem) error          { return nil }
func (r *stubPricingRepo) CreateAuditEntry(*workspace_pricing.PricingAuditEntry) error { return nil }
func (r *stubPricingRepo) ListAuditEntries(*string, int, int) ([]workspace_pricing.PricingAuditEntry, error) {
	return nil, nil
}

type stubCurrentSubscriptionChecker struct {
	err error
}

func (c *stubCurrentSubscriptionChecker) Execute(workspaceID string) (*workspace_plan.WorkspaceSubscription, error) {
	if c.err != nil {
		return nil, c.err
	}
	return &workspace_plan.WorkspaceSubscription{WorkspaceID: workspaceID, PlanName: "Starter", MaxCallChannels: 3, Status: workspace_plan.SubscriptionStatusActive}, nil
}

func TestCreateInvoiceUseCase_TopUpRequiresCurrentSubscription(t *testing.T) {
	repo := &stubInvoiceRepo{}
	asaasService := &stubAsaasService{}
	uc := NewCreateInvoiceUseCase(
		repo,
		&stubUserRepo{user: &user.User{ID: "user-1", Username: "Tester", CPF: "12345678900"}},
		asaasService,
		&stubPricingRepo{},
		&stubCurrentSubscriptionChecker{err: workspace_plan.ErrSubscriptionNotCurrent},
		nil,
		nil,
	)

	_, err := uc.Execute(invoice.CreateInvoiceInput{
		WorkspaceID: "ws-1",
		UserID:      "user-1",
		AmountBRL:   50,
		BillingType: "PIX",
		Description: "Recarga",
	})
	if !errors.Is(err, invoice.ErrActiveSubscriptionRequired) {
		t.Fatalf("expected ErrActiveSubscriptionRequired, got %v", err)
	}
	if asaasService.createCalls != 0 {
		t.Fatalf("expected no Asaas payment creation when subscription gate fails, got %d", asaasService.createCalls)
	}
	if repo.created != nil {
		t.Fatal("expected invoice not to be persisted when subscription gate fails")
	}
}

func TestCreateInvoiceUseCase_SubscriptionInvoicePersistsPurposeMetadata(t *testing.T) {
	repo := &stubInvoiceRepo{}
	asaasService := &stubAsaasService{}
	uc := NewCreateInvoiceUseCase(
		repo,
		&stubUserRepo{user: &user.User{ID: "user-1", Username: "Tester", CPF: "12345678900"}},
		asaasService,
		&stubPricingRepo{},
		&stubCurrentSubscriptionChecker{err: workspace_plan.ErrSubscriptionNotCurrent},
		nil,
		nil,
	)

	output, err := uc.Execute(invoice.CreateInvoiceInput{
		WorkspaceID:      "ws-1",
		UserID:           "user-1",
		Purpose:          invoice.PurposeSubscription,
		PlanDefinitionID: "plan-1",
		AmountBRL:        199,
		BillingType:      "PIX",
		Description:      "Plano Starter",
	})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if output.Invoice == nil || output.Invoice.NormalizedPurpose() != invoice.PurposeSubscription {
		t.Fatalf("expected subscription invoice output, got %+v", output.Invoice)
	}
	if repo.created == nil || repo.created.PlanDefinitionID == nil || *repo.created.PlanDefinitionID != "plan-1" {
		t.Fatalf("expected persisted plan definition ID plan-1, got %+v", repo.created)
	}
	if asaasService.createCalls != 1 {
		t.Fatalf("expected one Asaas payment creation, got %d", asaasService.createCalls)
	}
}

type errStubInvoiceRepo struct {
	stubInvoiceRepo
	createErr error
}

func (r *errStubInvoiceRepo) Create(inv *invoice.Invoice) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.created = inv
	return nil
}

type errStubUserRepo struct {
	stubUserRepo
	findErr error
}

func (r *errStubUserRepo) FindByID(string) (*user.User, error) {
	return nil, r.findErr
}

type errStubAsaasService struct {
	stubAsaasService
	createErr error
	qrErr     error
}

func (s *errStubAsaasService) CreatePayment(name, cpf string, p *asaas.AsaasPayment) (*asaas.AsaasPayment, error) {
	if s.createErr != nil {
		return nil, s.createErr
	}
	s.createCalls++
	return &asaas.AsaasPayment{ID: "pay-1", BankSlipUrl: "https://boleto", InvoiceUrl: "https://invoice"}, nil
}

func (s *errStubAsaasService) GetPaymentQrCode(string) (string, string, error) {
	if s.qrErr != nil {
		return "", "", s.qrErr
	}
	return "qr-code", "pix-copy", nil
}

type errStubPricingRepo struct {
	stubPricingRepo
	listErr error
	items   []workspace_pricing.PricingItem
}

func (r *errStubPricingRepo) ListDefaultPricingItems() ([]workspace_pricing.PricingItem, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	if r.items != nil {
		return r.items, nil
	}
	return nil, nil
}

func TestGetInvoiceUseCase(t *testing.T) {
	repo := &stubInvoiceRepo{}
	uc := NewGetInvoiceUseCase(repo)

	inv, err := uc.Execute("inv-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inv != nil {
		t.Fatalf("expected nil, got %+v", inv)
	}
}

func TestListInvoicesUseCase(t *testing.T) {
	repo := &stubInvoiceRepo{}
	uc := NewListInvoicesUseCase(repo)
	invs, total, err := uc.Execute("ws-1", 1, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if invs != nil || total != 0 {
		t.Fatalf("expected empty, got %+v %d", invs, total)
	}
}

func TestCreateInvoice_InvalidAmount(t *testing.T) {
	uc := NewCreateInvoiceUseCase(&stubInvoiceRepo{}, &stubUserRepo{user: &user.User{}}, &stubAsaasService{}, &stubPricingRepo{}, nil, nil, nil)
	_, err := uc.Execute(invoice.CreateInvoiceInput{AmountBRL: 0})
	if !errors.Is(err, invoice.ErrInvalidAmount) {
		t.Fatalf("expected ErrInvalidAmount, got %v", err)
	}
}

func TestCreateInvoice_InvalidPurpose(t *testing.T) {
	uc := NewCreateInvoiceUseCase(&stubInvoiceRepo{}, &stubUserRepo{user: &user.User{}}, &stubAsaasService{}, &stubPricingRepo{}, nil, nil, nil)
	_, err := uc.Execute(invoice.CreateInvoiceInput{AmountBRL: 10, Purpose: "BOGUS"})
	if !errors.Is(err, invoice.ErrInvalidPurpose) {
		t.Fatalf("expected ErrInvalidPurpose, got %v", err)
	}
}

func TestCreateInvoice_SubscriptionMissingPlanID(t *testing.T) {
	uc := NewCreateInvoiceUseCase(&stubInvoiceRepo{}, &stubUserRepo{user: &user.User{}}, &stubAsaasService{}, &stubPricingRepo{}, nil, nil, nil)
	_, err := uc.Execute(invoice.CreateInvoiceInput{AmountBRL: 10, Purpose: invoice.PurposeSubscription})
	if !errors.Is(err, invoice.ErrPlanDefinitionRequired) {
		t.Fatalf("expected ErrPlanDefinitionRequired, got %v", err)
	}
}

func TestCreateInvoice_MissingCustomerDocument(t *testing.T) {
	asaasStub := &stubAsaasService{}
	uc := NewCreateInvoiceUseCase(
		&stubInvoiceRepo{},
		&stubUserRepo{user: &user.User{ID: "user-1", Username: "NoDoc"}}, // no CPF/CNPJ on file
		asaasStub,
		&stubPricingRepo{},
		nil, nil, nil,
	)
	_, err := uc.Execute(invoice.CreateInvoiceInput{
		WorkspaceID: "ws-1", UserID: "user-1", AmountBRL: 10,
		Purpose: invoice.PurposeSubscription, PlanDefinitionID: "plan-1",
	})
	if !errors.Is(err, invoice.ErrCustomerDocumentRequired) {
		t.Fatalf("expected ErrCustomerDocumentRequired, got %v", err)
	}
	if asaasStub.createCalls != 0 {
		t.Fatalf("expected no Asaas charge when document missing, got %d calls", asaasStub.createCalls)
	}
}

func TestCreateInvoice_UserNotFound(t *testing.T) {
	uc := NewCreateInvoiceUseCase(
		&stubInvoiceRepo{},
		&errStubUserRepo{findErr: errors.New("not found")},
		&stubAsaasService{},
		&stubPricingRepo{},
		&stubCurrentSubscriptionChecker{},
		nil,
		nil,
	)
	_, err := uc.Execute(invoice.CreateInvoiceInput{
		WorkspaceID: "ws-1", UserID: "user-1", AmountBRL: 10, Purpose: invoice.PurposeTopUp,
	})
	if err == nil || err.Error() != "user not found: not found" {
		t.Fatalf("expected user not found error, got %v", err)
	}
}

func TestCreateInvoice_AsaasCreateError(t *testing.T) {
	uc := NewCreateInvoiceUseCase(
		&stubInvoiceRepo{},
		&stubUserRepo{user: &user.User{ID: "user-1", Username: "Test", CPF: "123"}},
		&errStubAsaasService{createErr: errors.New("gateway down")},
		&stubPricingRepo{},
		&stubCurrentSubscriptionChecker{},
		nil,
		nil,
	)
	_, err := uc.Execute(invoice.CreateInvoiceInput{
		WorkspaceID: "ws-1", UserID: "user-1", AmountBRL: 10, Purpose: invoice.PurposeTopUp,
	})
	if err == nil || err.Error() != "failed to create payment: gateway down" {
		t.Fatalf("expected payment error, got %v", err)
	}
}

func TestCreateInvoice_RepoCreateError(t *testing.T) {
	uc := NewCreateInvoiceUseCase(
		&errStubInvoiceRepo{createErr: errors.New("db error")},
		&stubUserRepo{user: &user.User{ID: "user-1", Username: "Test", CPF: "123"}},
		&stubAsaasService{},
		&stubPricingRepo{},
		&stubCurrentSubscriptionChecker{},
		nil,
		nil,
	)
	_, err := uc.Execute(invoice.CreateInvoiceInput{
		WorkspaceID: "ws-1", UserID: "user-1", AmountBRL: 10, Purpose: invoice.PurposeTopUp,
	})
	if err == nil || err.Error() != "failed to save invoice: db error" {
		t.Fatalf("expected save error, got %v", err)
	}
}

func TestCreateInvoice_BOLETOSkipsQrCode(t *testing.T) {
	repo := &stubInvoiceRepo{}
	asaasSvc := &errStubAsaasService{}
	uc := NewCreateInvoiceUseCase(
		repo,
		&stubUserRepo{user: &user.User{ID: "user-1", Email: "test@test.com", CNPJ: "12345"}},
		asaasSvc,
		&stubPricingRepo{},
		&stubCurrentSubscriptionChecker{},
		nil,
		nil,
	)
	out, err := uc.Execute(invoice.CreateInvoiceInput{
		WorkspaceID: "ws-1", UserID: "user-1", AmountBRL: 50,
		Purpose: invoice.PurposeTopUp, BillingType: "BOLETO",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Invoice.PixQrCode != nil || out.Invoice.PixCopy != nil {
		t.Fatal("BOLETO should have no QR code")
	}
	if out.Invoice.BankSlipUrl == nil {
		t.Fatal("BOLETO should have bank slip URL")
	}
	if out.Invoice.BillingType != "BOLETO" {
		t.Fatalf("expected BOLETO, got %s", out.Invoice.BillingType)
	}
}

func TestCreateInvoice_PIXQrCodeError(t *testing.T) {
	repo := &stubInvoiceRepo{}
	asaasSvc := &errStubAsaasService{qrErr: errors.New("qr fail")}
	uc := NewCreateInvoiceUseCase(
		repo,
		&stubUserRepo{user: &user.User{ID: "user-1", Username: "Test", CPF: "123"}},
		asaasSvc,
		&stubPricingRepo{},
		&stubCurrentSubscriptionChecker{},
		nil,
		nil,
	)
	out, err := uc.Execute(invoice.CreateInvoiceInput{
		WorkspaceID: "ws-1", UserID: "user-1", AmountBRL: 10,
		Purpose: invoice.PurposeTopUp, BillingType: "PIX",
	})
	if err != nil {
		t.Fatalf("unexpected error (should succeed even with QR error): %v", err)
	}
	if out.Invoice.PixQrCode != nil || out.Invoice.PixCopy != nil {
		t.Fatal("expected nil QR code on QR fetch failure")
	}
}

func TestCreateInvoice_DefaultBillingTypeAndDescription(t *testing.T) {
	repo := &stubInvoiceRepo{}
	uc := NewCreateInvoiceUseCase(
		repo,
		&stubUserRepo{user: &user.User{ID: "user-1", Username: "Test", CPF: "123"}},
		&stubAsaasService{},
		&stubPricingRepo{},
		&stubCurrentSubscriptionChecker{},
		nil,
		nil,
	)
	out, err := uc.Execute(invoice.CreateInvoiceInput{
		WorkspaceID: "ws-1", UserID: "user-1", AmountBRL: 10,
		Purpose: invoice.PurposeTopUp,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Invoice.BillingType != "PIX" {
		t.Fatalf("expected default PIX, got %s", out.Invoice.BillingType)
	}
	if out.Invoice.Description != "Recarga de saldo" {
		t.Fatalf("expected default description 'Recarga de saldo', got %s", out.Invoice.Description)
	}
}

func TestCreateInvoice_SubscriptionDefaultDescription(t *testing.T) {
	repo := &stubInvoiceRepo{}
	uc := NewCreateInvoiceUseCase(
		repo,
		&stubUserRepo{user: &user.User{ID: "user-1", Username: "Test", CPF: "123"}},
		&stubAsaasService{},
		&stubPricingRepo{},
		nil,
		nil,
		nil,
	)
	out, err := uc.Execute(invoice.CreateInvoiceInput{
		WorkspaceID:      "ws-1",
		UserID:           "user-1",
		AmountBRL:        199,
		Purpose:          invoice.PurposeSubscription,
		PlanDefinitionID: "plan-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Invoice.Description != "Assinatura de plano" {
		t.Fatalf("expected 'Assinatura de plano', got %s", out.Invoice.Description)
	}
}

func TestCreateInvoice_UserFallbackToCNPJAndEmail(t *testing.T) {
	repo := &stubInvoiceRepo{}
	uc := NewCreateInvoiceUseCase(
		repo,
		&stubUserRepo{user: &user.User{ID: "user-1", Email: "a@b.com", CNPJ: "99999"}},
		&stubAsaasService{},
		&stubPricingRepo{},
		&stubCurrentSubscriptionChecker{},
		nil,
		nil,
	)
	out, err := uc.Execute(invoice.CreateInvoiceInput{
		WorkspaceID: "ws-1", UserID: "user-1", AmountBRL: 10,
		Purpose: invoice.PurposeTopUp,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Invoice == nil {
		t.Fatal("expected invoice")
	}
}

func TestCreateInvoice_TopUp_EmptyWorkspaceID(t *testing.T) {
	uc := NewCreateInvoiceUseCase(
		&stubInvoiceRepo{},
		&stubUserRepo{user: &user.User{}},
		&stubAsaasService{},
		&stubPricingRepo{},
		&stubCurrentSubscriptionChecker{},
		nil,
		nil,
	)
	_, err := uc.Execute(invoice.CreateInvoiceInput{
		WorkspaceID: "", UserID: "user-1", AmountBRL: 10, Purpose: invoice.PurposeTopUp,
	})
	if !errors.Is(err, invoice.ErrActiveSubscriptionRequired) {
		t.Fatalf("expected ErrActiveSubscriptionRequired, got %v", err)
	}
}

func TestCreateInvoice_TopUp_NilSubscriptionChecker(t *testing.T) {
	uc := NewCreateInvoiceUseCase(
		&stubInvoiceRepo{},
		&stubUserRepo{user: &user.User{}},
		&stubAsaasService{},
		&stubPricingRepo{},
		nil,
		nil,
		nil,
	)
	_, err := uc.Execute(invoice.CreateInvoiceInput{
		WorkspaceID: "ws-1", UserID: "user-1", AmountBRL: 10, Purpose: invoice.PurposeTopUp,
	})
	if !errors.Is(err, invoice.ErrActiveSubscriptionRequired) {
		t.Fatalf("expected ErrActiveSubscriptionRequired, got %v", err)
	}
}

func TestCreateInvoice_TopUp_SubscriptionNotFound(t *testing.T) {
	uc := NewCreateInvoiceUseCase(
		&stubInvoiceRepo{},
		&stubUserRepo{user: &user.User{}},
		&stubAsaasService{},
		&stubPricingRepo{},
		&stubCurrentSubscriptionChecker{err: workspace_plan.ErrSubscriptionNotFound},
		nil,
		nil,
	)
	_, err := uc.Execute(invoice.CreateInvoiceInput{
		WorkspaceID: "ws-1", UserID: "user-1", AmountBRL: 10, Purpose: invoice.PurposeTopUp,
	})
	if !errors.Is(err, invoice.ErrActiveSubscriptionRequired) {
		t.Fatalf("expected ErrActiveSubscriptionRequired, got %v", err)
	}
}

func TestCreateInvoice_TopUp_UnexpectedError(t *testing.T) {
	uc := NewCreateInvoiceUseCase(
		&stubInvoiceRepo{},
		&stubUserRepo{user: &user.User{}},
		&stubAsaasService{},
		&stubPricingRepo{},
		&stubCurrentSubscriptionChecker{err: errors.New("db error")},
		nil,
		nil,
	)
	_, err := uc.Execute(invoice.CreateInvoiceInput{
		WorkspaceID: "ws-1", UserID: "user-1", AmountBRL: 10, Purpose: invoice.PurposeTopUp,
	})
	if err == nil || err.Error() != "db error" {
		t.Fatalf("expected db error passthrough, got %v", err)
	}
}

func TestCreateInvoice_ExchangeRate_RepoError_Fallback(t *testing.T) {
	repo := &stubInvoiceRepo{}
	uc := NewCreateInvoiceUseCase(
		repo,
		&stubUserRepo{user: &user.User{ID: "user-1", Username: "Test", CPF: "123"}},
		&stubAsaasService{},
		&errStubPricingRepo{listErr: errors.New("redis down")},
		&stubCurrentSubscriptionChecker{},
		nil,
		nil,
	)
	out, err := uc.Execute(invoice.CreateInvoiceInput{
		WorkspaceID: "ws-1", UserID: "user-1", AmountBRL: 60, Purpose: invoice.PurposeTopUp,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out.Invoice.ExchangeRate != 6.0 {
		t.Fatalf("expected fallback rate 6.0, got %f", out.Invoice.ExchangeRate)
	}
	if out.Invoice.AmountUSD != 10_000_000 {
		t.Fatalf("expected 10_000_000 µUSD, got %d", out.Invoice.AmountUSD)
	}
}

func TestCreateInvoice_ExchangeRate_NoItem_Fallback(t *testing.T) {
	repo := &stubInvoiceRepo{}
	uc := NewCreateInvoiceUseCase(
		repo,
		&stubUserRepo{user: &user.User{ID: "user-1", Username: "Test", CPF: "123"}},
		&stubAsaasService{},
		&errStubPricingRepo{items: []workspace_pricing.PricingItem{{Category: "other", Service: "other", PriceMicros: 1}}},
		&stubCurrentSubscriptionChecker{},
		nil,
		nil,
	)
	out, err := uc.Execute(invoice.CreateInvoiceInput{
		WorkspaceID: "ws-1", UserID: "user-1", AmountBRL: 60, Purpose: invoice.PurposeTopUp,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Invoice.ExchangeRate != 6.0 {
		t.Fatalf("expected fallback rate 6.0, got %f", out.Invoice.ExchangeRate)
	}
}

type stubAffiliateRepo struct {
	existingReferral *affiliate.Referral
	getReferralCalls int
}

func (r *stubAffiliateRepo) Create(context.Context, *affiliate.Affiliate) error { return nil }
func (r *stubAffiliateRepo) GetByID(context.Context, string) (*affiliate.Affiliate, error) {
	return nil, nil
}
func (r *stubAffiliateRepo) GetByUserID(context.Context, string) (*affiliate.Affiliate, error) {
	return nil, nil
}
func (r *stubAffiliateRepo) GetByCode(context.Context, string) (*affiliate.Affiliate, error) {
	return nil, nil
}
func (r *stubAffiliateRepo) Update(context.Context, *affiliate.Affiliate) error { return nil }
func (r *stubAffiliateRepo) List(context.Context, int, int) ([]affiliate.Affiliate, int64, error) {
	return nil, 0, nil
}
func (r *stubAffiliateRepo) CreateReferral(context.Context, *affiliate.Referral) error { return nil }
func (r *stubAffiliateRepo) GetReferralByWorkspaceID(_ context.Context, _ string) (*affiliate.Referral, error) {
	r.getReferralCalls++
	return r.existingReferral, nil
}
func (r *stubAffiliateRepo) ListReferralsByAffiliateID(context.Context, string, int, int) ([]affiliate.Referral, int64, error) {
	return nil, 0, nil
}
func (r *stubAffiliateRepo) CountReferralsByAffiliateID(context.Context, string) (int64, error) {
	return 0, nil
}
func (r *stubAffiliateRepo) CreateEarning(context.Context, *affiliate.Earning) error { return nil }
func (r *stubAffiliateRepo) GetEarningByInvoiceID(context.Context, string) (*affiliate.Earning, error) {
	return nil, nil
}
func (r *stubAffiliateRepo) ListEarningsByAffiliateID(context.Context, string, int, int) ([]affiliate.Earning, int64, error) {
	return nil, 0, nil
}
func (r *stubAffiliateRepo) SumEarningsByAffiliateID(context.Context, string) (int64, error) {
	return 0, nil
}
func (r *stubAffiliateRepo) SumEarningsSince(context.Context, string, int64) (int64, error) {
	return 0, nil
}

type stubInvoiceTrackReferral struct {
	calls []affiliate.TrackReferralInput
	err   error
}

func (s *stubInvoiceTrackReferral) Execute(_ context.Context, input affiliate.TrackReferralInput) (*affiliate.Referral, error) {
	s.calls = append(s.calls, input)
	return nil, s.err
}

func newInvoiceUCForReferral(affRepo affiliate.Repository, track affiliate.TrackReferralUseCase) invoice.CreateInvoiceUseCase {
	return NewCreateInvoiceUseCase(
		&stubInvoiceRepo{},
		&stubUserRepo{user: &user.User{ID: "user-1", Username: "Tester", CPF: "12345678900"}},
		&stubAsaasService{},
		&stubPricingRepo{},
		&stubCurrentSubscriptionChecker{},
		affRepo,
		track,
	)
}

func TestCreateInvoice_Referral_NewWorkspace_Tracks(t *testing.T) {
	affRepo := &stubAffiliateRepo{existingReferral: nil}
	track := &stubInvoiceTrackReferral{}
	uc := newInvoiceUCForReferral(affRepo, track)

	_, err := uc.Execute(invoice.CreateInvoiceInput{
		WorkspaceID:  "ws-1",
		UserID:       "user-1",
		AmountBRL:    50,
		Purpose:      invoice.PurposeTopUp,
		BillingType:  "PIX",
		ReferralCode: "  MYCODE  ",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(track.calls) != 1 {
		t.Fatalf("expected 1 track call, got %d", len(track.calls))
	}
	if track.calls[0].Code != "MYCODE" {
		t.Fatalf("expected trimmed code MYCODE, got %q", track.calls[0].Code)
	}
	if track.calls[0].WorkspaceID != "ws-1" || track.calls[0].WorkspaceOwnerUserID != "user-1" {
		t.Fatalf("unexpected track input: %+v", track.calls[0])
	}
}

func TestCreateInvoice_Referral_AlreadyReferred_DoesNotTrack(t *testing.T) {
	affRepo := &stubAffiliateRepo{existingReferral: &affiliate.Referral{WorkspaceID: "ws-1", AffiliateID: "aff-existing"}}
	track := &stubInvoiceTrackReferral{}
	uc := newInvoiceUCForReferral(affRepo, track)

	_, err := uc.Execute(invoice.CreateInvoiceInput{
		WorkspaceID:  "ws-1",
		UserID:       "user-1",
		AmountBRL:    50,
		Purpose:      invoice.PurposeTopUp,
		BillingType:  "PIX",
		ReferralCode: "SOMECODE",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(track.calls) != 0 {
		t.Fatalf("track must NOT be invoked when workspace already referred (got %d)", len(track.calls))
	}
}

func TestCreateInvoice_Referral_EmptyCode_NoOp(t *testing.T) {
	affRepo := &stubAffiliateRepo{}
	track := &stubInvoiceTrackReferral{}
	uc := newInvoiceUCForReferral(affRepo, track)

	_, err := uc.Execute(invoice.CreateInvoiceInput{
		WorkspaceID: "ws-1",
		UserID:      "user-1",
		AmountBRL:   50,
		Purpose:     invoice.PurposeTopUp,
		BillingType: "PIX",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(track.calls) != 0 {
		t.Fatalf("track must NOT be called without a code")
	}
}

func TestCreateInvoice_Referral_TrackFailure_Swallowed(t *testing.T) {
	affRepo := &stubAffiliateRepo{}
	track := &stubInvoiceTrackReferral{err: affiliate.ErrInvalidReferralCode}
	uc := newInvoiceUCForReferral(affRepo, track)

	out, err := uc.Execute(invoice.CreateInvoiceInput{
		WorkspaceID:  "ws-1",
		UserID:       "user-1",
		AmountBRL:    50,
		Purpose:      invoice.PurposeTopUp,
		BillingType:  "PIX",
		ReferralCode: "UNKNOWN",
	})
	if err != nil {
		t.Fatalf("track failure must be swallowed, got: %v", err)
	}
	if out == nil || out.Invoice == nil {
		t.Fatal("invoice must still be created despite referral failure")
	}
}

func TestCreateInvoice_Referral_NilTrackReferral_NoOp(t *testing.T) {
	affRepo := &stubAffiliateRepo{}
	uc := newInvoiceUCForReferral(affRepo, nil)

	_, err := uc.Execute(invoice.CreateInvoiceInput{
		WorkspaceID:  "ws-1",
		UserID:       "user-1",
		AmountBRL:    50,
		Purpose:      invoice.PurposeTopUp,
		BillingType:  "PIX",
		ReferralCode: "X",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
