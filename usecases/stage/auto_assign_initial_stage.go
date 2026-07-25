package stage_usecase

import (
	"log"

	"github.com/google/uuid"

	"vozko/domain/stage"
)

func AutoAssignInitialStage(repo stage.Repository, workspaceID, campaignID, campaignType, entryID, entryType string) {
	if err := repo.EnsureDefaultStagesForCampaign(workspaceID, campaignID, campaignType); err != nil {
		log.Printf("[TagAutoAssign] Error ensuring default tags for campaign %s: %v", campaignID, err)
		return
	}

	existingTag, err := repo.GetEntryStage(entryID, entryType, workspaceID)
	if err != nil {
		log.Printf("[TagAutoAssign] Error checking existing tag for entry %s: %v", entryID, err)
		return
	}
	if existingTag != nil {
		return
	}

	initialTag, err := repo.GetInitialStageForCampaign(workspaceID, campaignID, campaignType)
	if err != nil {
		log.Printf("[TagAutoAssign] Error getting initial tag for campaign %s: %v", campaignID, err)
		return
	}
	if initialTag == nil {
		return
	}

	et := &stage.EntryStage{
		ID:          uuid.New().String(),
		StageID:       initialTag.ID,
		EntryID:     entryID,
		EntryType:   entryType,
		WorkspaceID: workspaceID,
	}

	if err := repo.AssignStage(et); err != nil {
		if err != stage.ErrEntryTagExists {
			log.Printf("[TagAutoAssign] Error assigning initial tag %s to entry %s: %v", initialTag.ID, entryID, err)
		}
	} else {
		log.Printf("[TagAutoAssign] Auto-assigned tag '%s' to entry %s (%s) for campaign %s", initialTag.Name, entryID, entryType, campaignID)
	}
}
