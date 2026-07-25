package container

import (
	"context"

	workspace_domain "vozko/domain/workspace"
	workflow_usecase "vozko/usecases/workflow"
)

// builderMemberLister adapts the workspace member repository to the AI builder's
// resource resolver so the copilot can resolve attendants for transfer/assign
// nodes (target_user_id). The id is the member's UserID — the exact value the
// human picker stores and the target_user_id field expects. Workspace-scoped and
// re-asserted per row, so it never leaks members of another tenant.
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
	// restrict=false / includeAdmins=true / no self-exclusion: the builder lists
	// the whole workspace roster (the runtime transfer still enforces ownership).
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
