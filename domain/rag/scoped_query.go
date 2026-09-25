package rag

import (
	"context"
	"errors"

	wd "vozko/domain/workspace/workspace_department"
)

const (
	DefaultQueryResults      = 5
	MaxQueryResults          = 20
	DefaultQueryMinScore     = 0.3
	MaxQueriedKnowledgeBases = 10
)

var (
	ErrKnowledgeBaseAccessDenied = errors.New("rag: no access to this knowledge base")
	ErrQueryRequired             = errors.New("rag: query is required")
	ErrQueryTooManyBases         = errors.New("rag: too many knowledge bases in one query")
)

type Viewer struct {
	WorkspaceID string
	Departments *wd.DepartmentFilter
}

func (v Viewer) CanRead(kb *KnowledgeBase) bool {
	if kb == nil || v.WorkspaceID == "" || kb.WorkspaceID != v.WorkspaceID {
		return false
	}
	return v.Departments.Allows(kb.DepartmentID)
}

type ScopedQueryUseCase interface {
	Execute(ctx context.Context, viewer Viewer, input QueryInput) (*QueryOutput, error)
}

func QueryResultLimit(requested int) int {
	if requested <= 0 {
		return DefaultQueryResults
	}
	if requested > MaxQueryResults {
		return MaxQueryResults
	}
	return requested
}
