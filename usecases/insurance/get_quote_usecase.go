package insurance_usecase

import (
	"context"
	"errors"

	"vozko/domain/insurance"

	"gorm.io/gorm"
)

type getQuotationUseCase struct {
	repo insurance.InsuranceRepository
}

func NewGetQuotationUseCase(repo insurance.InsuranceRepository) insurance.GetQuotationUseCase {
	return &getQuotationUseCase{repo: repo}
}

func (uc *getQuotationUseCase) Execute(ctx context.Context, userID, quotationID string) (*insurance.Quotation, error) {
	if uc.repo == nil {
		return nil, insurance.ErrRepositoryNotConfigured
	}

	quotation, err := uc.repo.GetQuotationByID(ctx, quotationID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, insurance.ErrQuotationNotFound
		}
		return nil, err
	}

	if quotation.UserID != userID {
		return nil, insurance.ErrQuotationNotFound
	}

	return quotation, nil
}
