package stage_usecase

import (
	"vozko/domain/stage"
)

type GetStageGroupUseCase struct {
	repo stage.StageGroupRepository
}

func NewGetStageGroupUseCase(repo stage.StageGroupRepository) stage.GetStageGroupUseCase {
	return &GetStageGroupUseCase{repo: repo}
}

func (uc *GetStageGroupUseCase) Execute(workspaceID, groupID string) (*stage.StageGroup, error) {
	group, err := uc.repo.FindByID(groupID)
	if err != nil {
		return nil, stage.ErrTagGroupNotFound
	}
	if group.WorkspaceID != workspaceID {
		return nil, stage.ErrUnauthorized
	}
	return group, nil
}
