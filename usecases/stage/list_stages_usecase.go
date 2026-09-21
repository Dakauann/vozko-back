package stage_usecase

import (
	"strings"

	"vozko/domain/stage"
)

type ListStagesUseCase struct {
	repo stage.Repository
}

func NewListStagesUseCase(repo stage.Repository) stage.ListStagesUseCase {
	return &ListStagesUseCase{repo: repo}
}

func (uc *ListStagesUseCase) Execute(workspaceID, campaignID, campaignType, pipelineID string) ([]*stage.Stage, error) {
	if pipelineID = strings.TrimSpace(pipelineID); pipelineID != "" {
		return uc.repo.ListByPipeline(workspaceID, pipelineID)
	}
	if err := uc.repo.EnsureDefaultStagesForCampaign(workspaceID, campaignID, campaignType); err != nil {
		return nil, err
	}
	return uc.repo.ListByCampaign(workspaceID, campaignID, campaignType)
}
