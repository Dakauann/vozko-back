package workspace_usecase

import "vozko/domain/workspace"

func actorFor(repo workspace.Repository, workspaceID, actorID, callerRole string) (workspace.Actor, error) {
	if callerRole == "admin" {
		return workspace.Actor{PlatformAdmin: true}, nil
	}
	actor := mustBeMember(repo, workspaceID, actorID)
	if actor == nil {
		return workspace.Actor{}, workspace.ErrUnauthorized
	}
	return workspace.Actor{UserID: actor.UserID, Role: actor.Role}, nil
}

func requireManager(repo workspace.Repository, workspaceID, actorID, callerRole string) error {
	actor, err := actorFor(repo, workspaceID, actorID, callerRole)
	if err != nil {
		return err
	}
	return actor.ManagesMembers()
}

func mustBeMember(repo workspace.Repository, workspaceID, userID string) *workspace.Member {
	member, err := repo.GetMember(workspaceID, userID)
	if err != nil || member == nil {
		return nil
	}
	return member
}
