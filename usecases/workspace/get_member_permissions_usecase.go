package workspace_usecase

import (
	"time"

	"vozko/domain/workspace"
)

type getMemberPermissionsUseCase struct {
	repo workspace.Repository
}

func NewGetMemberPermissionsUseCase(repo workspace.Repository) workspace.GetMemberPermissionsUseCase {
	return &getMemberPermissionsUseCase{repo: repo}
}

func (uc *getMemberPermissionsUseCase) Execute(actorID, workspaceID, memberUserID, callerRole string) ([]*workspace.Permission, error) {
	if callerRole != "admin" {
		actor := mustBeMember(uc.repo, workspaceID, actorID)
		if actor == nil {
			return nil, workspace.ErrUnauthorized
		}

		if actorID != memberUserID {
			if !actor.Role.CanManageMembers() {
				has, _ := uc.repo.HasPermission(actor.ID, workspace.ResourceMembers, workspace.ActionRead)
				if !has {
					return nil, workspace.ErrInsufficientPermissions
				}
			}
		}
	}

	target, err := uc.repo.GetMember(workspaceID, memberUserID)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, workspace.ErrMemberNotFound
	}

	if target.Role == workspace.RoleOwner || target.Role == workspace.RoleAdmin {
		return allPermissionsForMember(target.ID), nil
	}

	return uc.repo.GetPermissions(target.ID)
}

func allPermissionsForMember(memberID string) []*workspace.Permission {
	var perms []*workspace.Permission
	now := time.Now()
	for resource, actions := range workspace.ResourceActions {
		for _, action := range actions {
			perms = append(perms, &workspace.Permission{
				MemberID:  memberID,
				Resource:  resource,
				Action:    action.ActionName,
				CreatedAt: now,
			})
		}
	}
	return perms
}
