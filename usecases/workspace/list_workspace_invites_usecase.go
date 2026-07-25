package workspace_usecase

import "vozko/domain/workspace"

type listWorkspaceInvitesUseCase struct {
	repo workspace.Repository
}

func NewListWorkspaceInvitesUseCase(repo workspace.Repository) workspace.ListWorkspaceInvitesUseCase {
	return &listWorkspaceInvitesUseCase{repo: repo}
}

func (uc *listWorkspaceInvitesUseCase) Execute(userID, workspaceID, callerRole string) ([]*workspace.Invite, error) {
	if callerRole != "admin" {
		member := mustBeMember(uc.repo, workspaceID, userID)
		if member == nil {
			return nil, workspace.ErrUnauthorized
		}
	}
	return uc.repo.ListInvitesByWorkspace(workspaceID)
}
