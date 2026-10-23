package invoice

import (
	"time"

	"vozko/domain/payment"
)

type ChargeAttachment struct {
	ExternalID  string
	PixQrCode   *string
	PixCopy     *string
	BankSlipUrl *string
	InvoiceUrl  *string
}

type Repository interface {
	Reserve(invoice *Invoice) (*Invoice, bool, error)
	AttachCharge(id string, charge ChargeAttachment) error
	GetByID(id string) (*Invoice, error)
	GetByExternalID(provider payment.Provider, externalID string) (*Invoice, error)

	ListUnpaidByPurpose(purpose Purpose, afterID string, limit int) ([]Invoice, error)
	UpdateStatus(id string, status Status) error
	MarkPaid(id string, amountUSD int64) (bool, error)
	ListByWorkspace(workspaceID string, page, pageSize int) ([]Invoice, int64, error)

	CancelStaleReservations(createdBefore time.Time) (int64, error)
	OpenObligations(now time.Time) ([]payment.Obligations, error)
}
