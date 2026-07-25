package affiliate_usecase

import (
	"context"
	"strings"
	"time"

	"vozko/domain/affiliate"
	"vozko/domain/user"
)

type updateMyAffiliateUseCase struct {
	repo            affiliate.Repository
	userRepo        user.UserRepository
	walletValidator affiliate.WalletValidator
}

func NewUpdateMyAffiliateUseCase(
	repo affiliate.Repository,
	userRepo user.UserRepository,
	walletValidator affiliate.WalletValidator,
) affiliate.UpdateMyAffiliateUseCase {
	return &updateMyAffiliateUseCase{repo: repo, userRepo: userRepo, walletValidator: walletValidator}
}

func (uc *updateMyAffiliateUseCase) Execute(ctx context.Context, input affiliate.UpdateAffiliateProfileInput) (*affiliate.Affiliate, error) {
	if strings.TrimSpace(input.UserID) == "" {
		return nil, affiliate.ErrUnauthorized
	}
	aff, err := uc.repo.GetByUserID(ctx, input.UserID)
	if err != nil {
		return nil, err
	}

	previousWallet := aff.AsaasWalletID
	if input.BrandName != nil {
		aff.BrandName = strings.TrimSpace(*input.BrandName)
	}
	if input.BrandLogoURL != nil {
		aff.BrandLogoURL = strings.TrimSpace(*input.BrandLogoURL)
	}
	if input.AsaasWalletID != nil {
		aff.AsaasWalletID = strings.TrimSpace(*input.AsaasWalletID)
	}
	aff.UpdatedAt = time.Now().UTC()

	if err := aff.Validate(); err != nil {
		return nil, err
	}

	if uc.walletValidator != nil && aff.AsaasWalletID != previousWallet {
		var u *user.User
		if uc.userRepo != nil {
			u, _ = uc.userRepo.FindByID(input.UserID)
		}
		name, doc := resolveCustomerIdentity(u)
		if err := uc.walletValidator.ValidateWallet(ctx, affiliate.WalletValidationInput{
			WalletID:     aff.AsaasWalletID,
			CustomerName: name,
			CustomerDoc:  doc,
		}); err != nil {
			return nil, err
		}
	}

	if err := uc.repo.Update(ctx, aff); err != nil {
		return nil, err
	}
	return aff, nil
}

type adminUpdateAffiliateUseCase struct {
	repo affiliate.Repository
}

func NewAdminUpdateAffiliateUseCase(repo affiliate.Repository) affiliate.AdminUpdateAffiliateUseCase {
	return &adminUpdateAffiliateUseCase{repo: repo}
}

func (uc *adminUpdateAffiliateUseCase) Execute(ctx context.Context, input affiliate.AdminUpdateAffiliateInput) (*affiliate.Affiliate, error) {
	aff, err := uc.repo.GetByID(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	if input.CommissionPct != nil {
		if err := affiliate.ValidateCommissionPct(*input.CommissionPct); err != nil {
			return nil, err
		}
		aff.CommissionPct = *input.CommissionPct
	}
	if input.Active != nil {
		aff.Active = *input.Active
	}
	if input.Tier != nil {
		if !input.Tier.IsValid() {
			return nil, affiliate.ErrInvalidTier
		}
		aff.Tier = *input.Tier
	}
	aff.UpdatedAt = time.Now().UTC()

	if err := uc.repo.Update(ctx, aff); err != nil {
		return nil, err
	}
	return aff, nil
}

type adminListAffiliatesUseCase struct {
	repo affiliate.Repository
}

func NewAdminListAffiliatesUseCase(repo affiliate.Repository) affiliate.AdminListAffiliatesUseCase {
	return &adminListAffiliatesUseCase{repo: repo}
}

func (uc *adminListAffiliatesUseCase) Execute(ctx context.Context, page, pageSize int) ([]affiliate.Affiliate, int64, error) {
	return uc.repo.List(ctx, page, pageSize)
}

type adminGetAffiliateUseCase struct {
	repo  affiliate.Repository
	stats affiliate.GetAffiliateStatsUseCase
}

func NewAdminGetAffiliateUseCase(repo affiliate.Repository, stats affiliate.GetAffiliateStatsUseCase) affiliate.AdminGetAffiliateUseCase {
	return &adminGetAffiliateUseCase{repo: repo, stats: stats}
}

func (uc *adminGetAffiliateUseCase) Execute(ctx context.Context, id string) (*affiliate.AffiliateProfileWithStats, error) {
	aff, err := uc.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	s, err := uc.stats.Execute(ctx, aff.ID)
	if err != nil {
		return nil, err
	}
	return &affiliate.AffiliateProfileWithStats{Affiliate: aff, Stats: s}, nil
}
