package stage_usecase

import (
	"vozko/domain/conversation"
	"vozko/domain/stage"
)

type initialTagAssignerService struct {
	repo stage.Repository
}

func NewInitialStageAssignerService(repo stage.Repository) conversation.InitialStageAssigner {
	return &initialTagAssignerService{repo: repo}
}

func (s *initialTagAssignerService) AutoAssignInitialStage(workspaceID, campaignID, campaignType, entryID, entryType string) {
	AutoAssignInitialStage(s.repo, workspaceID, campaignID, campaignType, entryID, entryType)
}
