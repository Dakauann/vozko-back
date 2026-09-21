package invoice

type Repository interface {
	Create(invoice *Invoice) error
	GetByID(id string) (*Invoice, error)
	GetByExternalID(externalID string) (*Invoice, error)
	GetByIdempotencyKey(key string) (*Invoice, error)

	ListUnpaidByPurpose(purpose Purpose, afterID string, limit int) ([]Invoice, error)
	UpdateStatus(id string, status Status) error
	MarkPaid(id string, amountUSD int64) (bool, error)
	ListByWorkspace(workspaceID string, page, pageSize int) ([]Invoice, int64, error)
}
