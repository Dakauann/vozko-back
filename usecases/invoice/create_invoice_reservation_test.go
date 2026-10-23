package invoice_usecase

import (
	"errors"
	"reflect"
	"testing"

	"vozko/domain/invoice"
	"vozko/domain/payment"
	"vozko/domain/user"
)

func topUpInput(key string) invoice.CreateInvoiceInput {
	return invoice.CreateInvoiceInput{
		WorkspaceID: "ws-1", UserID: "user-1", Purpose: invoice.PurposeTopUp,
		AmountBRL: 50, BillingType: "PIX", IdempotencyKey: key,
	}
}

func TestTheInvoiceIsSavedBeforeTheGatewayIsCalled(t *testing.T) {
	var steps []string
	repo := &stubInvoiceRepo{steps: &steps}
	gw := newStubGateway()
	gw.steps = &steps

	if _, err := newCreditableUC(repo, gw).Execute(topUpInput("")); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if want := []string{"reserve", "charge", "attach"}; !reflect.DeepEqual(steps, want) {
		t.Fatalf("steps = %v, want %v; a charge must never exist without its invoice", steps, want)
	}
	if repo.created.ExternalID != "pay-1" || repo.created.PixCopy == nil {
		t.Fatalf("the charge must be attached to the saved invoice, got %+v", repo.created)
	}
}

func TestTheGatewayKeyIsTheInvoiceExternalReference(t *testing.T) {
	repo := &stubInvoiceRepo{}
	gw := newStubGateway()

	if _, err := newCreditableUC(repo, gw).Execute(topUpInput("")); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := invoice.ExternalReference(repo.created.ID)
	if gw.lastRequest.IdempotencyKey != want || gw.lastRequest.ExternalReference != want {
		t.Fatalf("gateway key = %q, reference = %q, want both %q so a retry of this invoice is the same charge",
			gw.lastRequest.IdempotencyKey, gw.lastRequest.ExternalReference, want)
	}
}

func TestTheInvoiceRecordsItsProvider(t *testing.T) {
	repo := &stubInvoiceRepo{}
	gw := newStubGateway()
	gw.provider = payment.ProviderMercadoPago

	if _, err := newCreditableUC(repo, gw).Execute(topUpInput("")); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if repo.created.Provider != payment.ProviderMercadoPago {
		t.Fatalf("invoice provider = %q, want mercadopago so its webhooks and reversals find the right gateway", repo.created.Provider)
	}
}

func TestAFailedChargeCancelsTheReservedInvoice(t *testing.T) {
	var steps []string
	repo := &stubInvoiceRepo{steps: &steps}
	gw := &errStubGateway{stubGateway: *newStubGateway(), createErr: errors.New("gateway down")}

	_, err := NewCreateInvoiceUseCase(repo, creditableUser(), nil, gw, &stubPricingRepo{}, &stubCurrentSubscriptionChecker{}, nil, nil).Execute(topUpInput(""))
	if err == nil {
		t.Fatal("expected the gateway error")
	}
	if repo.created == nil || repo.created.Status != invoice.StatusCancelled {
		t.Fatalf("the reserved invoice must be cancelled when the gateway refuses, got %+v", repo.created)
	}
	if want := []string{"reserve", "status:CANCELLED"}; !reflect.DeepEqual(steps, want) {
		t.Fatalf("steps = %v, want %v", steps, want)
	}
}

func TestAChargeThatCannotBeAttachedKeepsItsReservation(t *testing.T) {
	repo := &stubInvoiceRepo{attachErr: errors.New("db blip")}
	gw := newStubGateway()

	_, err := newCreditableUC(repo, gw).Execute(topUpInput(""))
	if err == nil {
		t.Fatal("the caller must learn the invoice could not be completed")
	}
	if repo.created == nil || repo.created.Status == invoice.StatusCancelled {
		t.Fatalf("the reservation must stay so the charge's webhook finds it by reference, got %+v", repo.created)
	}
}

func TestARepeatedKeyNeverChargesTwiceEvenBeforeTheChargeIsAttached(t *testing.T) {
	repo := &stubInvoiceRepo{attachErr: errors.New("db blip")}
	gw := newStubGateway()
	uc := newCreditableUC(repo, gw)

	_, _ = uc.Execute(topUpInput("client:ws-1:k1"))
	again, err := uc.Execute(topUpInput("client:ws-1:k1"))
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if gw.createCalls != 1 {
		t.Fatalf("a repeated key must return the reservation, not charge again; charges = %d", gw.createCalls)
	}
	if again.Invoice.ID != repo.created.ID {
		t.Fatalf("expected the reserved invoice back, got %q", again.Invoice.ID)
	}
}

func TestAnInvoiceFromAnotherWorkspaceIsNeverReturnedForAKey(t *testing.T) {
	repo := &stubInvoiceRepo{byKey: map[string]*invoice.Invoice{
		"client:ws-1:k1": {ID: "inv-other", WorkspaceID: "ws-2", IdempotencyKey: "client:ws-1:k1"},
	}}
	gw := newStubGateway()

	_, err := newCreditableUC(repo, gw).Execute(topUpInput("client:ws-1:k1"))
	if !errors.Is(err, invoice.ErrInvalidIdempotencyKey) {
		t.Fatalf("expected ErrInvalidIdempotencyKey, got %v", err)
	}
	if gw.createCalls != 0 {
		t.Fatalf("no charge may be created, got %d", gw.createCalls)
	}
}

func TestAnInvoiceWithoutAPurposeIsRefused(t *testing.T) {
	repo := &stubInvoiceRepo{}
	gw := newStubGateway()
	input := topUpInput("")
	input.Purpose = ""

	if _, err := newCreditableUC(repo, gw).Execute(input); !errors.Is(err, invoice.ErrInvalidPurpose) {
		t.Fatalf("expected ErrInvalidPurpose, got %v", err)
	}
	if gw.createCalls != 0 || repo.created != nil {
		t.Fatal("an invoice without a purpose must never be saved or charged")
	}
}

func creditableUser() *stubUserRepo {
	return &stubUserRepo{user: &user.User{ID: "user-1", Username: "Tester", CPF: "12345678900"}}
}
