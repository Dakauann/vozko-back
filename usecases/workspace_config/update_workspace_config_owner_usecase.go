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
	// An unknown mode or an out-of-range window is normalized, not rejected —
	// the same treatment the auto-close hours above get. The response echoes
	// what was stored, so the UI shows the value that will actually be used
	// rather than the one that was typed.
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

	existing.UpdatedBy = callerID

	if err := uc.repo.Upsert(ctx, existing); err != nil {
		return nil, err
	}

	return existing, nil
}
