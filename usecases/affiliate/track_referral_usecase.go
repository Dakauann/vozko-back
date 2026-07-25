package affiliate_usecase

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/affiliate"
)

type validateReferralCodeUseCase struct {
	repo affiliate.Repository
}

func NewValidateReferralCodeUseCase(repo affiliate.Repository) affiliate.ValidateReferralCodeUseCase {
	return &validateReferralCodeUseCase{repo: repo}
}

func (uc *validateReferralCodeUseCase) Execute(ctx context.Context, code string) (*affiliate.ReferralValidationResult, error) {
	normalized := affiliate.NormalizeCode(code)
	if normalized == "" {
		return &affiliate.ReferralValidationResult{Valid: false}, nil
	}
	aff, err := uc.repo.GetByCode(ctx, normalized)
	if err != nil {
		if errors.Is(err, affiliate.ErrAffiliateNotFound) {
			return &affiliate.ReferralValidationResult{Valid: false}, nil
		}
		return nil, err
	}
	if !aff.Active {
		return &affiliate.ReferralValidationResult{Valid: false}, nil
	}
	return &affiliate.ReferralValidationResult{
		Valid:        true,
		Code:         aff.Code,
		BrandName:    aff.BrandName,
		BrandLogoURL: aff.BrandLogoURL,
	}, nil
}

type trackReferralUseCase struct {
	repo affiliate.Repository
}

func NewTrackReferralUseCase(repo affiliate.Repository) affiliate.TrackReferralUseCase {
	return &trackReferralUseCase{repo: repo}
}

func (uc *trackReferralUseCase) Execute(ctx context.Context, input affiliate.TrackReferralInput) (*affiliate.Referral, error) {
	workspaceID := strings.TrimSpace(input.WorkspaceID)
	if workspaceID == "" {
		return nil, affiliate.ErrInvalidReferralCode
	}
	normalized := affiliate.NormalizeCode(input.Code)
	if normalized == "" {
		return nil, affiliate.ErrInvalidReferralCode
	}
	aff, err := uc.repo.GetByCode(ctx, normalized)
	if err != nil {
		if errors.Is(err, affiliate.ErrAffiliateNotFound) {
			return nil, affiliate.ErrInvalidReferralCode
		}
		return nil, err
	}
	if !aff.Active {
		return nil, affiliate.ErrInvalidReferralCode
	}

	if strings.TrimSpace(input.WorkspaceOwnerUserID) != "" && input.WorkspaceOwnerUserID == aff.UserID {
		return nil, affiliate.ErrSelfReferral
	}

	existing, err := uc.repo.GetReferralByWorkspaceID(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, affiliate.ErrWorkspaceAlreadyReferred
	}

	ref := &affiliate.Referral{
		ID:          uuid.New().String(),
		AffiliateID: aff.ID,
		WorkspaceID: workspaceID,
		ReferredAt:  time.Now().UTC(),
	}
	if err := uc.repo.CreateReferral(ctx, ref); err != nil {
		return nil, err
	}
	return ref, nil
}
