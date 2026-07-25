package stage_usecase

import "vozko/domain/stage"

type ListStagesUseCase struct {
	repo stage.Repository
}

func NewListStagesUseCase(repo stage.Repository) stage.ListStagesUseCase {
	return &ListStagesUseCase{repo: repo}
}

func (uc *ListStagesUseCase) Execute(workspaceID, campaignID, campaignType string) ([]*stage.Stage, error) {
	if err := uc.repo.EnsureDefaultStagesForCampaign(workspaceID, campaignID, campaignType); err != nil {
		return nil, err
	}
	return uc.repo.ListByCampaign(workspaceID, campaignID, campaignType)
}
