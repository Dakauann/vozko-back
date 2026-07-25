package insurance

import "context"

type InsuranceRepository interface {
	SaveQuotation(ctx context.Context, quotation *Quotation) error
	GetQuotationByID(ctx context.Context, quotationID string) (*Quotation, error)
	ListQuotationsByUser(ctx context.Context, userID string) ([]Quotation, error)
}
