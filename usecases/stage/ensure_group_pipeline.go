package stage_usecase

import (
	"fmt"
	"strings"

	"github.com/google/uuid"

	"vozko/domain/stage"
)

func ensurePipelineForGroup(
	groupRepo stage.StageGroupRepository,
	stageRepo stage.Repository,
	workspaceID, stageGroupID string,
) (string, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	stageGroupID = strings.TrimSpace(stageGroupID)
	if workspaceID == "" || stageGroupID == "" {
		return "", fmt.Errorf("ensurePipelineForGroup: workspace and stage group are required")
	}

	existingID, err := stageRepo.FindConversationPipelineByGroup(workspaceID, stageGroupID)
	if err != nil {
		return "", fmt.Errorf("lookup pipeline for group %s: %w", stageGroupID, err)
	}
	if existingID != "" {
		return existingID, nil
	}

	group, err := groupRepo.FindByID(stageGroupID)
	if err != nil {
		return "", fmt.Errorf("find stage group %s: %w", stageGroupID, err)
	}
	if group == nil {
		return "", fmt.Errorf("stage group %s not found", stageGroupID)
	}
	if group.WorkspaceID != workspaceID {
		return "", fmt.Errorf("stage group %s belongs to a different workspace", stageGroupID)
	}

	pipelineID, err := stageRepo.CreateConversationPipeline(workspaceID, group.Name, stageGroupID)
	if err != nil {
		return "", fmt.Errorf("create pipeline from group %s: %w", stageGroupID, err)
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
		if err := stageRepo.Create(s); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return pipelineID, firstErr
}
