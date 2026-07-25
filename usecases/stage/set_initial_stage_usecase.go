package stage_usecase

import "vozko/domain/stage"

type SetInitialStageUseCase struct {
	repo stage.Repository
}

func NewSetInitialStageUseCase(repo stage.Repository) stage.SetInitialStageUseCase {
	return &SetInitialStageUseCase{repo: repo}
}

func (uc *SetInitialStageUseCase) Execute(workspaceID, StageID string) (*stage.Stage, error) {
	existing, err := uc.repo.FindByID(StageID)
	if err != nil {
		return nil, err
	}
	if existing.WorkspaceID != workspaceID {
		return nil, stage.ErrUnauthorized
	}

	if err := uc.repo.SetInitialStage(workspaceID, existing.CampaignID, existing.CampaignType, StageID); err != nil {
		return nil, err
	}

	return uc.repo.FindByID(StageID)
}
