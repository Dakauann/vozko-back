package config_usecase

import (
	"context"
	"strings"
	"time"

	"vozko/domain/config"
	"vozko/domain/user"
)

type updateSystemConfigUseCase struct {
	repo config.SystemConfigRepository
}

func NewUpdateSystemConfigUseCase(repo config.SystemConfigRepository) config.UpdateSystemConfigUseCase {
	return &updateSystemConfigUseCase{repo: repo}
}

func (uc *updateSystemConfigUseCase) Execute(ctx context.Context, userID string, userRole string, input config.UpdateSystemConfigInput) (*config.SystemConfig, error) {
	if userRole != string(user.RoleAdmin) {
		return nil, config.ErrUnauthorized
	}

	existing, err := uc.repo.Get(ctx)
	if err != nil {
		return nil, err
	}

	if input.BaseSystemPrompt != nil {
		existing.BaseSystemPrompt = strings.TrimSpace(*input.BaseSystemPrompt)
	}

	if input.MaxConcurrentCalls != nil && *input.MaxConcurrentCalls > 0 {
		existing.MaxConcurrentCalls = *input.MaxConcurrentCalls
	}

	if input.WorkTimeEnabled != nil {
		existing.WorkTimeEnabled = *input.WorkTimeEnabled
	}

	workTimeStart := existing.WorkTimeStart
	workTimeEnd := existing.WorkTimeEnd

	if input.WorkTimeStart != nil {
		workTimeStart = strings.TrimSpace(*input.WorkTimeStart)
	}
	if input.WorkTimeEnd != nil {
		workTimeEnd = strings.TrimSpace(*input.WorkTimeEnd)
	}

	if input.WorkTimeStart != nil || input.WorkTimeEnd != nil {
		if err := config.ValidateWorkTime(workTimeStart, workTimeEnd); err != nil {
			return nil, err
		}
	}

	existing.WorkTimeStart = workTimeStart
	existing.WorkTimeEnd = workTimeEnd

	if input.AffiliateCommissionPct != nil {
		v := *input.AffiliateCommissionPct
		if v < 0 || v > config.MaxAffiliateCommissionPct {
			return nil, config.ErrInvalidAffiliateCommissionPct
		}
		existing.AffiliateCommissionPct = v
	}

	existing.UpdatedBy = userID
	existing.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	if err := uc.repo.Upsert(ctx, existing); err != nil {
		return nil, err
	}

	return existing, nil
}
