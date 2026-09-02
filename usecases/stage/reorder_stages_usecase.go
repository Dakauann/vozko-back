package stage_usecase

import (
	"strings"

	"vozko/domain/stage"
)

type ReorderStagesUseCase struct {
	repo stage.Repository
}

func NewReorderStagesUseCase(repo stage.Repository) stage.ReorderStagesUseCase {
	return &ReorderStagesUseCase{repo: repo}
}

func (uc *ReorderStagesUseCase) Execute(workspaceID string, input stage.ReorderStagesInput) ([]*stage.Stage, error) {
	if len(input.StageIDs) > 0 {
		if err := uc.repo.ReorderStages(workspaceID, input.StageIDs); err != nil {
			return nil, err
		}
	}
	return uc.list(workspaceID, input)
}

// list returns the funnel that was just reordered. Reading it back through the
// campaign resolution would answer with the DEFAULT funnel, so a reorder on any
// other funnel appeared to snap back in the UI.
func (uc *ReorderStagesUseCase) list(
	workspaceID string,
	input stage.ReorderStagesInput,
) ([]*stage.Stage, error) {
	if pipelineID := strings.TrimSpace(input.PipelineID); pipelineID != "" {
		return uc.repo.ListByPipeline(workspaceID, pipelineID)
	}
	return uc.repo.ListByCampaign(workspaceID, input.CampaignID, input.CampaignType)
}
