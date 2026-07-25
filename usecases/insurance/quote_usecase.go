package insurance_usecase

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/business_metrics"
	"vozko/domain/insurance"
)

type quoteUseCase struct {
	repo         insurance.InsuranceRepository
	providers    []insurance.QuoteProvider
	now          func() time.Time
	newID        func() string
	recordMetric business_metrics.RecordMetricUseCase
}

func NewQuoteUseCase(
	repo insurance.InsuranceRepository,
	providers []insurance.QuoteProvider,
	recordMetric business_metrics.RecordMetricUseCase,
) insurance.QuoteInsuranceUseCase {
	return &quoteUseCase{
		repo:         repo,
		providers:    cloneProviders(providers),
		now:          time.Now,
		newID:        func() string { return uuid.NewString() },
		recordMetric: recordMetric,
	}
}

func (uc *quoteUseCase) Execute(ctx context.Context, req insurance.InsuranceQuoteRequest) (insurance.InsuranceQuoteResponse, error) {
	if strings.TrimSpace(req.UserID) == "" {
		return insurance.InsuranceQuoteResponse{}, insurance.ErrInvalidQuoteRequest
	}
	if req.PolicyType == "" {
		return insurance.InsuranceQuoteResponse{}, insurance.ErrInvalidQuoteRequest
	}

	uc.recordQuoteRequest(req.UserID, string(req.PolicyType))

	details := req.Details
	if details == nil {
		details = map[string]interface{}{}
	}

	providers := filterProvidersByPolicy(uc.providers, req.PolicyType, req.Providers)
	if len(providers) == 0 {
		return insurance.InsuranceQuoteResponse{}, insurance.ErrNoProvidersForPolicy
	}

	requirements, err := insurance.AggregateRequiredFields(req.PolicyType, providers)
	if err != nil {
		return insurance.InsuranceQuoteResponse{}, err
	}

	violations := insurance.ValidateRequirements(details, requirements.Fields)
	if len(violations) > 0 {
		return insurance.InsuranceQuoteResponse{}, &insurance.MissingRequiredFieldsError{
			PolicyType: req.PolicyType,
			Violations: violations,
		}
	}

	quotationID := uc.newID()
	now := uc.now()

	quotes := make([]insurance.InsuranceQuote, 0, len(providers))
	for _, provider := range providers {
		providerReq := insurance.ProviderQuoteRequest{
			UserID:     req.UserID,
			PolicyType: req.PolicyType,
			Details:    details,
		}

		quote, err := provider.Quote(ctx, providerReq)
		if err != nil {
			return insurance.InsuranceQuoteResponse{}, fmt.Errorf("quote from provider %s: %w", provider.Provider(), err)
		}
		if quote == nil {
			continue
		}

		if strings.TrimSpace(quote.ID) == "" {
			quote.ID = uc.newID()
		}
		quote.QuotationID = quotationID
		if strings.TrimSpace(quote.UserID) == "" {
			quote.UserID = req.UserID
		}
		if quote.PolicyType == "" {
			quote.PolicyType = req.PolicyType
		}
		if quote.Provider == "" {
			quote.Provider = provider.Provider()
		}
		if quote.CreatedAt.IsZero() {
			quote.CreatedAt = now
		}
		if quote.Metadata == nil {
			quote.Metadata = make(map[string]interface{})
		}

		quotes = append(quotes, *quote)
	}

	sort.Slice(quotes, func(i, j int) bool {
		if quotes[i].Provider == quotes[j].Provider {
			return quotes[i].ID < quotes[j].ID
		}
		return quotes[i].Provider < quotes[j].Provider
	})

	quotation := &insurance.Quotation{
		ID:         quotationID,
		UserID:     req.UserID,
		PolicyType: req.PolicyType,
		Quotes:     quotes,
		CreatedAt:  now,
	}

	if uc.repo != nil {
		if err := uc.repo.SaveQuotation(ctx, quotation); err != nil {
			return insurance.InsuranceQuoteResponse{}, fmt.Errorf("save quotation: %w", err)
		}

		uc.recordQuoteCreated(quotation)
	}

	return insurance.InsuranceQuoteResponse{Quotation: quotation}, nil
}

func cloneProviders(providers []insurance.QuoteProvider) []insurance.QuoteProvider {
	if len(providers) == 0 {
		return nil
	}
	out := make([]insurance.QuoteProvider, 0, len(providers))
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		out = append(out, provider)
	}
	return out
}

func filterProvidersByPolicy(providers []insurance.QuoteProvider, policy insurance.PolicyType, allowed []insurance.InsuranceProvider) []insurance.QuoteProvider {
	if len(providers) == 0 {
		return nil
	}

	allowedSet := make(map[insurance.InsuranceProvider]struct{})
	for _, name := range allowed {
		allowedSet[name] = struct{}{}
	}

	seen := make(map[insurance.InsuranceProvider]struct{})
	var filtered []insurance.QuoteProvider

	for _, provider := range providers {
		if provider == nil {
			continue
		}

		name := provider.Provider()
		if _, ok := seen[name]; ok {
			continue
		}

		if len(allowedSet) > 0 {
			if _, ok := allowedSet[name]; !ok {
				continue
			}
		}

		if !provider.Supports(policy) {
			continue
		}

		filtered = append(filtered, provider)
		seen[name] = struct{}{}
	}

	return filtered
}

func (uc *quoteUseCase) recordQuoteRequest(userID, policyType string) {
	if uc.recordMetric == nil {
		return
	}

	err := uc.recordMetric.Execute(business_metrics.RecordMetricInput{
		EventType:  business_metrics.EventInsuranceQuoteRequested,
		EntityID:   userID,
		EntityType: business_metrics.EntityTypeUser,
		UserID:     &userID,
		Metadata: map[string]string{
			"policy_type": policyType,
		},
	})

	if err != nil {
		log.Printf("failed to record insurance quote requested metric: %v", err)
	}
}

func (uc *quoteUseCase) recordQuoteCreated(quotation *insurance.Quotation) {
	if uc.recordMetric == nil {
		return
	}

	for _, quote := range quotation.Quotes {
		err := uc.recordMetric.Execute(business_metrics.RecordMetricInput{
			EventType:  business_metrics.EventInsuranceQuoteCreated,
			EntityID:   quote.ID,
			EntityType: business_metrics.EntityTypeInsuranceQuote,
			UserID:     &quotation.UserID,
			Metadata: map[string]string{
				"quotation_id": quotation.ID,
				"policy_type":  string(quotation.PolicyType),
				"provider":     string(quote.Provider),
				"premium":      fmt.Sprintf("%.2f", quote.Premium),
			},
		})

		if err != nil {
			log.Printf("failed to record insurance quote created metric for quote %s: %v", quote.ID, err)
		}
	}
}
