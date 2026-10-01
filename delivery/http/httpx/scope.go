package httpx

import (
	"net/http"
	"time"

	"vozko/domain/shared"
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
