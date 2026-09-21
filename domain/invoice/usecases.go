package invoice

import "time"

type CreateInvoiceInput struct {
	WorkspaceID      string
	UserID           string
	Purpose          Purpose
	PlanDefinitionID string
	AmountBRL        float64
	CreditableBRL    float64
	IdempotencyKey   string
	BillingType      string
	BillingCycle     string
	Description      string
	DueDate          *time.Time
	LineItems        []InvoiceLineItem

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
