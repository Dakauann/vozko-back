package workspace_usecase

import "vozko/domain/workspace"

type listResourceAssignmentsUseCase struct {
	repo workspace.Repository
}

func NewListResourceAssignmentsUseCase(repo workspace.Repository) workspace.ListResourceAssignmentsUseCase {
	return &listResourceAssignmentsUseCase{repo: repo}
}

func (uc *listResourceAssignmentsUseCase) Execute(actorID, workspaceID, callerRole, resourceType, resourceID string) ([]*workspace.ResourceAssignment, error) {
	if callerRole != "admin" {
		actor := mustBeMember(uc.repo, workspaceID, actorID)
		if actor == nil {
			return nil, workspace.ErrUnauthorized
		}
	}

	rt := workspace.Resource(resourceType)
	if !rt.IsValid() {
		return nil, workspace.ErrInvalidResource
	}

	return uc.repo.ListAssignmentsByResource(workspaceID, rt, resourceID)
}
