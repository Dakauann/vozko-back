package insurance

import "context"

type InsuranceQuoteRequest struct {
	UserID     string
	PolicyType PolicyType
	Details    map[string]interface{}
	Providers  []InsuranceProvider
}

type InsuranceQuoteResponse struct {
	Quotation *Quotation `json:"quotation"`
}

type QuoteInsuranceUseCase interface {
	Execute(ctx context.Context, req InsuranceQuoteRequest) (InsuranceQuoteResponse, error)
}

type DescribeQuoteRequirementsUseCase interface {
	Execute(ctx context.Context, policyType PolicyType, providers []InsuranceProvider) (RequirementSet, error)
}

type ListUserQuotationsUseCase interface {
	Execute(ctx context.Context, userID string) ([]Quotation, error)
}

type GetQuotationUseCase interface {
	Execute(ctx context.Context, userID, quotationID string) (*Quotation, error)
}

type ListPoliciesUseCase interface {
	Execute(ctx context.Context) ([]PolicySummary, error)
}
