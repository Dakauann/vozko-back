package invoice

import (
	"testing"

	invoicedomain "vozko/domain/invoice"
)

func TestTopUpRequestsUseTheDomainBounds(t *testing.T) {
	for _, tc := range []struct {
		amount float64
		valid  bool
	}{
		{invoicedomain.MinTopUpBRL - 0.01, false},
		{invoicedomain.MinTopUpBRL, true},
		{invoicedomain.MaxTopUpBRL, true},
		{invoicedomain.MaxTopUpBRL + 0.01, false},
		{0, false},
		{-1, false},
	} {
		user := (&CreateInvoiceRequest{AmountBRL: tc.amount, BillingType: "PIX"}).Validate() == nil
		admin := (&CreateInvoiceRequest{AmountBRL: tc.amount}).ValidateAdmin() == nil
		if user != tc.valid || admin != tc.valid {
			t.Errorf("amount %v: user valid=%v admin valid=%v, want %v for both", tc.amount, user, admin, tc.valid)
		}
	}
}

func TestTopUpRequestsAcceptOnlyPixOrBoleto(t *testing.T) {
	for billingType, valid := range map[string]bool{"PIX": true, "boleto": true, "CREDIT_CARD": false, "": false} {
		if got := (&CreateInvoiceRequest{AmountBRL: 50, BillingType: billingType}).Validate() == nil; got != valid {
			t.Errorf("billing type %q valid = %v, want %v", billingType, got, valid)
		}
	}
}
