package workspace_usecase

import (
	"github.com/google/uuid"

	"vozko/domain/workspace"
)

type assignResourceUseCase struct {
	repo workspace.Repository
}

func NewAssignResourceUseCase(repo workspace.Repository) workspace.AssignResourceUseCase {
	return &assignResourceUseCase{repo: repo}
}

func (uc *assignResourceUseCase) Execute(actorID, workspaceID, callerRole string, input workspace.AssignResourceInput) (*workspace.ResourceAssignment, error) {
	if callerRole != "admin" {
		actor := mustBeMember(uc.repo, workspaceID, actorID)
		if actor == nil {
			return nil, workspace.ErrUnauthorized
		}
		if !actor.Role.CanManageMembers() {
			return nil, workspace.ErrInsufficientPermissions
		}
	}

	resourceType := workspace.Resource(input.ResourceType)
	if !resourceType.IsValid() {
		return nil, workspace.ErrInvalidResource
	}

	targetMember, err := uc.repo.GetMember(workspaceID, input.MemberUserID)
	if err != nil {
		return nil, err
	}
	if targetMember == nil {
		return nil, workspace.ErrMemberNotFound
	}

	assigned, err := uc.repo.IsResourceAssignedToMember(workspaceID, resourceType, input.ResourceID, targetMember.ID)
	if err != nil {
		return nil, err
	}
	if assigned {
		return nil, workspace.ErrResourceAlreadyAssigned
	}

	assignment := &workspace.ResourceAssignment{
		ID:           uuid.New().String(),
		WorkspaceID:  workspaceID,
		ResourceType: resourceType,
		ResourceID:   input.ResourceID,
		MemberID:     targetMember.ID,
	}

	if err := uc.repo.AssignResource(assignment); err != nil {
		return nil, err
	}

	assignment.MemberEmail = targetMember.Email
	assignment.MemberUsername = targetMember.Username

	return assignment, nil
}
