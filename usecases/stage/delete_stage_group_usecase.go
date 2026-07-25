package stage_usecase

import (
	"vozko/domain/stage"
)

type DeleteStageGroupUseCase struct {
	repo stage.StageGroupRepository
}

func NewDeleteStageGroupUseCase(repo stage.StageGroupRepository) stage.DeleteStageGroupUseCase {
	return &DeleteStageGroupUseCase{repo: repo}
}

func (uc *DeleteStageGroupUseCase) Execute(workspaceID, groupID string) error {
	existing, err := uc.repo.FindByID(groupID)
	if err != nil {
		return stage.ErrTagGroupNotFound
	}
	if existing.WorkspaceID != workspaceID {
		return stage.ErrUnauthorized
	}
	return uc.repo.Delete(groupID)
}
