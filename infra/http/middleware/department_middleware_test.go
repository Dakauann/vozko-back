package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"vozko/domain/auth"
	"vozko/domain/workspace"
	dept "vozko/domain/workspace/workspace_department"
)

type stubDepartmentResolver struct {
	departmentIDs []string
	departments   map[string]*dept.Department
	err           error
}

func (s *stubDepartmentResolver) ListDepartments(workspaceID string) ([]dept.Department, error) {
	if s.err != nil {
		return nil, s.err
	}
	result := make([]dept.Department, 0)
	for _, department := range s.departments {
		if department != nil && department.WorkspaceID == workspaceID {
			result = append(result, *department)
		}
	}
	return result, nil
}

func (s *stubDepartmentResolver) GetMemberDepartmentIDs(workspaceID, userID string) ([]string, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.departmentIDs, nil
}

func (s *stubDepartmentResolver) GetDepartmentByID(id string) (*dept.Department, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.departments[id], nil
}

type stubWorkspaceMembershipChecker struct {
	member *workspace.Member
	err    error
}

func (s *stubWorkspaceMembershipChecker) GetMember(workspaceID, userID string) (*workspace.Member, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.member, nil
}

func TestResolveDepartment_WorkspaceOwnerBypassesMembershipFilter(t *testing.T) {
	middleware := NewDepartmentMiddleware(
		&stubDepartmentResolver{},
		&stubWorkspaceMembershipChecker{
			member: &workspace.Member{Role: workspace.RoleOwner},
		},
	)

	req := httptest.NewRequest(http.MethodGet, "/departments", nil)
	ctx := context.WithValue(req.Context(), ClaimsContextKey, &auth.Claims{UserID: "user-1", Role: "member"})
	ctx = context.WithValue(ctx, WorkspaceIDContextKey, "ws-1")
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	var filter *dept.DepartmentFilter
	handler := middleware.ResolveDepartment()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		filter = GetDepartmentFilter(r)
		w.WriteHeader(http.StatusNoContent)
	}))

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", rr.Code)
	}
	if filter == nil {
		t.Fatal("expected department filter in context")
	}
	if !filter.IsOwnerOrAdmin {
		t.Fatal("expected workspace owner to bypass department filtering")
	}
	if len(filter.DepartmentIDs) != 0 {
		t.Fatalf("expected no department membership lookup for owner, got %d ids", len(filter.DepartmentIDs))
	}
}

func TestResolveDepartment_PrivilegedUserKeepsSelectedDepartment(t *testing.T) {
	middleware := NewDepartmentMiddleware(
		&stubDepartmentResolver{
			departments: map[string]*dept.Department{
				"dept-1": {ID: "dept-1", WorkspaceID: "ws-1", Name: "Sales"},
			},
		},
		&stubWorkspaceMembershipChecker{
			member: &workspace.Member{Role: workspace.RoleOwner},
		},
	)

	req := httptest.NewRequest(http.MethodGet, "/departments", nil)
	req.Header.Set("X-Department-ID", "dept-1")
	ctx := context.WithValue(req.Context(), ClaimsContextKey, &auth.Claims{UserID: "user-1", Role: "member"})
	ctx = context.WithValue(ctx, WorkspaceIDContextKey, "ws-1")
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	var filter *dept.DepartmentFilter
	handler := middleware.ResolveDepartment()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		filter = GetDepartmentFilter(r)
		w.WriteHeader(http.StatusNoContent)
	}))

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", rr.Code)
	}
	if filter == nil || filter.SelectedDepartmentID == nil {
		t.Fatal("expected selected department to be preserved for privileged user")
	}
	if *filter.SelectedDepartmentID != "dept-1" {
		t.Fatalf("expected selected department dept-1, got %q", *filter.SelectedDepartmentID)
	}
}

func TestResolveDepartment_RegularMemberGetsDepartmentIDs(t *testing.T) {
	middleware := NewDepartmentMiddleware(
		&stubDepartmentResolver{
			departmentIDs: []string{"dept-a", "dept-b"},
			departments: map[string]*dept.Department{
				"dept-a": {ID: "dept-a", WorkspaceID: "ws-1", Name: "Sales"},
				"dept-b": {ID: "dept-b", WorkspaceID: "ws-1", Name: "Support"},
			},
		},
		&stubWorkspaceMembershipChecker{
			member: &workspace.Member{Role: workspace.RoleMember},
		},
	)

	req := httptest.NewRequest(http.MethodGet, "/campaigns", nil)
	ctx := context.WithValue(req.Context(), ClaimsContextKey, &auth.Claims{UserID: "user-1", Role: "user"})
	ctx = context.WithValue(ctx, WorkspaceIDContextKey, "ws-1")
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	var filter *dept.DepartmentFilter
	handler := middleware.ResolveDepartment()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		filter = GetDepartmentFilter(r)
		w.WriteHeader(http.StatusNoContent)
	}))

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", rr.Code)
	}
	if filter == nil {
		t.Fatal("expected department filter in context")
	}
	if filter.IsOwnerOrAdmin {
		t.Fatal("regular member should not have admin bypass")
	}
	if len(filter.DepartmentIDs) != 2 {
		t.Fatalf("expected 2 department IDs, got %d", len(filter.DepartmentIDs))
	}

	if !filter.ShouldFilter() {
		t.Fatal("expected ShouldFilter to be true for regular member with departments")
	}

	ids := filter.EffectiveDepartmentIDs()
	if len(ids) != 2 || ids[0] != "dept-a" || ids[1] != "dept-b" {
		t.Fatalf("expected [dept-a, dept-b], got %v", ids)
	}
}

func TestResolveDepartment_MemberWithSelectedDepartment(t *testing.T) {
	middleware := NewDepartmentMiddleware(
		&stubDepartmentResolver{
			departmentIDs: []string{"dept-a", "dept-b"},
			departments: map[string]*dept.Department{
				"dept-a": {ID: "dept-a", WorkspaceID: "ws-1", Name: "Sales"},
				"dept-b": {ID: "dept-b", WorkspaceID: "ws-1", Name: "Support"},
			},
		},
		&stubWorkspaceMembershipChecker{
			member: &workspace.Member{Role: workspace.RoleMember},
		},
	)

	req := httptest.NewRequest(http.MethodGet, "/campaigns", nil)
	req.Header.Set("X-Department-ID", "dept-b")
	ctx := context.WithValue(req.Context(), ClaimsContextKey, &auth.Claims{UserID: "user-1", Role: "user"})
	ctx = context.WithValue(ctx, WorkspaceIDContextKey, "ws-1")
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	var filter *dept.DepartmentFilter
	handler := middleware.ResolveDepartment()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		filter = GetDepartmentFilter(r)
		w.WriteHeader(http.StatusNoContent)
	}))

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", rr.Code)
	}
	if filter == nil || filter.SelectedDepartmentID == nil {
		t.Fatal("expected selected department to be set")
	}
	if *filter.SelectedDepartmentID != "dept-b" {
		t.Fatalf("expected dept-b, got %q", *filter.SelectedDepartmentID)
	}

	ids := filter.EffectiveDepartmentIDs()
	if len(ids) != 1 || ids[0] != "dept-b" {
		t.Fatalf("expected [dept-b], got %v", ids)
	}
}

func TestResolveDepartment_MemberSelectingWrongDepartmentIs403(t *testing.T) {
	middleware := NewDepartmentMiddleware(
		&stubDepartmentResolver{
			departmentIDs: []string{"dept-a"},
			departments: map[string]*dept.Department{
				"dept-a": {ID: "dept-a", WorkspaceID: "ws-1", Name: "Sales"},
			},
		},
		&stubWorkspaceMembershipChecker{
			member: &workspace.Member{Role: workspace.RoleMember},
		},
	)

	req := httptest.NewRequest(http.MethodGet, "/campaigns", nil)
	req.Header.Set("X-Department-ID", "dept-not-mine")
	ctx := context.WithValue(req.Context(), ClaimsContextKey, &auth.Claims{UserID: "user-1", Role: "user"})
	ctx = context.WithValue(ctx, WorkspaceIDContextKey, "ws-1")
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	handler := middleware.ResolveDepartment()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be reached — expected 403")
	}))

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d", rr.Code)
	}
}

func TestResolveDepartment_WorkspaceAdminIsPrivileged(t *testing.T) {
	middleware := NewDepartmentMiddleware(
		&stubDepartmentResolver{},
		&stubWorkspaceMembershipChecker{
			member: &workspace.Member{Role: workspace.RoleAdmin},
		},
	)

	req := httptest.NewRequest(http.MethodGet, "/campaigns", nil)
	ctx := context.WithValue(req.Context(), ClaimsContextKey, &auth.Claims{UserID: "user-1", Role: "user"})
	ctx = context.WithValue(ctx, WorkspaceIDContextKey, "ws-1")
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	var filter *dept.DepartmentFilter
	handler := middleware.ResolveDepartment()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		filter = GetDepartmentFilter(r)
		w.WriteHeader(http.StatusNoContent)
	}))

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", rr.Code)
	}
	if filter == nil {
		t.Fatal("expected filter in context")
	}
	if !filter.IsOwnerOrAdmin {
		t.Fatal("workspace admin should have IsOwnerOrAdmin = true")
	}

	ids := filter.EffectiveDepartmentIDs()
	if ids != nil && len(ids) != 0 {
		t.Fatalf("expected nil or empty EffectiveDepartmentIDs for admin without selection, got %v", ids)
	}
}

func TestResolveDepartment_MemberWithNoDepartmentsInDepartmentAwareWorkspace(t *testing.T) {
	middleware := NewDepartmentMiddleware(
		&stubDepartmentResolver{
			departmentIDs: nil,
			departments: map[string]*dept.Department{
				"dept-a": {ID: "dept-a", WorkspaceID: "ws-1", Name: "Sales"},
			},
		},
		&stubWorkspaceMembershipChecker{
			member: &workspace.Member{Role: workspace.RoleMember},
		},
	)

	req := httptest.NewRequest(http.MethodGet, "/campaigns", nil)
	ctx := context.WithValue(req.Context(), ClaimsContextKey, &auth.Claims{UserID: "user-1", Role: "user"})
	ctx = context.WithValue(ctx, WorkspaceIDContextKey, "ws-1")
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	var filter *dept.DepartmentFilter
	handler := middleware.ResolveDepartment()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		filter = GetDepartmentFilter(r)
		w.WriteHeader(http.StatusNoContent)
	}))

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", rr.Code)
	}
	if filter == nil {
		t.Fatal("expected filter in context")
	}
	if filter.IsOwnerOrAdmin {
		t.Fatal("should not be admin")
	}
	if !filter.ShouldFilter() {
		t.Fatal("department-aware workspace should keep filtering enabled")
	}
	if ids := filter.EffectiveDepartmentIDs(); len(ids) != 0 {
		t.Fatalf("expected empty effective departments, got %v", ids)
	}
	if !filter.WorkspaceHasDepartments {
		t.Fatal("expected workspace to be marked as department-aware")
	}
}

func TestResolveDepartment_MemberWithNoDepartmentsInWorkspaceWithoutDepartments(t *testing.T) {
	middleware := NewDepartmentMiddleware(
		&stubDepartmentResolver{
			departmentIDs: nil,
		},
		&stubWorkspaceMembershipChecker{
			member: &workspace.Member{Role: workspace.RoleMember},
		},
	)

	req := httptest.NewRequest(http.MethodGet, "/campaigns", nil)
	ctx := context.WithValue(req.Context(), ClaimsContextKey, &auth.Claims{UserID: "user-1", Role: "user"})
	ctx = context.WithValue(ctx, WorkspaceIDContextKey, "ws-1")
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	var filter *dept.DepartmentFilter
	handler := middleware.ResolveDepartment()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		filter = GetDepartmentFilter(r)
		w.WriteHeader(http.StatusNoContent)
	}))

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", rr.Code)
	}
	if filter == nil {
		t.Fatal("expected filter in context")
	}
	if filter.ShouldFilter() {
		t.Fatal("workspace without departments should not apply department filtering")
	}
}
