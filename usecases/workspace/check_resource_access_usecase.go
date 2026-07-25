package workspace_usecase

import "vozko/domain/workspace"

type checkResourceAccessUseCase struct {
	repo workspace.Repository
}

func NewCheckResourceAccessUseCase(repo workspace.Repository) workspace.CheckResourceAccessUseCase {
	return &checkResourceAccessUseCase{repo: repo}
}

func (uc *checkResourceAccessUseCase) Execute(userID, workspaceID string, resource workspace.Resource, action workspace.Action, resourceID string) error {
	member, err := uc.repo.GetMember(workspaceID, userID)
	if err != nil {
		return err
	}
	if member == nil {
		return workspace.ErrUnauthorized
	}

	if member.Role == workspace.RoleOwner || member.Role == workspace.RoleAdmin {
		return nil
	}

	has, err := uc.repo.HasPermission(member.ID, resource, action)
	if err != nil {
		return err
	}
	if !has {
		return workspace.ErrInsufficientPermissions
	}

	if resourceID == "" {
		return nil
	}

	hasAssignments, err := uc.repo.HasAnyAssignments(workspaceID, resource, resourceID)
	if err != nil {
		return err
	}

	if !hasAssignments {
		return nil
	}

	assigned, err := uc.repo.IsResourceAssignedToMember(workspaceID, resource, resourceID, member.ID)
	if err != nil {
		return err
	}
	if !assigned {
		return workspace.ErrResourceAccessDenied
	}

	return nil
}
