package stage_usecase

import (
	"vozko/domain/stage"
)

type ReorderStagesUseCase struct {
	repo stage.Repository
}

func NewReorderStagesUseCase(repo stage.Repository) stage.ReorderStagesUseCase {
	return &ReorderStagesUseCase{repo: repo}
}

func (uc *ReorderStagesUseCase) Execute(workspaceID string, input stage.ReorderStagesInput) ([]*stage.Stage, error) {
	if len(input.StageIDs) == 0 {
		return uc.repo.ListByCampaign(workspaceID, input.CampaignID, input.CampaignType)
	}

	if err := uc.repo.ReorderStages(workspaceID, input.StageIDs); err != nil {
		return nil, err
	}

	return uc.repo.ListByCampaign(workspaceID, input.CampaignID, input.CampaignType)
}
