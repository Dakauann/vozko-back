package invoice

type Repository interface {
	Create(invoice *Invoice) error
	GetByID(id string) (*Invoice, error)
	GetByExternalID(externalID string) (*Invoice, error)
	// GetByIdempotencyKey returns the invoice previously created with this key, or (nil, nil) when
	// none exists. Used to make repeated invoice creation (e.g. a monthly emit re-run) idempotent.
	GetByIdempotencyKey(key string) (*Invoice, error)

	// ListUnpaidByPurpose returns invoices of the given purpose still awaiting payment (PENDING or
	// OVERDUE), ordered by id and starting strictly after afterID (empty for the first page), up to
	// limit. Keyset pagination lets the cancel sweep stream through every unpaid monthly charge.
	ListUnpaidByPurpose(purpose Purpose, afterID string, limit int) ([]Invoice, error)
	UpdateStatus(id string, status Status) error
	MarkPaid(id string, amountUSD int64) (bool, error)
	ListByWorkspace(workspaceID string, page, pageSize int) ([]Invoice, int64, error)
}
