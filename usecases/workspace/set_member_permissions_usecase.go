package workspace_usecase

import (
	"github.com/google/uuid"

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

	if actorID == memberUserID {
		return nil, workspace.ErrCannotModifySelf
	}

	target, err := uc.repo.GetMember(workspaceID, memberUserID)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, workspace.ErrMemberNotFound
	}

	if target.Role == workspace.RoleOwner || target.Role == workspace.RoleAdmin {
		return nil, workspace.ErrCannotChangeOwnerRole
	}

	for _, pe := range input.Permissions {
		if !pe.Resource.IsValid() {
			return nil, workspace.ErrInvalidResource
		}
		if !pe.Action.IsValid() {
			return nil, workspace.ErrInvalidAction
		}
		if !workspace.ValidActionForResource(pe.Resource, pe.Action) {
			return nil, workspace.ErrInvalidAction
		}
	}

	input.Permissions = workspace.EnforceDependencies(input.Permissions)

	permissions := make([]*workspace.Permission, len(input.Permissions))
	for i, pe := range input.Permissions {
		permissions[i] = &workspace.Permission{
			ID:       uuid.New().String(),
			MemberID: target.ID,
			Resource: pe.Resource,
			Action:   pe.Action,
		}
	}

	if err := uc.repo.SetPermissions(target.ID, permissions); err != nil {
		return nil, err
	}

	return uc.repo.GetPermissions(target.ID)
}
