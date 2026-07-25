package workspace_usecase

import "vozko/domain/workspace"

func mustBeMember(repo workspace.Repository, workspaceID, userID string) *workspace.Member {
	member, err := repo.GetMember(workspaceID, userID)
	if err != nil || member == nil {
		return nil
	}
	return member
}
