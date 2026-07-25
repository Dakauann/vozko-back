package insurance_usecase

import (
	"context"
	"strings"

	"vozko/domain/insurance"
)

func NewListUserQuotationsUseCase(repo insurance.InsuranceRepository) insurance.ListUserQuotationsUseCase {
	return &listUserQuotationsUseCase{repo: repo}
}

type listUserQuotationsUseCase struct {
	repo insurance.InsuranceRepository
}

func (uc *listUserQuotationsUseCase) Execute(ctx context.Context, userID string) ([]insurance.Quotation, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, insurance.ErrInvalidQuoteRequest
	}

	if uc.repo == nil {
		return nil, insurance.ErrRepositoryNotConfigured
	}

	quotations, err := uc.repo.ListQuotationsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	return quotations, nil
}
