package invoice_usecase

import "vozko/domain/invoice"

type getInvoiceUseCase struct {
	repo invoice.Repository
}

func NewGetInvoiceUseCase(repo invoice.Repository) invoice.GetInvoiceUseCase {
	return &getInvoiceUseCase{repo: repo}
}

func (uc *getInvoiceUseCase) Execute(id string) (*invoice.Invoice, error) {
	return uc.repo.GetByID(id)
}
