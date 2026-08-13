package httpx

import (
	"net/http"
	"strings"
	"time"

	"vozko/infra/http/middleware"
)

// ParseDateBound parses an inclusive date bound from a query parameter.
//
// It accepts a full RFC3339 timestamp (used as-is, timezone honored) or a
// date-only "2006-01-02" value. For a date-only upper bound, endOfDay extends it
// to the final instant of that day so the range stays inclusive. Returns nil for
// blank or unparseable input; an absent bound means unbounded.
//
// This lives here rather than beside one handler because two endpoints answer
// the same question about the same data — the disparos summary and the export
// of the leads behind it. Two parsers would let a file and the tiles above it
// disagree about what "até 31/07" means, which is the kind of mismatch nobody
// reports as a bug and everybody stops trusting.
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

// DepartmentFilterIDs returns the departments the caller is scoped to, or nil
// when they are not scoped at all.
func DepartmentFilterIDs(r *http.Request) []string {
	filter := middleware.GetDepartmentFilter(r)
	if filter == nil {
		return nil
	}
	return filter.EffectiveDepartmentIDs()
}

// ShouldReturnEmptyDepartmentList reports that the caller is department-scoped
// but that scope resolves to no department at all — meaning they can see
// nothing, which is not the same as an unscoped caller seeing everything.
func ShouldReturnEmptyDepartmentList(r *http.Request) bool {
	filter := middleware.GetDepartmentFilter(r)
	if filter == nil || !filter.ShouldFilter() {
		return false
	}
	return len(filter.EffectiveDepartmentIDs()) == 0
}

// CanAccessDepartment reports whether the caller may read a resource sitting in
// resourceDepartmentID (empty meaning the resource has no department).
//
// The unassigned case is the subtle one and the reason this is a shared
// function rather than a loop at each call site: a resource with no department
// is visible to an unscoped caller and to an owner/admin who has not narrowed
// their view, and hidden from everyone else. An export re-deriving that rule by
// hand would be a second, slightly different answer to "is this yours" — and
// the one that governs whole-file downloads is not where a near-miss is
// acceptable.
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
