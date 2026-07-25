package workspace_usecase

import "vozko/domain/workspace"

type listMembersUseCase struct {
	repo workspace.Repository
}

func NewListMembersUseCase(repo workspace.Repository) workspace.ListMembersUseCase {
	return &listMembersUseCase{repo: repo}
}

func (uc *listMembersUseCase) Execute(userID, workspaceID, callerRole string) ([]*workspace.Member, error) {
	if callerRole != "admin" {
		member := mustBeMember(uc.repo, workspaceID, userID)
		if member == nil {
			return nil, workspace.ErrUnauthorized
		}
	}
	return uc.repo.ListMembers(workspaceID)
}
