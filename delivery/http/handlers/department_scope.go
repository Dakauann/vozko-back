package handlers

import (
	"net/http"
	"strings"

	"vozko/delivery/http/httpx"
	workspace_department "vozko/domain/workspace/workspace_department"
	"vozko/infra/http/middleware"
)

func withDepartmentCreationScope(r *http.Request, explicitDepartmentID string) *http.Request {
	if r == nil {
		return nil
	}

	requestedDepartmentID := strings.TrimSpace(explicitDepartmentID)
	if requestedDepartmentID == "" {
		if filter := middleware.GetDepartmentFilter(r); filter != nil && filter.SelectedDepartmentID != nil {
			requestedDepartmentID = strings.TrimSpace(*filter.SelectedDepartmentID)
		}
	}

	scope := workspace_department.CreationScope{RequestedDepartmentID: requestedDepartmentID}
	if claims := middleware.GetClaims(r); claims != nil {
		scope.UserID = claims.UserID
		scope.IsSystemAdmin = claims.Role == "admin"
	}

	return r.WithContext(workspace_department.WithCreationScope(r.Context(), scope))
}

// The two below are thin aliases: the definitions moved to httpx so the export
// package can apply the caller's department scope with exactly the same rule
// this package does, instead of re-deriving it from the middleware and drifting.

func departmentFilterIDs(r *http.Request) []string {
	return httpx.DepartmentFilterIDs(r)
}

func shouldReturnEmptyDepartmentList(r *http.Request) bool {
	return httpx.ShouldReturnEmptyDepartmentList(r)
}

func canAccessDepartment(r *http.Request, resourceDepartmentID string) bool {
	return httpx.CanAccessDepartment(r, resourceDepartmentID)
}
