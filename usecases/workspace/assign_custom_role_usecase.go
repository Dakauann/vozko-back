package workspace_usecase

import (
	"github.com/google/uuid"

	"vozko/domain/workspace"
)

type assignCustomRoleUseCase struct {
	repo     workspace.Repository
	roleRepo workspace.CustomRoleRepository
}

func NewAssignCustomRoleUseCase(repo workspace.Repository, roleRepo workspace.CustomRoleRepository) workspace.AssignCustomRoleUseCase {
	return &assignCustomRoleUseCase{repo: repo, roleRepo: roleRepo}
}

func (uc *assignCustomRoleUseCase) Execute(actorID, workspaceID, memberUserID, callerRole, roleID string) (*workspace.Member, error) {

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

	role, err := uc.roleRepo.GetRoleByID(roleID)
	if err != nil {
		return nil, err
	}
	if role.WorkspaceID != workspaceID {
		return nil, workspace.ErrRoleNotFound
	}

	if err := uc.repo.UpdateMemberRoleID(target.ID, roleID); err != nil {
		return nil, err
	}

	perms := make([]*workspace.Permission, len(role.Permissions))
	for i, pe := range role.Permissions {
		perms[i] = &workspace.Permission{
			ID:       uuid.New().String(),
			MemberID: target.ID,
			Resource: pe.Resource,
			Action:   pe.Action,
		}
	}
	if err := uc.repo.SetPermissions(target.ID, perms); err != nil {
		return nil, err
	}

	return uc.repo.GetMemberByID(target.ID)
}
