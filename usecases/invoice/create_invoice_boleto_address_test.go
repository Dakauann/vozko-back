package invoice_usecase

import (
	"testing"

	"errors"

	"vozko/domain/address"
	"vozko/domain/invoice"
	"vozko/domain/payment"
	"vozko/domain/user"
)

type stubAddressRepo struct {
	addresses []*address.Address
	err       error
	calls     int
}

func (r *stubAddressRepo) Create(*address.Address) error { return nil }
func (r *stubAddressRepo) GetByID(string, string) (*address.Address, error) {
	return nil, nil
}
func (r *stubAddressRepo) GetAllByUserID(string) ([]*address.Address, error) {
	r.calls++
	return r.addresses, r.err
}
func (r *stubAddressRepo) Update(*address.Address) error                  { return nil }
func (r *stubAddressRepo) Delete(string, string) error                    { return nil }
func (r *stubAddressRepo) CountByUserID(string) (int, error)              { return 0, nil }
func (r *stubAddressRepo) UpdateDefaultStatus(string, string, bool) error { return nil }

var _ address.AddressRepository = (*stubAddressRepo)(nil)

func boletoUC(gw *stubGateway, addrRepo address.AddressRepository) invoice.CreateInvoiceUseCase {
	return NewCreateInvoiceUseCase(
		&stubInvoiceRepo{},
		&stubUserRepo{user: &user.User{ID: "user-1", Username: "Maria Silva", Email: "m@e.com", CPF: "11144477735"}},
		addrRepo,
		gw,
		&stubPricingRepo{},
		&stubCurrentSubscriptionChecker{},
		nil,
		nil,
	)
}

func boletoInput() invoice.CreateInvoiceInput {
	return invoice.CreateInvoiceInput{
		WorkspaceID: "ws-1", UserID: "user-1", AmountBRL: 50,
		Purpose: invoice.PurposeTopUp, BillingType: "BOLETO",
	}
}

func TestCreateInvoice_BoletoAttachesDefaultAddress(t *testing.T) {
	gw := newStubGateway()
	addrRepo := &stubAddressRepo{addresses: []*address.Address{
		{ID: "a1", Street: "Rua Antiga", Number: "1", ZipCode: "00000000", City: "Old", State: "RJ"},
		{ID: "a2", Street: "Av. Paulista", Number: "1000", District: "Bela Vista",
			City: "Sao Paulo", State: "SP", ZipCode: "01310100", IsDefault: true},
	}}

	if _, err := boletoUC(gw, addrRepo).Execute(boletoInput()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := gw.lastRequest.Customer.Address
	if got == nil {
		t.Fatal("boleto charge was sent with no payer address")
	}
	if got.ZipCode != "01310100" || got.StreetName != "Av. Paulista" || got.StreetNumber != "1000" {
		t.Fatalf("default address not used: %+v", got)
	}
	if got.Neighborhood != "Bela Vista" || got.City != "Sao Paulo" || got.FederalUnit != "SP" {
		t.Fatalf("address fields not mapped: %+v", got)
	}
}

func TestCreateInvoice_BoletoFallsBackToFirstAddress(t *testing.T) {
	gw := newStubGateway()
	addrRepo := &stubAddressRepo{addresses: []*address.Address{
		{ID: "a1", Street: "Rua Unica", Number: "7", ZipCode: "22222222", City: "Rio", State: "RJ"},
	}}

	if _, err := boletoUC(gw, addrRepo).Execute(boletoInput()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := gw.lastRequest.Customer.Address; got == nil || got.ZipCode != "22222222" {
		t.Fatalf("expected the only address to be used, got %+v", got)
	}
}

func TestCreateInvoice_PixNeverLooksUpAnAddress(t *testing.T) {
	gw := newStubGateway()
	addrRepo := &stubAddressRepo{}

	input := boletoInput()
	input.BillingType = "PIX"
	if _, err := boletoUC(gw, addrRepo).Execute(input); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addrRepo.calls != 0 {
		t.Fatalf("PIX must not query the address repository, got %d calls", addrRepo.calls)
	}
	if gw.lastRequest.Customer.Address != nil {
		t.Fatal("PIX charge must carry no address")
	}
	if gw.lastRequest.Method != payment.MethodBoleto && gw.lastRequest.Method != payment.MethodPix {
		t.Fatalf("unexpected method %q", gw.lastRequest.Method)
	}
}

func TestCreateInvoice_BoletoWithoutAddressStillReachesTheGateway(t *testing.T) {
	gw := newStubGateway()

	for _, repo := range []address.AddressRepository{
		nil,
		&stubAddressRepo{},
		&stubAddressRepo{err: errStubAddress},
	} {
		if _, err := boletoUC(gw, repo).Execute(boletoInput()); err != nil {
			t.Fatalf("a missing address must not fail the use case: %v", err)
		}
		if gw.lastRequest.Customer.Address != nil {
			t.Fatal("expected no address when none is available")
		}
	}
}

var errStubAddress = errStubAddressType{}

type errStubAddressType struct{}

func (errStubAddressType) Error() string { return "address lookup failed" }

func TestCreateInvoice_BoletoWithoutAddressOnStrictProvider(t *testing.T) {
	gw := newStubGateway()
	gw.boletoNeedsAddress = true

	for name, repo := range map[string]address.AddressRepository{
		"no repository":  nil,
		"no addresses":   &stubAddressRepo{},
		"lookup failure": &stubAddressRepo{err: errStubAddress},
		"incomplete address": &stubAddressRepo{addresses: []*address.Address{
			{ID: "a1", Street: "Rua Sem Numero", ZipCode: "01310100"},
		}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := boletoUC(gw, repo).Execute(boletoInput())
			if !errors.Is(err, invoice.ErrBillingAddressRequired) {
				t.Fatalf("expected ErrBillingAddressRequired, got %v", err)
			}
			if gw.createCalls != 0 {
				t.Fatalf("the provider must not be called at all, got %d calls", gw.createCalls)
			}
		})
	}
}

func TestCreateInvoice_BoletoWithoutAddressOnLenientProvider(t *testing.T) {
	gw := newStubGateway()
	gw.boletoNeedsAddress = false

	if _, err := boletoUC(gw, &stubAddressRepo{}).Execute(boletoInput()); err != nil {
		t.Fatalf("a lenient provider must still issue the boleto: %v", err)
	}
	if gw.createCalls != 1 {
		t.Fatalf("expected the charge to be created, got %d calls", gw.createCalls)
	}
}

func TestCreateInvoice_CompleteAddressPassesTheStrictCheck(t *testing.T) {
	gw := newStubGateway()
	gw.boletoNeedsAddress = true
	addrRepo := &stubAddressRepo{addresses: []*address.Address{{
		ID: "a1", Street: "Av. Paulista", Number: "1000", District: "Bela Vista",
		City: "Sao Paulo", State: "SP", ZipCode: "01310100", IsDefault: true,
	}}}

	if _, err := boletoUC(gw, addrRepo).Execute(boletoInput()); err != nil {
		t.Fatalf("a complete address must pass: %v", err)
	}
	if !gw.lastRequest.Customer.Address.Complete() {
		t.Fatalf("address reached the gateway incomplete: %+v", gw.lastRequest.Customer.Address)
	}
}
