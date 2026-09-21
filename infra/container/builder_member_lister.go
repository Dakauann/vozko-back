package container

import (
	"context"

	workspace_domain "vozko/domain/workspace"
	workflow_usecase "vozko/usecases/workflow"
)

type builderMemberLister struct {
	repo workspace_domain.Repository
}

func (l builderMemberLister) ListMembers(_ context.Context, workspaceID, query string, limit int) ([]workflow_usecase.ResourceMatch, error) {
	if l.repo == nil {
		return nil, nil
	}
	pageSize := limit
	if pageSize < 25 {
		pageSize = 25
	}
	members, _, err := l.repo.ListAssignableMembers(workspaceID, query, false, nil, true, "", 1, pageSize)
	if err != nil {
		return nil, err
	}
	out := make([]workflow_usecase.ResourceMatch, 0, len(members))
	for _, m := range members {
		if m == nil || m.WorkspaceID != workspaceID || m.UserID == "" {
			continue
		}
		name := m.Username
		if name == "" {
			name = m.Email
		}
		if name == "" {
			name = m.UserID
		}
		out = append(out, workflow_usecase.ResourceMatch{ID: m.UserID, Name: name})
	}
	return out, nil
}
