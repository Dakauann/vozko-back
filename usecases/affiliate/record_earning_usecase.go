package affiliate_usecase

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/affiliate"
)

type recordEarningUseCase struct {
	repo         affiliate.Repository
	rateProvider affiliate.ExchangeRateProvider
}

func NewRecordEarningUseCase(repo affiliate.Repository, rateProvider affiliate.ExchangeRateProvider) affiliate.RecordEarningUseCase {
	return &recordEarningUseCase{repo: repo, rateProvider: rateProvider}
}

func (uc *recordEarningUseCase) Execute(ctx context.Context, input affiliate.RecordEarningInput) (*affiliate.Earning, error) {
	workspaceID := strings.TrimSpace(input.WorkspaceID)
	invoiceID := strings.TrimSpace(input.InvoiceID)
	if workspaceID == "" || invoiceID == "" || input.AmountUSDMicros <= 0 {
		return nil, nil
	}

	ref, err := uc.repo.GetReferralByWorkspaceID(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if ref == nil {
		return nil, nil
	}

	aff, err := uc.repo.GetByID(ctx, ref.AffiliateID)
	if err != nil {
		if errors.Is(err, affiliate.ErrAffiliateNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if !aff.Active {
		return nil, nil
	}

	if existing, err := uc.repo.GetEarningByInvoiceID(ctx, invoiceID); err != nil {
		return nil, err
	} else if existing != nil {
		return existing, nil
	}

	rateMicros := input.ExchangeRateMicros
	if rateMicros <= 0 {
		if uc.rateProvider == nil {
			return nil, affiliate.ErrExchangeRateUnavailable
		}
		r, err := uc.rateProvider.CurrentRateMicros(ctx)
		if err != nil {
			return nil, err
		}
		if r <= 0 {
			return nil, affiliate.ErrExchangeRateUnavailable
		}
		rateMicros = r
	}

	commissionMicros := int64(math.Round(
		float64(input.AmountUSDMicros) * aff.CommissionPct,
	))
	if commissionMicros <= 0 {
		return nil, nil
	}

	earning := &affiliate.Earning{
		ID:                 uuid.New().String(),
		AffiliateID:        aff.ID,
		InvoiceID:          invoiceID,
		WorkspaceID:        workspaceID,
		AmountMicros:       commissionMicros,
		ExchangeRateMicros: rateMicros,
		Purpose:            strings.ToUpper(strings.TrimSpace(input.Purpose)),
		Status:             "paid",
		CreatedAt:          time.Now().UTC(),
	}
	if err := uc.repo.CreateEarning(ctx, earning); err != nil {
		return nil, err
	}
	return earning, nil
}
