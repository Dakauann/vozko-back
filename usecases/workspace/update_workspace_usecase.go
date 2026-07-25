package workspace_usecase

import (
	"strings"

	"vozko/domain/workspace"
)

type updateWorkspaceUseCase struct {
	repo workspace.Repository
}

func NewUpdateWorkspaceUseCase(repo workspace.Repository) workspace.UpdateWorkspaceUseCase {
	return &updateWorkspaceUseCase{repo: repo}
}

func (uc *updateWorkspaceUseCase) Execute(userID, workspaceID, callerRole string, input workspace.UpdateWorkspaceInput) (*workspace.Workspace, error) {
	if callerRole != "admin" {
		member := mustBeMember(uc.repo, workspaceID, userID)
		if member == nil {
			return nil, workspace.ErrUnauthorized
		}
		if !member.Role.CanManageMembers() {
			return nil, workspace.ErrInsufficientPermissions
		}
	}

	ws, err := uc.repo.GetWorkspaceByID(workspaceID)
	if err != nil {
		return nil, err
	}

	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			return nil, workspace.ErrWorkspaceNameRequired
		}
		ws.Name = name
	}

	if err := uc.repo.UpdateWorkspace(ws); err != nil {
		return nil, err
	}

	return uc.repo.GetWorkspaceByID(workspaceID)
}
