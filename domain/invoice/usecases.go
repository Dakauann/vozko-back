package invoice

import "time"

type CreateInvoiceInput struct {
	WorkspaceID      string
	UserID           string
	Purpose          Purpose
	PlanDefinitionID string
	AmountBRL        float64
	// CreditableBRL is the portion of AmountBRL that should become saldo when paid. Leave zero to
	// credit the full amount (TOP_UP, SUBSCRIPTION). For MONTHLY_BILLING the emitter sets it to the
	// plan portion only, so channel-license repasse is never credited as saldo.
	CreditableBRL float64
	// IdempotencyKey makes creation idempotent. When set and an invoice already exists for it,
	// create_invoice returns that invoice without charging Asaas again. Empty disables the check.
	IdempotencyKey string
	BillingType    string
	BillingCycle   string
	Description    string
	// DueDate sets the payment due date (the billing anchor for MONTHLY_BILLING). Nil defaults to a
	// short grace from now (ad-hoc invoices).
	DueDate *time.Time
	// LineItems is the customer-facing breakdown persisted on the invoice (price only).
	LineItems []InvoiceLineItem

	ReferralCode string
}

type CreateInvoiceOutput struct {
	Invoice *Invoice `json:"invoice"`
}

type CreateInvoiceUseCase interface {
	Execute(input CreateInvoiceInput) (*CreateInvoiceOutput, error)
}

type ListInvoicesUseCase interface {
	Execute(workspaceID string, page, pageSize int) ([]Invoice, int64, error)
}

type GetInvoiceUseCase interface {
	Execute(id string) (*Invoice, error)
}
