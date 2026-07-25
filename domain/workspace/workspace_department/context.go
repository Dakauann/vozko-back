package workspace_department

import (
	"context"
	"strings"
)

type creationScopeKey struct{}

type CreationScope struct {
	UserID                string
	IsSystemAdmin         bool
	RequestedDepartmentID string
}

func WithCreationScope(ctx context.Context, scope CreationScope) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	scope.RequestedDepartmentID = strings.TrimSpace(scope.RequestedDepartmentID)
	return context.WithValue(ctx, creationScopeKey{}, scope)
}

func GetCreationScope(ctx context.Context) (CreationScope, bool) {
	if ctx == nil {
		return CreationScope{}, false
	}
	scope, ok := ctx.Value(creationScopeKey{}).(CreationScope)
	if !ok {
		return CreationScope{}, false
	}
	scope.RequestedDepartmentID = strings.TrimSpace(scope.RequestedDepartmentID)
	return scope, true
}
