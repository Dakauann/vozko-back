package container

import (
	workspace_domain "vozko/domain/workspace"
)

type workspaceOwnerReaderAdapter struct {
	repo workspace_domain.Repository
}

func newWorkspaceOwnerReaderAdapter(repo workspace_domain.Repository) *workspaceOwnerReaderAdapter {
	return &workspaceOwnerReaderAdapter{repo: repo}
}

func (a *workspaceOwnerReaderAdapter) GetOwnerUserID(workspaceID string) (string, error) {
	if a == nil || a.repo == nil || workspaceID == "" {
		return "", nil
	}
	ws, err := a.repo.GetWorkspaceByID(workspaceID)
	if err != nil {
		if err == workspace_domain.ErrWorkspaceNotFound {
			return "", nil
		}
		return "", err
	}
	if ws == nil {
		return "", nil
	}
	return ws.OwnerID, nil
}
