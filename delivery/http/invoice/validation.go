package invoice

import (
	"errors"
	"strings"

	invoicedomain "vozko/domain/invoice"
)

func topUpAmountError(amountBRL float64) map[string]string {
	switch err := invoicedomain.ValidateTopUpAmount(amountBRL); {
	case errors.Is(err, invoicedomain.ErrTopUpBelowMinimum):
		return map[string]string{"amountBrl": "minimum amount is R$ 5.00"}
	case errors.Is(err, invoicedomain.ErrTopUpAboveMaximum):
		return map[string]string{"amountBrl": "maximum amount is R$ 50,000.00"}
	default:
		return nil
	}
}

func (r *CreateInvoiceRequest) Validate() map[string]string {
	if errs := topUpAmountError(r.AmountBRL); errs != nil {
		return errs
	}
	billingType := strings.ToUpper(strings.TrimSpace(r.BillingType))
	if billingType != "PIX" && billingType != "BOLETO" {
		return map[string]string{"billingType": "must be PIX or BOLETO"}
	}
	return nil
}

func (r *CreateInvoiceRequest) ValidateAdmin() map[string]string {
	return topUpAmountError(r.AmountBRL)
}
