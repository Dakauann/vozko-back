package mercadopago

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/payment"
)

type fakeClient struct {
	createReq      CreatePaymentRequest
	createIdemKey  string
	createResponse *Payment
	createErr      error

	getID       string
	getResponse *Payment
	getErr      error

	refundID     string
	refundAmount float64
	refundErr    error

	cancelID  string
	cancelErr error
}

func (f *fakeClient) CreatePayment(_ context.Context, req CreatePaymentRequest, key string) (*Payment, error) {
	f.createReq = req
	f.createIdemKey = key
	if f.createErr != nil {
		return nil, f.createErr
	}
	if f.createResponse != nil {
		return f.createResponse, nil
	}
	return &Payment{ID: 1234567890, Status: StatusPending, PaymentMethodID: req.PaymentMethodID, TransactionAmount: req.TransactionAmount}, nil
}

func (f *fakeClient) GetPayment(_ context.Context, id string) (*Payment, error) {
	f.getID = id
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.getResponse != nil {
		return f.getResponse, nil
	}
	return &Payment{ID: 1234567890, Status: StatusApproved}, nil
}

func (f *fakeClient) RefundPayment(_ context.Context, id string, amount float64, _ string) (*Refund, error) {
	f.refundID, f.refundAmount = id, amount
	if f.refundErr != nil {
		return nil, f.refundErr
	}
	return &Refund{ID: 1, PaymentID: 1234567890, Amount: amount}, nil
}

func (f *fakeClient) CancelPayment(_ context.Context, id string) (*Payment, error) {
	f.cancelID = id
	if f.cancelErr != nil {
		return nil, f.cancelErr
	}
	return &Payment{ID: 1234567890, Status: StatusCancelled}, nil
}

var _ Client = (*fakeClient)(nil)

var fixedNow = time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)

func newTestGateway(f *fakeClient) payment.Gateway {
	return NewGateway(f, WithClock(func() time.Time { return fixedNow }))
}

func validCustomer() payment.GatewayCustomer {
	return payment.GatewayCustomer{
		Name:     "Maria da Silva",
		Email:    "maria@example.com",
		Document: "111.444.777-35",
	}
}

func TestGateway_ProviderAndCapabilities(t *testing.T) {
	g := newTestGateway(&fakeClient{})
	if g.Provider() != payment.ProviderMercadoPago {
		t.Fatalf("provider: got %q", g.Provider())
	}
	caps := g.Capabilities()
	if caps.Split {
		t.Fatal("Mercado Pago must not advertise split support")
	}
	if caps.WalletValidation {
		t.Fatal("Mercado Pago has no wallet-id concept to validate")
	}
	if !caps.PartialRefund {
		t.Fatal("partial refunds are supported")
	}
}

func TestGateway_CreatePixCharge(t *testing.T) {
	f := &fakeClient{createResponse: &Payment{
		ID:                1234567890,
		Status:            StatusPending,
		StatusDetail:      DetailPendingWaitingTransfer,
		PaymentMethodID:   PaymentMethodPix,
		TransactionAmount: 49.9,
		ExternalReference: "inv:abc",
		PointOfInteraction: PointOfInteraction{TransactionData: TransactionData{
			QRCode:       "00020126580014BR.GOV.BCB.PIX",
			QRCodeBase64: "aVZCT1J3MEtHZ29B",
			TicketURL:    "https://mp.test/ticket/1234567890",
		}},
	}}
	g := newTestGateway(f)

	charge, err := g.CreateCharge(context.Background(), payment.ChargeRequest{
		Method:            payment.MethodPix,
		Amount:            49.9,
		DueDate:           fixedNow.Add(72 * time.Hour),
		Description:       "Recarga de saldo",
		ExternalReference: "inv:abc",
		Customer:          validCustomer(),
		IdempotencyKey:    "inv:abc",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if f.createReq.PaymentMethodID != PaymentMethodPix {
		t.Fatalf("payment_method_id: got %q", f.createReq.PaymentMethodID)
	}
	if f.createReq.TransactionAmount != 49.9 {
		t.Fatalf("amount: got %v", f.createReq.TransactionAmount)
	}
	if f.createIdemKey != "inv:abc" {
		t.Fatalf("idempotency key not forwarded: %q", f.createIdemKey)
	}
	if f.createReq.Payer.Identification == nil ||
		f.createReq.Payer.Identification.Number != "11144477735" ||
		f.createReq.Payer.Identification.Type != IdentificationCPF {
		t.Fatalf("identification: got %+v", f.createReq.Payer.Identification)
	}
	if f.createReq.Payer.FirstName != "Maria" || f.createReq.Payer.LastName != "da Silva" {
		t.Fatalf("name not split: %+v", f.createReq.Payer)
	}
	if f.createReq.DateOfExpiration != "2026-09-03T12:00:00.000+00:00" {
		t.Fatalf("expiration layout: got %q", f.createReq.DateOfExpiration)
	}

	if charge.ID != "1234567890" {
		t.Fatalf("charge id: got %q", charge.ID)
	}
	if charge.Provider != payment.ProviderMercadoPago {
		t.Fatalf("provider: got %q", charge.Provider)
	}
	if charge.PixCopyPaste == "" || charge.PixQRCodeBase64 == "" {
		t.Fatalf("PIX payload not mapped: %+v", charge)
	}
	if charge.InvoiceURL != "https://mp.test/ticket/1234567890" {
		t.Fatalf("invoice URL: got %q", charge.InvoiceURL)
	}
	if charge.Status != payment.StatusPending {
		t.Fatalf("status: got %q", charge.Status)
	}
	if charge.ProviderStatus != StatusPending || charge.ProviderStatusDetail != DetailPendingWaitingTransfer {
		t.Fatalf("raw provider status not preserved: %+v", charge)
	}
}

func TestGateway_CreateChargeClampsPixExpiry(t *testing.T) {
	f := &fakeClient{}
	g := newTestGateway(f)

	if _, err := g.CreateCharge(context.Background(), payment.ChargeRequest{
		Method:   payment.MethodPix,
		Amount:   10,
		DueDate:  fixedNow.Add(time.Minute),
		Customer: validCustomer(),
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parsed, err := time.Parse(ExpirationLayout, f.createReq.DateOfExpiration)
	if err != nil {
		t.Fatalf("bad expiration: %v", err)
	}
	if !parsed.Equal(fixedNow.Add(MinPixExpiry)) {
		t.Fatalf("expected clamp to the 30-minute floor, got %v", parsed)
	}
}

func TestGateway_CreateChargeOmitsExpirationWhenNoDueDate(t *testing.T) {
	f := &fakeClient{}
	g := newTestGateway(f)

	if _, err := g.CreateCharge(context.Background(), payment.ChargeRequest{
		Method: payment.MethodPix, Amount: 10, Customer: validCustomer(),
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.createReq.DateOfExpiration != "" {
		t.Fatalf("expected no expiration, got %q", f.createReq.DateOfExpiration)
	}
}

func TestGateway_CreateBoletoCharge(t *testing.T) {
	f := &fakeClient{createResponse: &Payment{
		ID:                 222,
		Status:             StatusPending,
		PaymentMethodID:    PaymentMethodBoleto,
		TransactionAmount:  120,
		TransactionDetails: TransactionDetails{ExternalResourceURL: "https://mp.test/boleto.pdf"},
	}}
	g := newTestGateway(f)

	customer := validCustomer()
	customer.Address = &payment.GatewayAddress{
		ZipCode: "01310100", StreetName: "Av. Paulista", StreetNumber: "1000",
		Neighborhood: "Bela Vista", City: "São Paulo", FederalUnit: "SP",
	}

	charge, err := g.CreateCharge(context.Background(), payment.ChargeRequest{
		Method: payment.MethodBoleto, Amount: 120, Customer: customer,
		DueDate: fixedNow.AddDate(0, 0, 5),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.createReq.PaymentMethodID != PaymentMethodBoleto {
		t.Fatalf("payment_method_id: got %q", f.createReq.PaymentMethodID)
	}
	if f.createReq.Payer.Address == nil || f.createReq.Payer.Address.ZipCode != "01310100" {
		t.Fatalf("boleto address not forwarded: %+v", f.createReq.Payer.Address)
	}
	if charge.BoletoURL != "https://mp.test/boleto.pdf" {
		t.Fatalf("boleto URL: got %q", charge.BoletoURL)
	}
	if charge.PixCopyPaste != "" {
		t.Fatal("a boleto must carry no PIX payload")
	}
}

func TestGateway_BoletoRequiresAddress(t *testing.T) {
	g := newTestGateway(&fakeClient{})

	cases := []struct {
		name     string
		customer payment.GatewayCustomer
	}{
		{"no address at all", validCustomer()},
		{"address without zip", func() payment.GatewayCustomer {
			c := validCustomer()
			c.Address = &payment.GatewayAddress{StreetName: "Av. Paulista"}
			return c
		}()},
		{"address without street", func() payment.GatewayCustomer {
			c := validCustomer()
			c.Address = &payment.GatewayAddress{ZipCode: "01310100"}
			return c
		}()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := g.CreateCharge(context.Background(), payment.ChargeRequest{
				Method: payment.MethodBoleto, Amount: 10, Customer: c.customer,
			})
			if err == nil || !strings.Contains(err.Error(), "address") {
				t.Fatalf("expected a clear address error, got %v", err)
			}
		})
	}
}

func TestGateway_RejectsSplit(t *testing.T) {
	f := &fakeClient{}
	g := newTestGateway(f)

	_, err := g.CreateCharge(context.Background(), payment.ChargeRequest{
		Method:   payment.MethodPix,
		Amount:   100,
		Customer: validCustomer(),
		Splits: []payment.SplitRecipient{
			{RecipientID: "wallet-1", Percentage: 10},
		},
	})
	if !errors.Is(err, payment.ErrSplitUnsupported) {
		t.Fatalf("expected ErrSplitUnsupported, got %v", err)
	}
	if f.createReq.TransactionAmount != 0 {
		t.Fatal("no charge may be created when a split was requested")
	}
}

func TestGateway_CreateChargeValidation(t *testing.T) {
	g := newTestGateway(&fakeClient{})

	noDoc := validCustomer()
	noDoc.Document = "  "
	if _, err := g.CreateCharge(context.Background(), payment.ChargeRequest{
		Method: payment.MethodPix, Amount: 10, Customer: noDoc,
	}); !errors.Is(err, payment.ErrCustomerDocumentRequired) {
		t.Fatalf("expected ErrCustomerDocumentRequired, got %v", err)
	}

	junkDoc := validCustomer()
	junkDoc.Document = "..-/"
	if _, err := g.CreateCharge(context.Background(), payment.ChargeRequest{
		Method: payment.MethodPix, Amount: 10, Customer: junkDoc,
	}); !errors.Is(err, payment.ErrCustomerDocumentRequired) {
		t.Fatalf("expected ErrCustomerDocumentRequired for a punctuation-only document, got %v", err)
	}

	noEmail := validCustomer()
	noEmail.Email = ""
	if _, err := g.CreateCharge(context.Background(), payment.ChargeRequest{
		Method: payment.MethodPix, Amount: 10, Customer: noEmail,
	}); !errors.Is(err, payment.ErrCustomerEmailRequired) {
		t.Fatalf("expected ErrCustomerEmailRequired, got %v", err)
	}

	for _, amount := range []float64{0, -1} {
		if _, err := g.CreateCharge(context.Background(), payment.ChargeRequest{
			Method: payment.MethodPix, Amount: amount, Customer: validCustomer(),
		}); !errors.Is(err, payment.ErrInvalidAmount) {
			t.Fatalf("amount %v: expected ErrInvalidAmount, got %v", amount, err)
		}
	}

	if _, err := g.CreateCharge(context.Background(), payment.ChargeRequest{
		Method: payment.MethodCreditCard, Amount: 10, Customer: validCustomer(),
	}); !errors.Is(err, payment.ErrMethodUnsupported) {
		t.Fatalf("expected ErrMethodUnsupported for a card charge, got %v", err)
	}
}

func TestGateway_CreateChargeDefaultsToPix(t *testing.T) {
	f := &fakeClient{}
	g := newTestGateway(f)
	if _, err := g.CreateCharge(context.Background(), payment.ChargeRequest{
		Amount: 10, Customer: validCustomer(),
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.createReq.PaymentMethodID != PaymentMethodPix {
		t.Fatalf("an unset method must default to PIX, got %q", f.createReq.PaymentMethodID)
	}
}

func TestGateway_CreateChargePropagatesClientError(t *testing.T) {
	g := newTestGateway(&fakeClient{createErr: &ResponseError{StatusCode: 400, Message: "invalid CPF"}})
	_, err := g.CreateCharge(context.Background(), payment.ChargeRequest{
		Method: payment.MethodPix, Amount: 10, Customer: validCustomer(),
	})
	var respErr *ResponseError
	if !errors.As(err, &respErr) {
		t.Fatalf("expected the API error to reach the caller, got %T: %v", err, err)
	}
}

func TestGateway_GetCharge(t *testing.T) {
	f := &fakeClient{getResponse: &Payment{
		ID: 999, Status: StatusApproved, StatusDetail: DetailAccredited,
		PaymentMethodID: PaymentMethodPix, TransactionAmount: 25,
		TransactionAmountRefunded: 5, ExternalReference: "inv:z",
	}}
	g := newTestGateway(f)

	charge, err := g.GetCharge(context.Background(), "999")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.getID != "999" {
		t.Fatalf("id not forwarded: %q", f.getID)
	}
	if charge.Status != payment.StatusReceived || charge.AmountRefunded != 5 {
		t.Fatalf("charge not mapped: %+v", charge)
	}
}

func TestGateway_GetChargeErrorTranslation(t *testing.T) {
	g := newTestGateway(&fakeClient{getErr: &ResponseError{StatusCode: 404}})
	if _, err := g.GetCharge(context.Background(), "1"); !errors.Is(err, payment.ErrChargeNotFound) {
		t.Fatalf("expected ErrChargeNotFound, got %v", err)
	}

	g2 := newTestGateway(&fakeClient{getErr: ErrInvalidPaymentID})
	if _, err := g2.GetCharge(context.Background(), "abc"); !errors.Is(err, payment.ErrInvalidChargeID) {
		t.Fatalf("expected ErrInvalidChargeID, got %v", err)
	}

	boom := errors.New("boom")
	g3 := newTestGateway(&fakeClient{getErr: boom})
	if _, err := g3.GetCharge(context.Background(), "1"); !errors.Is(err, boom) {
		t.Fatalf("expected the underlying error, got %v", err)
	}
}

func TestGateway_RefundCharge(t *testing.T) {
	f := &fakeClient{}
	g := newTestGateway(f)

	if err := g.RefundCharge(context.Background(), "1234567890", 10.5, "customer request"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.refundID != "1234567890" || f.refundAmount != 10.5 {
		t.Fatalf("refund args: %q %v", f.refundID, f.refundAmount)
	}

	if err := g.RefundCharge(context.Background(), "1234567890", 0, ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.refundAmount != 0 {
		t.Fatalf("expected a full refund, got amount %v", f.refundAmount)
	}
}

func TestGateway_RefundChargeErrors(t *testing.T) {
	g := newTestGateway(&fakeClient{refundErr: &ResponseError{StatusCode: 404}})
	if err := g.RefundCharge(context.Background(), "1", 1, ""); !errors.Is(err, payment.ErrChargeNotFound) {
		t.Fatalf("expected ErrChargeNotFound, got %v", err)
	}
}

func TestGateway_CancelCharge(t *testing.T) {
	f := &fakeClient{}
	g := newTestGateway(f)
	if err := g.CancelCharge(context.Background(), "1234567890"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.cancelID != "1234567890" {
		t.Fatalf("id not forwarded: %q", f.cancelID)
	}

	g2 := newTestGateway(&fakeClient{cancelErr: &ResponseError{StatusCode: 404}})
	if err := g2.CancelCharge(context.Background(), "1"); !errors.Is(err, payment.ErrChargeNotFound) {
		t.Fatalf("expected ErrChargeNotFound, got %v", err)
	}
}

func TestGateway_NilClientIsSafe(t *testing.T) {
	g := NewGateway(nil)
	if _, err := g.CreateCharge(context.Background(), payment.ChargeRequest{Amount: 1, Customer: validCustomer()}); err == nil {
		t.Fatal("expected an error with no client configured")
	}
	if _, err := g.GetCharge(context.Background(), "1"); err == nil {
		t.Fatal("expected an error with no client configured")
	}
	if err := g.RefundCharge(context.Background(), "1", 1, ""); err == nil {
		t.Fatal("expected an error with no client configured")
	}
	if err := g.CancelCharge(context.Background(), "1"); err == nil {
		t.Fatal("expected an error with no client configured")
	}
}

func TestGateway_ToChargePrefersTicketURLOverBoletoURL(t *testing.T) {
	f := &fakeClient{getResponse: &Payment{
		ID: 1, Status: StatusPending, PaymentMethodID: PaymentMethodPix,
		TransactionDetails: TransactionDetails{ExternalResourceURL: "https://mp.test/slip"},
		PointOfInteraction: PointOfInteraction{TransactionData: TransactionData{TicketURL: "https://mp.test/pix"}},
	}}
	charge, err := newTestGateway(f).GetCharge(context.Background(), "1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if charge.InvoiceURL != "https://mp.test/pix" {
		t.Fatalf("invoice URL: got %q", charge.InvoiceURL)
	}
}

func TestGateway_ToChargeMapsExpiration(t *testing.T) {
	expiry := fixedNow.Add(48 * time.Hour)
	f := &fakeClient{getResponse: &Payment{ID: 1, Status: StatusPending, DateOfExpiration: &expiry}}
	charge, err := newTestGateway(f).GetCharge(context.Background(), "1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if charge.DueDate == nil || !charge.DueDate.Equal(expiry) {
		t.Fatalf("due date: got %v", charge.DueDate)
	}
}

func TestGateway_SandboxPayerEmailOverride(t *testing.T) {
	f := &fakeClient{}
	g := NewGateway(f,
		WithClock(func() time.Time { return fixedNow }),
		WithSandboxPayerEmail("test_user_4138@testuser.com"),
	)

	if _, err := g.CreateCharge(context.Background(), payment.ChargeRequest{
		Method: payment.MethodPix, Amount: 5, Customer: validCustomer(),
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.createReq.Payer.Email != "test_user_4138@testuser.com" {
		t.Fatalf("payer email not overridden: %q", f.createReq.Payer.Email)
	}
	if f.createReq.Payer.Identification.Number != "11144477735" {
		t.Fatalf("the override must not touch the document: %+v", f.createReq.Payer.Identification)
	}
	if f.createReq.Payer.FirstName != "Maria" {
		t.Fatalf("the override must not touch the name: %+v", f.createReq.Payer)
	}
}

func TestGateway_SandboxPayerEmailAbsentLeavesRealEmail(t *testing.T) {
	f := &fakeClient{}
	g := newTestGateway(f)

	if _, err := g.CreateCharge(context.Background(), payment.ChargeRequest{
		Method: payment.MethodPix, Amount: 5, Customer: validCustomer(),
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.createReq.Payer.Email != "maria@example.com" {
		t.Fatalf("without an override the real email must be used, got %q", f.createReq.Payer.Email)
	}
}

func TestGateway_SandboxOverrideDoesNotMaskMissingEmail(t *testing.T) {
	g := NewGateway(&fakeClient{}, WithSandboxPayerEmail("test_user_4138@testuser.com"))

	noEmail := validCustomer()
	noEmail.Email = "   "
	_, err := g.CreateCharge(context.Background(), payment.ChargeRequest{
		Method: payment.MethodPix, Amount: 5, Customer: noEmail,
	})
	if !errors.Is(err, payment.ErrCustomerEmailRequired) {
		t.Fatalf("expected ErrCustomerEmailRequired, got %v", err)
	}
}

func TestGateway_SandboxPayerStatusOverride(t *testing.T) {
	f := &fakeClient{}
	g := NewGateway(f,
		WithClock(func() time.Time { return fixedNow }),
		WithSandboxPayerStatus(SandboxStatusApproved),
	)

	if _, err := g.CreateCharge(context.Background(), payment.ChargeRequest{
		Method: payment.MethodPix, Amount: 5, Customer: validCustomer(),
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.createReq.Payer.FirstName != "APRO" {
		t.Fatalf("status keyword not sent as first_name: %q", f.createReq.Payer.FirstName)
	}
	if f.createReq.Payer.LastName != "da Silva" {
		t.Fatalf("last name must be untouched, got %q", f.createReq.Payer.LastName)
	}
	if f.createReq.Payer.Identification.Number != "11144477735" {
		t.Fatalf("document must be untouched, got %+v", f.createReq.Payer.Identification)
	}
}

func TestGateway_SandboxPayerStatusNormalizesCase(t *testing.T) {
	f := &fakeClient{}
	g := NewGateway(f, WithSandboxPayerStatus("  apro "))

	if _, err := g.CreateCharge(context.Background(), payment.ChargeRequest{
		Method: payment.MethodPix, Amount: 5, Customer: validCustomer(),
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.createReq.Payer.FirstName != "APRO" {
		t.Fatalf("keyword not normalized, got %q", f.createReq.Payer.FirstName)
	}
}

func TestGateway_SandboxPayerStatusAbsentKeepsRealName(t *testing.T) {
	f := &fakeClient{}
	if _, err := newTestGateway(f).CreateCharge(context.Background(), payment.ChargeRequest{
		Method: payment.MethodPix, Amount: 5, Customer: validCustomer(),
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.createReq.Payer.FirstName != "Maria" {
		t.Fatalf("without an override the real name must be used, got %q", f.createReq.Payer.FirstName)
	}
}
