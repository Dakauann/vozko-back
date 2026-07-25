package stage_usecase

import (
	"vozko/domain/stage"
)

type ListStageGroupsUseCase struct {
	repo stage.StageGroupRepository
}

func NewListStageGroupsUseCase(repo stage.StageGroupRepository) stage.ListStageGroupsUseCase {
	return &ListStageGroupsUseCase{repo: repo}
}

func (uc *ListStageGroupsUseCase) Execute(workspaceID string, departmentIDs []string) ([]*stage.StageGroup, error) {
	if len(departmentIDs) > 0 {
		return uc.repo.ListByWorkspaceAndDepartments(workspaceID, departmentIDs)
	}
	return uc.repo.ListByWorkspace(workspaceID)
}
