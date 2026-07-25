package insurance

import "context"

type ProviderQuoteRequest struct {
	UserID     string
	PolicyType PolicyType
	Details    map[string]interface{}
}

type QuoteProvider interface {
	Provider() InsuranceProvider
	Supports(policy PolicyType) bool
	RequiredFields(policy PolicyType) ([]RequiredField, error)
	Quote(ctx context.Context, req ProviderQuoteRequest) (*InsuranceQuote, error)
}
