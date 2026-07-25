package workspace_usecase

import "vozko/domain/workspace"

type updateMemberRoleUseCase struct {
	repo workspace.Repository
}

func NewUpdateMemberRoleUseCase(repo workspace.Repository) workspace.UpdateMemberRoleUseCase {
	return &updateMemberRoleUseCase{repo: repo}
}

func (uc *updateMemberRoleUseCase) Execute(actorID, workspaceID, memberUserID, callerRole string, role workspace.Role) (*workspace.Member, error) {
	if !role.IsValid() || role == workspace.RoleOwner {
		return nil, workspace.ErrInvalidRole
	}

	var actorRole workspace.Role
	if callerRole == "admin" {

		actorRole = workspace.RoleOwner
	} else {
		actor := mustBeMember(uc.repo, workspaceID, actorID)
		if actor == nil {
			return nil, workspace.ErrUnauthorized
		}
		if !actor.Role.CanManageMembers() {
			return nil, workspace.ErrInsufficientPermissions
		}
		actorRole = actor.Role
	}

	target, err := uc.repo.GetMember(workspaceID, memberUserID)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, workspace.ErrMemberNotFound
	}

	if target.Role == workspace.RoleOwner {
		return nil, workspace.ErrCannotChangeOwnerRole
	}

	if target.Role == workspace.RoleAdmin && actorRole != workspace.RoleOwner {
		return nil, workspace.ErrInsufficientPermissions
	}

	if err := uc.repo.UpdateMemberRole(target.ID, role); err != nil {
		return nil, err
	}

	return uc.repo.GetMemberByID(target.ID)
}
