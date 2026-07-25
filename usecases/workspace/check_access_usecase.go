package workspace_usecase

import "vozko/domain/workspace"

type checkAccessUseCase struct {
	repo workspace.Repository
}

func NewCheckAccessUseCase(repo workspace.Repository) workspace.CheckAccessUseCase {
	return &checkAccessUseCase{repo: repo}
}

func (uc *checkAccessUseCase) Execute(userID, workspaceID string, resource workspace.Resource, action workspace.Action) error {
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

	return nil
}
