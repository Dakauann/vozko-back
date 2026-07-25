package stage_usecase

import (
	"strings"

	"vozko/domain/stage"
)

type UpdateStageUseCase struct {
	repo stage.Repository
}

func NewUpdateStageUseCase(repo stage.Repository) stage.UpdateStageUseCase {
	return &UpdateStageUseCase{repo: repo}
}

func (uc *UpdateStageUseCase) Execute(workspaceID, StageID string, input stage.UpdateStageInput) (*stage.Stage, error) {
	existing, err := uc.repo.FindByID(StageID)
	if err != nil {
		return nil, err
	}
	if existing.WorkspaceID != workspaceID {
		return nil, stage.ErrUnauthorized
	}

	if existing.IsDefault {
		if input.Name != nil || input.Color != nil {
			return nil, stage.ErrTagDefaultUpdate
		}
	}

	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			return nil, stage.ErrTagNameRequired
		}
		nameExists, err := uc.repo.NameExistsInCampaign(workspaceID, existing.CampaignID, existing.CampaignType, name, &StageID)
		if err != nil {
			return nil, err
		}
		if nameExists {
			return nil, stage.ErrTagNameExists
		}
		existing.Name = strings.ToLower(name)
	}

	if input.Color != nil {
		existing.Color = strings.TrimSpace(*input.Color)
	}

	if input.Description != nil {
		existing.Description = strings.TrimSpace(*input.Description)
	}

	if err := uc.repo.Update(existing); err != nil {
		return nil, err
	}

	return uc.repo.FindByID(StageID)
}
