package affiliate_usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/affiliate"
	"vozko/domain/config"
	"vozko/domain/user"
)

type registerAffiliateUseCase struct {
	repo            affiliate.Repository
	userRepo        user.UserRepository
	systemConfig    config.SystemConfigRepository
	walletValidator affiliate.WalletValidator
}

func NewRegisterAffiliateUseCase(
	repo affiliate.Repository,
	userRepo user.UserRepository,
	systemConfig config.SystemConfigRepository,
	walletValidator affiliate.WalletValidator,
) affiliate.RegisterAffiliateUseCase {
	return &registerAffiliateUseCase{
		repo:            repo,
		userRepo:        userRepo,
		systemConfig:    systemConfig,
		walletValidator: walletValidator,
	}
}

func (uc *registerAffiliateUseCase) Execute(ctx context.Context, input affiliate.RegisterAffiliateInput) (*affiliate.Affiliate, error) {
	userID := strings.TrimSpace(input.UserID)
	if userID == "" {
		return nil, affiliate.ErrUnauthorized
	}

	var userRecord *user.User
	if uc.userRepo != nil {
		u, err := uc.userRepo.FindByID(userID)
		if err != nil || u == nil {
			return nil, affiliate.ErrUnauthorized
		}
		userRecord = u
	}

	existing, err := uc.repo.GetByUserID(ctx, userID)
	if err != nil && !errors.Is(err, affiliate.ErrAffiliateNotFound) {
		return nil, err
	}
	if existing != nil {
		return nil, affiliate.ErrAffiliateAlreadyExists
	}

	code := affiliate.NormalizeCode(input.Code)
	if code == "" {
		code = affiliate.GenerateCodeFromBrand(input.BrandName)
	}
	if code == "" {
		return nil, affiliate.ErrInvalidCode
	}

	finalCode, err := uc.findFreeCode(ctx, code)
	if err != nil {
		return nil, err
	}

	pct := affiliate.DefaultCommissionPct
	if uc.systemConfig != nil {
		if cfg, err := uc.systemConfig.Get(ctx); err == nil && cfg != nil {
			pct = cfg.GetAffiliateCommissionPct()
		}
	}

	aff := &affiliate.Affiliate{
		ID:            uuid.New().String(),
		UserID:        userID,
		Code:          finalCode,
		BrandName:     strings.TrimSpace(input.BrandName),
		BrandLogoURL:  strings.TrimSpace(input.BrandLogoURL),
		AsaasWalletID: strings.TrimSpace(input.AsaasWalletID),
		CommissionPct: pct,
		Active:        true,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}

	if err := aff.Validate(); err != nil {
		return nil, err
	}

	if uc.walletValidator != nil {
		name, doc := resolveCustomerIdentity(userRecord)
		if err := uc.walletValidator.ValidateWallet(ctx, affiliate.WalletValidationInput{
			WalletID:     aff.AsaasWalletID,
			CustomerName: name,
			CustomerDoc:  doc,
		}); err != nil {
			return nil, err
		}
	}

	if err := uc.repo.Create(ctx, aff); err != nil {
		return nil, fmt.Errorf("failed to create affiliate: %w", err)
	}
	return aff, nil
}

func resolveCustomerIdentity(u *user.User) (name string, doc string) {
	if u == nil {
		return "", ""
	}
	name = strings.TrimSpace(u.Username)
	if u.CustomerType == user.CustomerTypeCompany && strings.TrimSpace(u.CNPJ) != "" {
		return name, strings.TrimSpace(u.CNPJ)
	}
	if strings.TrimSpace(u.CPF) != "" {
		return name, strings.TrimSpace(u.CPF)
	}
	if strings.TrimSpace(u.CNPJ) != "" {
		return name, strings.TrimSpace(u.CNPJ)
	}
	return name, ""
}

func (uc *registerAffiliateUseCase) findFreeCode(ctx context.Context, base string) (string, error) {
	candidate := base
	for i := 0; i < 10; i++ {
		existing, err := uc.repo.GetByCode(ctx, candidate)
		if err != nil && !errors.Is(err, affiliate.ErrAffiliateNotFound) {
			return "", err
		}
		if existing == nil {
			return candidate, nil
		}
		suffix := fmt.Sprintf("-%d", i+2)
		candidate = affiliate.NormalizeCode(base + suffix)
		if candidate == "" {
			return "", affiliate.ErrInvalidCode
		}
	}
	return "", affiliate.ErrCodeAlreadyTaken
}
