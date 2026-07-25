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
	repo      wsc.Repository
	wsRepo    workspaceOwnerFetcher
	holdMusic wsc.HoldMusicTrackValidator
}

// NewUpdateWorkspaceConfigOwnerUseCase builds the owner config update. holdMusic
// may be nil (e.g. minimal test rigs): hold music selection is then rejected
// rather than stored unvalidated.
func NewUpdateWorkspaceConfigOwnerUseCase(repo wsc.Repository, wsRepo workspaceOwnerFetcher, holdMusic wsc.HoldMusicTrackValidator) wsc.UpdateWorkspaceConfigOwnerUseCase {
	return &updateWorkspaceConfigOwnerUseCase{repo: repo, wsRepo: wsRepo, holdMusic: holdMusic}
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
	if input.HoldMusicTrack != nil {
		track := *input.HoldMusicTrack
		if track != "" {
			if uc.holdMusic == nil {
				return nil, wsc.ErrInvalidHoldMusicTrack
			}
			if err := uc.holdMusic.ValidateHoldMusicTrack(ctx, workspaceID, track); err != nil {
				return nil, err
			}
		}
		existing.HoldMusicTrack = track
	}

	if input.QueueEnabled != nil {
		existing.QueueEnabled = *input.QueueEnabled
	}
	if input.QueueMaxWaitSeconds != nil {
		v := *input.QueueMaxWaitSeconds
		if v < 0 {
			v = 0
		}
		existing.QueueMaxWaitSeconds = v
	}
	if input.QueueMaxLength != nil {
		v := *input.QueueMaxLength
		if v < 0 {
			v = 0
		}
		existing.QueueMaxLength = v
	}
	if input.QueueOverflow != nil {
		if !wsc.ValidQueueOverflow(*input.QueueOverflow) {
			return nil, wsc.ErrInvalidQueueOverflow
		}
		existing.QueueOverflow = *input.QueueOverflow
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

	existing.UpdatedBy = callerID

	if err := uc.repo.Upsert(ctx, existing); err != nil {
		return nil, err
	}

	return existing, nil
}
