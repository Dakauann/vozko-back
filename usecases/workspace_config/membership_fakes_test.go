package workspace_config_usecase

import (
	"time"

	"vozko/domain/workspace"
)

const policyWorkspace = "5b0f4a7e-3c2d-4e8a-9f61-2d7c1b9e0a44"

var policyNow = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

type memWorkspaces struct {
	ownerID   string
	members   map[string]bool
	err       error
	memberErr error
}

func (m *memWorkspaces) GetWorkspaceByID(id string) (*workspace.Workspace, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &workspace.Workspace{ID: id, OwnerID: m.ownerID}, nil
}

func (m *memWorkspaces) GetMember(workspaceID, userID string) (*workspace.Member, error) {
	if m.memberErr != nil {
		return nil, m.memberErr
	}
	if !m.members[userID] {
		return nil, nil
	}
	return &workspace.Member{WorkspaceID: workspaceID, UserID: userID}, nil
}

type memNames map[string]string

func (m memNames) Names(ids ...string) map[string]string {
	out := map[string]string{}
	for _, id := range ids {
		if name, ok := m[id]; ok {
			out[id] = name
		}
	}
	return out
}
