package paymentsplit

import (
	"time"

	paymentdomain "vozko/domain/payment"
)

type paymentSplitRequest struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Provider    string   `json:"provider"`
	WalletID    string   `json:"walletId"`
	Percentage  *float64 `json:"percentageSplit,omitempty"`
	FixedAmount *float64 `json:"fixedAmountSplit,omitempty"`
}

type PaymentSplitResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Type        string    `json:"type"`
	Provider    string    `json:"provider"`
	WalletID    string    `json:"walletId"`
	Percentage  float64   `json:"percentageSplit,omitempty"`
	FixedAmount float64   `json:"fixedAmount,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

func toPaymentSplitResponse(s *paymentdomain.PaymentSplit) *PaymentSplitResponse {
	if s == nil {
		return nil
	}
	return &PaymentSplitResponse{
		ID:          s.ID,
		Name:        s.Name,
		Type:        string(s.Type),
		Provider:    string(s.Provider),
		WalletID:    s.WalletID,
		Percentage:  s.Percentage,
		FixedAmount: s.FixedAmount,
		CreatedAt:   s.CreatedAt,
		UpdatedAt:   s.UpdatedAt,
	}
}

func toPaymentSplitListResponse(items []paymentdomain.PaymentSplit) []*PaymentSplitResponse {
	if items == nil {
		return nil
	}
	out := make([]*PaymentSplitResponse, len(items))
	for i := range items {
		out[i] = toPaymentSplitResponse(&items[i])
	}
	return out
}

func toPaymentSplitSuppliersResponse(items []*paymentdomain.PaymentSplit) []*PaymentSplitResponse {
	if items == nil {
		return nil
	}
	out := make([]*PaymentSplitResponse, len(items))
	for i, it := range items {
		out[i] = toPaymentSplitResponse(it)
	}
	return out
}
