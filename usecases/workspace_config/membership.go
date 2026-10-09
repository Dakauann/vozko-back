package workspace_config_usecase

import (
	"errors"
	"strings"

	"github.com/google/uuid"

	"vozko/domain/workspace"
)

var ErrNotWorkspaceMember = errors.New("workspace config: the caller is not a member of this workspace")

type WorkspaceMembership struct {
	OwnerID       string
	UserID        string
	PlatformAdmin bool
}

type WorkspaceDirectory interface {
	GetWorkspaceByID(id string) (*workspace.Workspace, error)
	GetMember(workspaceID, userID string) (*workspace.Member, error)
}

type ActorNames interface {
	Names(actorIDs ...string) map[string]string
}

func ReadMembership(workspaces WorkspaceDirectory, workspaceID, userID string, platformAdmin bool) (WorkspaceMembership, error) {
	workspaceID, userID = strings.TrimSpace(workspaceID), strings.TrimSpace(userID)
	if workspaceID == "" || userID == "" || workspaces == nil {
		return WorkspaceMembership{}, ErrNotWorkspaceMember
	}
	if _, err := uuid.Parse(workspaceID); err != nil {
		return WorkspaceMembership{}, ErrNotWorkspaceMember
	}
	ws, err := workspaces.GetWorkspaceByID(workspaceID)
	if errors.Is(err, workspace.ErrWorkspaceNotFound) {
		return WorkspaceMembership{}, ErrNotWorkspaceMember
	}
	if err != nil {
		return WorkspaceMembership{}, err
	}
	if ws == nil {
		return WorkspaceMembership{}, ErrNotWorkspaceMember
	}
	if !platformAdmin {
		member, err := workspaces.GetMember(workspaceID, userID)
		if errors.Is(err, workspace.ErrMemberNotFound) {
			return WorkspaceMembership{}, ErrNotWorkspaceMember
		}
		if err != nil {
			return WorkspaceMembership{}, err
		}
		if member == nil {
			return WorkspaceMembership{}, ErrNotWorkspaceMember
		}
	}
	return WorkspaceMembership{OwnerID: ws.OwnerID, UserID: userID, PlatformAdmin: platformAdmin}, nil
}
