package handlers

import (
	"net/http"
	"strings"

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

func departmentFilterIDs(r *http.Request) []string {
	filter := middleware.GetDepartmentFilter(r)
	if filter == nil {
		return nil
	}
	return filter.EffectiveDepartmentIDs()
}

func shouldReturnEmptyDepartmentList(r *http.Request) bool {
	filter := middleware.GetDepartmentFilter(r)
	if filter == nil || !filter.ShouldFilter() {
		return false
	}
	return len(filter.EffectiveDepartmentIDs()) == 0
}

func canAccessDepartment(r *http.Request, resourceDepartmentID string) bool {
	filter := middleware.GetDepartmentFilter(r)
	if filter == nil {
		return true
	}
	ids := filter.EffectiveDepartmentIDs()
	if resourceDepartmentID == "" {
		if filter.IsOwnerOrAdmin {
			return len(ids) == 0
		}
		return !filter.ShouldFilter()
	}
	if len(ids) == 0 {
		return !filter.ShouldFilter() || filter.IsOwnerOrAdmin
	}
	for _, id := range ids {
		if id == resourceDepartmentID {
			return true
		}
	}
	return false
}
