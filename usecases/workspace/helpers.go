package workspace_usecase

import (
	"fmt"
	"log"

	"github.com/google/uuid"

	"vozko/domain/workspace"
)

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

func uniqueRoleName(roles workspace.CustomRoleRepository, workspaceID, name, exceptID string) error {
	existing, err := roles.ListRolesByWorkspace(workspaceID)
	if err != nil {
		return err
	}
	if workspace.RoleNameTaken(existing, name, exceptID) {
		return workspace.ErrRoleNameTaken
	}
	return nil
}

func validatedPermissions(context string, entries []workspace.PermissionEntry) ([]workspace.PermissionEntry, error) {
	if kept, dropped := workspace.DropRetiredResources(entries); len(dropped) > 0 {
		log.Printf("[workspace] %s: dropping %d permission(s) for resource(s) this build no longer defines: %v",
			context, len(entries)-len(kept), dropped)
		entries = kept
	}
	for _, pe := range entries {
		if !pe.Resource.IsValid() {
			return nil, workspace.ErrInvalidResource
		}
		if !pe.Action.IsValid() || !workspace.ValidActionForResource(pe.Resource, pe.Action) {
			return nil, workspace.ErrInvalidAction
		}
	}
	return workspace.EnforceDependencies(entries), nil
}

func propagateRolePermissions(repo workspace.Repository, roles workspace.CustomRoleRepository, role *workspace.CustomRole) error {
	members, err := roles.ListMembersByRoleID(role.ID)
	if err != nil {
		return fmt.Errorf("role %s saved but its members could not be listed: %w", role.ID, err)
	}
	for _, m := range members {
		perms := make([]*workspace.Permission, len(role.Permissions))
		for i, pe := range role.Permissions {
			perms[i] = &workspace.Permission{ID: uuid.New().String(), MemberID: m.ID, Resource: pe.Resource, Action: pe.Action}
		}
		if err := repo.SetPermissions(m.ID, perms); err != nil {
			return fmt.Errorf("role %s saved but member %s kept the old permissions: %w", role.ID, m.ID, err)
		}
	}
	return nil
}
