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

// Execute points the campaign at the conversation funnel for this stage group.
//
// Materializing the group is no longer this use case's job — ensurePipelineForGroup
// owns it, and the group's own creation already called it, so by the time a campaign
// gets here the pipeline usually exists and this is a pure attach. Calling it again
// is safe and deliberate: it self-heals a group created before this path existed,
// and keeps "same group → same board" true no matter which door ran first.
//
// Either way `ListByCampaign` / the AI stage classifier / initial-stage resolve
// through the campaign's pipeline afterwards.
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
