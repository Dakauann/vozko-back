package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	dept "vozko/domain/workspace/workspace_department"
	"vozko/infra/http/middleware"
)

func requestWithDepartmentFilter(filter *dept.DepartmentFilter) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	if filter != nil {
		ctx := context.WithValue(req.Context(), middleware.DepartmentFilterContextKey, filter)
		req = req.WithContext(ctx)
	}
	return req
}

func TestDepartmentFilterIDs_NilFilter(t *testing.T) {
	ids := departmentFilterIDs(requestWithDepartmentFilter(nil))
	if ids != nil {
		t.Fatalf("expected nil, got %v", ids)
	}
}

func TestDepartmentFilterIDs_AdminNoSelection(t *testing.T) {
	f := &dept.DepartmentFilter{IsOwnerOrAdmin: true}
	ids := departmentFilterIDs(requestWithDepartmentFilter(f))
	if len(ids) != 0 {
		t.Fatalf("expected empty for admin without selection, got %v", ids)
	}
}

func TestDepartmentFilterIDs_AdminWithSelection(t *testing.T) {
	sel := "dept-1"
	f := &dept.DepartmentFilter{IsOwnerOrAdmin: true, SelectedDepartmentID: &sel}
	ids := departmentFilterIDs(requestWithDepartmentFilter(f))
	if len(ids) != 1 || ids[0] != "dept-1" {
		t.Fatalf("expected [dept-1], got %v", ids)
	}
}

func TestDepartmentFilterIDs_MemberAllDepartments(t *testing.T) {
	f := &dept.DepartmentFilter{DepartmentIDs: []string{"dept-a", "dept-b"}}
	ids := departmentFilterIDs(requestWithDepartmentFilter(f))
	if len(ids) != 2 || ids[0] != "dept-a" || ids[1] != "dept-b" {
		t.Fatalf("expected [dept-a, dept-b], got %v", ids)
	}
}

func TestDepartmentFilterIDs_MemberWithSelection(t *testing.T) {
	sel := "dept-b"
	f := &dept.DepartmentFilter{DepartmentIDs: []string{"dept-a", "dept-b"}, SelectedDepartmentID: &sel}
	ids := departmentFilterIDs(requestWithDepartmentFilter(f))
	if len(ids) != 1 || ids[0] != "dept-b" {
		t.Fatalf("expected [dept-b], got %v", ids)
	}
}

func TestShouldReturnEmptyDepartmentList_MemberNoDepartmentsInScopedWorkspace(t *testing.T) {
	f := &dept.DepartmentFilter{WorkspaceHasDepartments: true}
	if !shouldReturnEmptyDepartmentList(requestWithDepartmentFilter(f)) {
		t.Fatal("expected empty list for member with no departments in department-aware workspace")
	}
}

func TestShouldReturnEmptyDepartmentList_UnscopedWorkspace(t *testing.T) {
	f := &dept.DepartmentFilter{}
	if shouldReturnEmptyDepartmentList(requestWithDepartmentFilter(f)) {
		t.Fatal("workspace without departments should not force empty lists")
	}
}

func TestCanAccessDepartment_EmptyResourceDeptDeniedForScopedMember(t *testing.T) {
	f := &dept.DepartmentFilter{DepartmentIDs: []string{"dept-a"}}
	if canAccessDepartment(requestWithDepartmentFilter(f), "") {
		t.Fatal("member in department-aware workspace should not access unscoped resources")
	}
}

func TestCanAccessDepartment_EmptyResourceDeptAllowedWhenWorkspaceHasNoDepartments(t *testing.T) {
	f := &dept.DepartmentFilter{}
	if !canAccessDepartment(requestWithDepartmentFilter(f), "") {
		t.Fatal("workspace without departments should allow unscoped resources")
	}
}

func TestCanAccessDepartment_NilFilter(t *testing.T) {
	if !canAccessDepartment(requestWithDepartmentFilter(nil), "dept-x") {
		t.Fatal("should allow when filter is nil (no middleware)")
	}
}

func TestCanAccessDepartment_AdminUnrestricted(t *testing.T) {
	f := &dept.DepartmentFilter{IsOwnerOrAdmin: true}
	if !canAccessDepartment(requestWithDepartmentFilter(f), "dept-x") {
		t.Fatal("admin without selection should have unrestricted access")
	}
}

func TestCanAccessDepartment_AdminWithMatchingSelection(t *testing.T) {
	sel := "dept-1"
	f := &dept.DepartmentFilter{IsOwnerOrAdmin: true, SelectedDepartmentID: &sel}
	if !canAccessDepartment(requestWithDepartmentFilter(f), "dept-1") {
		t.Fatal("admin with matching selection should have access")
	}
}

func TestCanAccessDepartment_AdminWithNonMatchingSelection(t *testing.T) {
	sel := "dept-1"
	f := &dept.DepartmentFilter{IsOwnerOrAdmin: true, SelectedDepartmentID: &sel}
	if canAccessDepartment(requestWithDepartmentFilter(f), "dept-2") {
		t.Fatal("admin with selection dept-1 should NOT see dept-2 resource")
	}
}

func TestCanAccessDepartment_AdminWithSelectionCannotAccessUnscoped(t *testing.T) {
	sel := "dept-1"
	f := &dept.DepartmentFilter{IsOwnerOrAdmin: true, SelectedDepartmentID: &sel}
	if canAccessDepartment(requestWithDepartmentFilter(f), "") {
		t.Fatal("admin with department selection should not see unscoped resource")
	}
}

func TestCanAccessDepartment_MemberWithAccess(t *testing.T) {
	f := &dept.DepartmentFilter{DepartmentIDs: []string{"dept-a", "dept-b"}}
	if !canAccessDepartment(requestWithDepartmentFilter(f), "dept-b") {
		t.Fatal("member in dept-b should access dept-b resource")
	}
}

func TestCanAccessDepartment_MemberDenied(t *testing.T) {
	f := &dept.DepartmentFilter{DepartmentIDs: []string{"dept-a"}}
	if canAccessDepartment(requestWithDepartmentFilter(f), "dept-b") {
		t.Fatal("member in dept-a should NOT access dept-b resource")
	}
}

func TestCanAccessDepartment_MemberNoDepartments(t *testing.T) {
	f := &dept.DepartmentFilter{WorkspaceHasDepartments: true}
	if canAccessDepartment(requestWithDepartmentFilter(f), "dept-x") {
		t.Fatal("member with no departments in department-aware workspace should not access scoped resources")
	}
	if canAccessDepartment(requestWithDepartmentFilter(f), "") {
		t.Fatal("member with no departments in department-aware workspace should not access unscoped resources")
	}
}
