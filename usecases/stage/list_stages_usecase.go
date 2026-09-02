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

// Execute resolves the funnel in this order: an explicit pipelineID, then the
// campaign's funnel, then the workspace default.
//
// The explicit branch skips EnsureDefaultStagesForCampaign on purpose. That
// self-heal exists to seed the DEFAULT funnel on a fresh workspace; running it
// while a caller is asking about a specific pipeline would seed the default
// funnel as a side effect of reading a different one.
func (uc *ListStagesUseCase) Execute(workspaceID, campaignID, campaignType, pipelineID string) ([]*stage.Stage, error) {
	if pipelineID = strings.TrimSpace(pipelineID); pipelineID != "" {
		return uc.repo.ListByPipeline(workspaceID, pipelineID)
	}
	if err := uc.repo.EnsureDefaultStagesForCampaign(workspaceID, campaignID, campaignType); err != nil {
		return nil, err
	}
	return uc.repo.ListByCampaign(workspaceID, campaignID, campaignType)
}
