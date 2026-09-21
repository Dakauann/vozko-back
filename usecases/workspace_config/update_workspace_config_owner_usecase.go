package workspace_config_usecase

import (
	"context"

	"vozko/domain/workspace"
	wsc "vozko/domain/workspace_config"
)

type workspaceOwnerFetcher interface {
	GetWorkspaceByID(id string) (*workspace.Workspace, error)
}

type updateWorkspaceConfigOwnerUseCase struct {
	repo   wsc.Repository
	wsRepo workspaceOwnerFetcher
}

func NewUpdateWorkspaceConfigOwnerUseCase(repo wsc.Repository, wsRepo workspaceOwnerFetcher) wsc.UpdateWorkspaceConfigOwnerUseCase {
	return &updateWorkspaceConfigOwnerUseCase{repo: repo, wsRepo: wsRepo}
}

func (uc *updateWorkspaceConfigOwnerUseCase) Execute(ctx context.Context, workspaceID, callerID, callerRole string, input wsc.UpdateWorkspaceConfigOwnerInput) (*wsc.WorkspaceConfig, error) {
	if callerRole != "admin" {
		ws, err := uc.wsRepo.GetWorkspaceByID(workspaceID)
		if err != nil || ws == nil {
			return nil, wsc.ErrForbidden
		}
		if ws.OwnerID != callerID {
			return nil, wsc.ErrForbidden
		}
	}

	existing, err := uc.repo.GetByWorkspaceID(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	if input.SkipAdminAssignment != nil {
		existing.SkipAdminAssignment = *input.SkipAdminAssignment
	}
	if input.AutoCloseEnabled != nil {
		existing.AutoCloseEnabled = *input.AutoCloseEnabled
	}
	if input.AutoCloseIdleAfterHours != nil {
		existing.AutoCloseIdleAfterHours = wsc.ClampAutoCloseIdleHours(*input.AutoCloseIdleAfterHours)
	}
	if input.AutoCloseMaxAgeEnabled != nil {
		existing.AutoCloseMaxAgeEnabled = *input.AutoCloseMaxAgeEnabled
	}
	if input.AutoCloseMaxAgeAfterHours != nil {
		existing.AutoCloseMaxAgeAfterHours = wsc.ClampAutoCloseMaxAgeHours(*input.AutoCloseMaxAgeAfterHours)
	}
	if input.RouletteMode != nil {
		existing.RouletteMode = wsc.NormalizeRouletteMode(*input.RouletteMode)
	}
	if input.RouletteLastSeenWindowHours != nil {
		existing.RouletteLastSeenWindowHours = wsc.ClampRouletteLastSeenWindowHours(*input.RouletteLastSeenWindowHours)
	}
	if input.RouletteRescueEnabled != nil {
		existing.RouletteRescueEnabled = *input.RouletteRescueEnabled
	}
	if input.RouletteRescueAfterMinutes != nil {
		existing.RouletteRescueAfterMinutes = wsc.ClampRouletteRescueMinutes(*input.RouletteRescueAfterMinutes)
	}

	switch {
	case input.ClearWorkingHours:
		existing.WorkingHours = nil
	case input.WorkingHours != nil:
		if err := input.WorkingHours.Validate(); err != nil {
			return nil, err
		}
		existing.WorkingHours = input.WorkingHours.Normalized()
	}

	existing.UpdatedBy = callerID

	if err := uc.repo.Upsert(ctx, existing); err != nil {
		return nil, err
	}

	return existing, nil
}
