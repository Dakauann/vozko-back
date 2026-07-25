package workspace_usecase

import "vozko/domain/workspace"

type unassignResourceUseCase struct {
	repo workspace.Repository
}

func NewUnassignResourceUseCase(repo workspace.Repository) workspace.UnassignResourceUseCase {
	return &unassignResourceUseCase{repo: repo}
}

func (uc *unassignResourceUseCase) Execute(actorID, workspaceID, resourceType, resourceID, memberUserID, callerRole string) error {
	if callerRole != "admin" {
		actor := mustBeMember(uc.repo, workspaceID, actorID)
		if actor == nil {
			return workspace.ErrUnauthorized
		}
		if !actor.Role.CanManageMembers() {
			return workspace.ErrInsufficientPermissions
		}
	}

	rt := workspace.Resource(resourceType)
	if !rt.IsValid() {
		return workspace.ErrInvalidResource
	}

	targetMember, err := uc.repo.GetMember(workspaceID, memberUserID)
	if err != nil {
		return err
	}
	if targetMember == nil {
		return workspace.ErrMemberNotFound
	}

	return uc.repo.UnassignResource(workspaceID, rt, resourceID, targetMember.ID)
}
