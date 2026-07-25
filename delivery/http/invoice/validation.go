package invoice

import "strings"

func (r *CreateInvoiceRequest) Validate() map[string]string {
	if r.AmountBRL <= 0 {
		return map[string]string{"amountBrl": "must be positive"}
	}
	if r.AmountBRL < 5.0 {
		return map[string]string{"amountBrl": "minimum amount is R$ 5.00"}
	}
	if r.AmountBRL > 50000.0 {
		return map[string]string{"amountBrl": "maximum amount is R$ 50,000.00"}
	}
	billingType := strings.ToUpper(strings.TrimSpace(r.BillingType))
	if billingType != "PIX" && billingType != "BOLETO" {
		return map[string]string{"billingType": "must be PIX or BOLETO"}
	}
	return nil
}

func (r *CreateInvoiceRequest) ValidateAdmin() map[string]string {
	if r.AmountBRL < 5.0 {
		return map[string]string{"amountBrl": "minimum amount is R$ 5.00"}
	}
	if r.AmountBRL > 50000.0 {
		return map[string]string{"amountBrl": "maximum amount is R$ 50,000.00"}
	}
	return nil
}
