package invoice

import (
	"time"

	invoicedomain "vozko/domain/invoice"
)

type CreateInvoiceRequest struct {
	AmountBRL      float64 `json:"amountBrl" example:"50"`
	BillingType    string  `json:"billingType" example:"PIX"`
	Description    string  `json:"description" example:"Recarga de saldo"`
	IdempotencyKey string  `json:"idempotencyKey,omitempty" example:"5f0c6a3e-1d2b-4c5d-9e8f-0a1b2c3d4e5f"`
}

type InvoiceLineItemResponse struct {
	Kind       string  `json:"kind" example:"PLAN"`
	Label      string  `json:"label" example:"Plano Profissional"`
	AmountBRL  float64 `json:"amountBRL" example:"210"`
	Quantity   int     `json:"quantity,omitempty" example:"1"`
	Prorated   bool    `json:"prorated,omitempty" example:"false"`
	Creditable bool    `json:"creditable" example:"true"`
}

type InvoiceResponse struct {
	ID               string                    `json:"id" example:"inv_a1b2c3"`
	WorkspaceID      string                    `json:"workspaceId" example:"ws_a1b2c3"`
	UserID           string                    `json:"userId" example:"usr_a1b2c3"`
	Purpose          string                    `json:"purpose" example:"TOP_UP"`
	PlanDefinitionID *string                   `json:"planDefinitionId,omitempty" example:"plan_a1b2c3"`
	AmountBRL        float64                   `json:"amountBRL" example:"50"`
	AmountUSD        int64                     `json:"amountUSD" example:"9500000"`
	CreditableUSD    int64                     `json:"creditableUSD" example:"9500000"`
	ExchangeRate     float64                   `json:"exchangeRate" example:"5.26"`
	Status           string                    `json:"status" example:"PENDING"`
	BillingType      string                    `json:"billingType" example:"PIX"`
	BillingCycle     string                    `json:"billingCycle" example:"MONTHLY"`
	ExternalID       string                    `json:"externalId" example:"pay_a1b2c3"`
	IdempotencyKey   string                    `json:"idempotencyKey,omitempty" example:"monthly:ws_a1b2c3:2026-07-18"`
	PixQrCode        *string                   `json:"pixQrCode,omitempty" example:"iVBORw0KGgo"`
	PixCopy          *string                   `json:"pixCopy,omitempty" example:"00020126"`
	Payment          InvoicePaymentResponse    `json:"payment"`
	BankSlipUrl      *string                   `json:"bankSlipUrl,omitempty" example:"https://exemplo.com/boleto/a1b2c3"`
	InvoiceUrl       *string                   `json:"invoiceUrl,omitempty" example:"https://exemplo.com/fatura/a1b2c3"`
	PaidAt           *int64                    `json:"paidAt,omitempty" example:"1752883200"`
	Description      string                    `json:"description" example:"Recarga de saldo"`
	DueDate          *time.Time                `json:"dueDate,omitempty"`
	LineItems        []InvoiceLineItemResponse `json:"lineItems,omitempty"`
	CreatedAt        time.Time                 `json:"createdAt"`
	UpdatedAt        time.Time                 `json:"updatedAt"`
}

type InvoicePaymentResponse struct {
	State        string     `json:"state" enums:"OPEN,RENEWABLE,CLOSED" example:"OPEN"`
	CanRenew     bool       `json:"canRenew" example:"false"`
	ExpiresAt    *time.Time `json:"expiresAt,omitempty"`
	PayableUntil *time.Time `json:"payableUntil,omitempty"`
}

type InvoiceEnvelope struct {
	Invoice *InvoiceResponse `json:"invoice"`
}

type InvoiceListResponse struct {
	Invoices   []InvoiceResponse `json:"invoices"`
	Page       int               `json:"page" example:"1"`
	PageSize   int               `json:"pageSize" example:"20"`
	Total      int64             `json:"total" example:"137"`
	TotalPages int               `json:"totalPages" example:"7"`
}

func toInvoiceLineItems(items []invoicedomain.InvoiceLineItem) []InvoiceLineItemResponse {
	if len(items) == 0 {
		return nil
	}
	out := make([]InvoiceLineItemResponse, len(items))
	for i, it := range items {
		out[i] = InvoiceLineItemResponse{
			Kind:       string(it.Kind),
			Label:      it.Label,
			AmountBRL:  it.AmountBRL,
			Quantity:   it.Quantity,
			Prorated:   it.Prorated,
			Creditable: it.Creditable,
		}
	}
	return out
}

func toInvoiceResponse(i invoicedomain.Invoice, now time.Time) InvoiceResponse {
	view := i.PaymentAt(now)
	var bankSlipURL *string
	if i.AwaitingPayment() {
		bankSlipURL = i.BankSlipUrl
	}
	return InvoiceResponse{
		ID:               i.ID,
		WorkspaceID:      i.WorkspaceID,
		UserID:           i.UserID,
		Purpose:          string(i.Purpose),
		PlanDefinitionID: i.PlanDefinitionID,
		AmountBRL:        i.AmountBRL,
		AmountUSD:        i.AmountUSD,
		CreditableUSD:    i.CreditableUSD,
		ExchangeRate:     i.ExchangeRate,
		Status:           string(i.Status),
		BillingType:      i.BillingType,
		BillingCycle:     i.BillingCycle,
		ExternalID:       i.ExternalID,
		IdempotencyKey:   i.IdempotencyKey,
		PixQrCode:        view.PixQrCode,
		PixCopy:          view.PixCopy,
		Payment:          toInvoicePaymentResponse(view),
		BankSlipUrl:      bankSlipURL,
		InvoiceUrl:       i.InvoiceUrl,
		PaidAt:           i.PaidAt,
		Description:      i.Description,
		DueDate:          i.DueDate,
		LineItems:        toInvoiceLineItems(i.LineItems),
		CreatedAt:        i.CreatedAt,
		UpdatedAt:        i.UpdatedAt,
	}
}

func toInvoiceResponsePtr(i *invoicedomain.Invoice, now time.Time) *InvoiceResponse {
	if i == nil {
		return nil
	}
	resp := toInvoiceResponse(*i, now)
	return &resp
}

func toInvoiceResponses(items []invoicedomain.Invoice, now time.Time) []InvoiceResponse {
	if items == nil {
		return nil
	}
	out := make([]InvoiceResponse, len(items))
	for i := range items {
		out[i] = toInvoiceResponse(items[i], now)
	}
	return out
}

func toInvoicePaymentResponse(view invoicedomain.PaymentView) InvoicePaymentResponse {
	return InvoicePaymentResponse{
		State:        string(view.State),
		CanRenew:     view.CanRenew(),
		ExpiresAt:    view.ExpiresAt,
		PayableUntil: view.PayableUntil,
	}
}
