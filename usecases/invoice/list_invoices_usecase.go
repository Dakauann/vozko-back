package invoice_usecase

import "vozko/domain/invoice"

type listInvoicesUseCase struct {
	repo invoice.Repository
}

func NewListInvoicesUseCase(repo invoice.Repository) invoice.ListInvoicesUseCase {
	return &listInvoicesUseCase{repo: repo}
}

func (uc *listInvoicesUseCase) Execute(workspaceID string, page, pageSize int) ([]invoice.Invoice, int64, error) {
	return uc.repo.ListByWorkspace(workspaceID, page, pageSize)
}
