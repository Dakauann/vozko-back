package workspace_usecase

import (
	"vozko/domain/workspace"
)

type setMemberPermissionsUseCase struct {
	repo workspace.Repository
}

func NewSetMemberPermissionsUseCase(repo workspace.Repository) workspace.SetMemberPermissionsUseCase {
	return &setMemberPermissionsUseCase{repo: repo}
}

func (uc *setMemberPermissionsUseCase) Execute(actorID, workspaceID, memberUserID, callerRole string, input workspace.SetPermissionsInput) ([]*workspace.Permission, error) {
	if callerRole != "admin" {
		actor := mustBeMember(uc.repo, workspaceID, actorID)
		if actor == nil {
			return nil, workspace.ErrUnauthorized
		}
	}

	target, err := uc.repo.GetMember(workspaceID, memberUserID)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, workspace.ErrMemberNotFound
	}

	if err := workspace.CanEditPermissions(actorID, target); err != nil {
		return nil, err
	}

	entries, err := validatedPermissions("set member permissions in workspace "+workspaceID, input.Permissions)
	if err != nil {
		return nil, err
	}
	if err := uc.repo.SetPermissions(target.ID, memberGrants(target.ID, entries)); err != nil {
		return nil, err
	}

	return uc.repo.GetPermissions(target.ID)
}
