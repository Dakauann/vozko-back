package stage_usecase

import (
	"fmt"

	"vozko/domain/stage"
)

type cloneStagesFromGroupUseCase struct {
	groupRepo stage.StageGroupRepository
	stageRepo stage.Repository
}

func NewCloneStagesFromGroupUseCase(
	groupRepo stage.StageGroupRepository,
	stageRepo stage.Repository,
) stage.CloneStagesFromGroupUseCase {
	return &cloneStagesFromGroupUseCase{groupRepo: groupRepo, stageRepo: stageRepo}
}

func (uc *cloneStagesFromGroupUseCase) Execute(workspaceID, campaignID, campaignType, stageGroupID string) error {
	pipelineID, err := ensurePipelineForGroup(uc.groupRepo, uc.stageRepo, workspaceID, stageGroupID)
	if err != nil {
		return err
	}
	if err := uc.stageRepo.SetCampaignPipeline(campaignID, campaignType, pipelineID); err != nil {
		return fmt.Errorf("attach campaign to pipeline %s: %w", pipelineID, err)
	}
	return nil
}
