package ws

import (
	"context"
	"net/http/httptest"
	"testing"

	workspace_department "vozko/domain/workspace/workspace_department"
	"vozko/infra/http/middleware"
)

func TestResolveConnectionDepartmentID(t *testing.T) {
	t.Run("query param overrides context selection", func(t *testing.T) {
		selected := "dept-context"
		request := httptest.NewRequest("GET", "/ws/conversations?departmentId=dept-query", nil)
		request = request.WithContext(context.WithValue(
			request.Context(),
			middleware.DepartmentFilterContextKey,
			&workspace_department.DepartmentFilter{SelectedDepartmentID: &selected},
		))

		if got := resolveConnectionDepartmentID(request); got != "dept-query" {
			t.Fatalf("resolveConnectionDepartmentID() = %q, want %q", got, "dept-query")
		}
	})

	t.Run("selected department from middleware is used", func(t *testing.T) {
		selected := "dept-selected"
		request := httptest.NewRequest("GET", "/ws/conversations", nil)
		request = request.WithContext(context.WithValue(
			request.Context(),
			middleware.DepartmentFilterContextKey,
			&workspace_department.DepartmentFilter{SelectedDepartmentID: &selected},
		))

		if got := resolveConnectionDepartmentID(request); got != selected {
			t.Fatalf("resolveConnectionDepartmentID() = %q, want %q", got, selected)
		}
	})

	t.Run("single department membership auto selects", func(t *testing.T) {
		request := httptest.NewRequest("GET", "/ws/conversations", nil)
		request = request.WithContext(context.WithValue(
			request.Context(),
			middleware.DepartmentFilterContextKey,
			&workspace_department.DepartmentFilter{DepartmentIDs: []string{"dept-only"}},
		))

		if got := resolveConnectionDepartmentID(request); got != "dept-only" {
			t.Fatalf("resolveConnectionDepartmentID() = %q, want %q", got, "dept-only")
		}
	})

	t.Run("multiple departments without selection stays empty", func(t *testing.T) {
		request := httptest.NewRequest("GET", "/ws/conversations", nil)
		request = request.WithContext(context.WithValue(
			request.Context(),
			middleware.DepartmentFilterContextKey,
			&workspace_department.DepartmentFilter{DepartmentIDs: []string{"dept-a", "dept-b"}},
		))

		if got := resolveConnectionDepartmentID(request); got != "" {
			t.Fatalf("resolveConnectionDepartmentID() = %q, want empty string", got)
		}
	})
}
