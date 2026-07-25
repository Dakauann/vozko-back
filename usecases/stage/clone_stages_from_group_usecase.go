package stage_usecase

import (
	"fmt"
	"strings"

	"github.com/google/uuid"

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

// Execute routes the campaign onto the conversation funnel for this stage group.
// "Same group → same board": if a pipeline was already stamped from this group in
// the workspace, the campaign is pointed at THAT existing pipeline (no duplicate,
// no re-clone). Otherwise it loads the group, rejects a cross-workspace group,
// creates ONE new conversation Pipeline tagged with the group id, clones the group's
// items into it (first = initial, name lower-cased/trimmed, pipeline-scoped NOT
// campaign_id-scoped), then points the campaign at it. Either way `ListByCampaign` /
// the AI stage classifier / initial-stage resolve through the campaign's pipeline.
// Per-item create errors don't abort the loop; the first one is returned to log.
func (uc *cloneStagesFromGroupUseCase) Execute(workspaceID, campaignID, campaignType, stageGroupID string) error {
	// Reuse an existing pipeline stamped from this group so multiple campaigns on
	// the same template share one board instead of forking duplicates.
	if existingID, err := uc.stageRepo.FindConversationPipelineByGroup(workspaceID, stageGroupID); err != nil {
		return fmt.Errorf("lookup pipeline for group %s: %w", stageGroupID, err)
	} else if existingID != "" {
		if err := uc.stageRepo.SetCampaignPipeline(campaignID, campaignType, existingID); err != nil {
			return fmt.Errorf("attach campaign to existing pipeline %s: %w", existingID, err)
		}
		return nil
	}

	group, err := uc.groupRepo.FindByID(stageGroupID)
	if err != nil {
		return fmt.Errorf("find stage group %s: %w", stageGroupID, err)
	}
	if group.WorkspaceID != workspaceID {
		return fmt.Errorf("stage group %s belongs to a different workspace", stageGroupID)
	}

	pipelineID, err := uc.stageRepo.CreateConversationPipeline(workspaceID, group.Name, stageGroupID)
	if err != nil {
		return fmt.Errorf("create pipeline from group %s: %w", stageGroupID, err)
	}

	var firstErr error
	for i, item := range group.Items {
		s := &stage.Stage{
			ID:          uuid.New().String(),
			WorkspaceID: workspaceID,
			PipelineID:  pipelineID,
			Name:        strings.ToLower(strings.TrimSpace(item.Name)),
			Description: item.Description,
			Color:       item.Color,
			Position:    item.Position,
			IsInitial:   i == 0,
		}
		if err := uc.stageRepo.Create(s); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if err := uc.stageRepo.SetCampaignPipeline(campaignID, campaignType, pipelineID); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}
