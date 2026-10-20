package httpx

import (
	"net/http"
	"time"

	"vozko/domain/shared"
	"vozko/domain/workflow"
	"vozko/infra/http/middleware"
)

func ParseDateBound(raw string, endOfDay bool) *time.Time {
	return shared.ParseDateBound(raw, endOfDay)
}

func DepartmentFilterIDs(r *http.Request) []string {
	filter := middleware.GetDepartmentFilter(r)
	if filter == nil {
		return nil
	}
	return filter.EffectiveDepartmentIDs()
}

func ShouldReturnEmptyDepartmentList(r *http.Request) bool {
	filter := middleware.GetDepartmentFilter(r)
	if filter == nil || !filter.ShouldFilter() {
		return false
	}
	return len(filter.EffectiveDepartmentIDs()) == 0
}

func CanAccessDepartment(r *http.Request, resourceDepartmentID string) bool {
	filter := middleware.GetDepartmentFilter(r)
	if filter == nil {
		return true
	}
	return filter.CanAccess(resourceDepartmentID)
}

func WorkflowScope(r *http.Request) workflow.Scope {
	return workflow.Scope{WorkspaceID: middleware.GetWorkspaceID(r), Departments: middleware.GetDepartmentFilter(r), Requester: RequesterOf(r)}
}
