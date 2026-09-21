package httpx

import (
	"net/http"
	"strings"
	"time"

	"vozko/infra/http/middleware"
)

func ParseDateBound(raw string, endOfDay bool) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return &t
	}
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		if endOfDay {
			t = t.Add(24*time.Hour - time.Nanosecond)
		}
		return &t
	}
	return nil
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
