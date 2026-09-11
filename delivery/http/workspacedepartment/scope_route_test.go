package workspacedepartment

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
	dept "vozko/domain/workspace/workspace_department"
	"vozko/infra/http/middleware"
)

// withFilter puts a resolved department filter on the request the way the
// department middleware does for every protected route.
func withFilter(r *http.Request, f *dept.DepartmentFilter) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), middleware.DepartmentFilterContextKey, f))
}

func scopeOf(t *testing.T, f *dept.DepartmentFilter) dept.Scope {
	t.Helper()
	rec := httptest.NewRecorder()
	req := withFilter(httptest.NewRequest(http.MethodGet, "/departments/scope", nil), f)

	(&WorkspaceDepartmentHandler{}).MyScope(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var scope dept.Scope
	if err := json.Unmarshal(rec.Body.Bytes(), &scope); err != nil {
		t.Fatalf("decode: %v (body %s)", err, rec.Body.String())
	}
	return scope
}

// The member who most needs this answer is the one with the least access, so
// the endpoint must work for them and must still say nothing about anyone else.
func TestMyScopeTellsABlockedMemberWhyTheySeeNothing(t *testing.T) {
	got := scopeOf(t, &dept.DepartmentFilter{WorkspaceHasDepartments: true})

	if !got.BlockedByMissingDepartment {
		t.Error("a member in no department was not told that is why their screen is empty")
	}
	if !got.WorkspaceUsesDepartments || !got.RestrictedToOwnDepartments {
		t.Errorf("scope = %+v, want the department rule reported as in force", got)
	}
	if got.MemberDepartmentCount != 0 {
		t.Errorf("member department count = %d, want 0", got.MemberDepartmentCount)
	}
}

// An admin is never scoped, so there is nothing to explain to them about their
// own visibility, and an "ask someone to add you to a department" message on an
// admin's screen would be nonsense.
func TestMyScopeSaysNothingIsWrongForAnAdmin(t *testing.T) {
	got := scopeOf(t, &dept.DepartmentFilter{IsOwnerOrAdmin: true, WorkspaceHasDepartments: true})
	if got.BlockedByMissingDepartment || got.RestrictedToOwnDepartments {
		t.Errorf("scope = %+v, want an admin reported as unrestricted", got)
	}
}

// A workspace that does not use departments must report nothing at all, so the
// explanation never appears where it would be noise.
func TestMyScopeIsSilentWithoutDepartments(t *testing.T) {
	if got := (scopeOf(t, &dept.DepartmentFilter{})); got != (dept.Scope{}) {
		t.Errorf("scope = %+v, want everything false", got)
	}
}

// The response is the caller's own facts. Department names and ids must never
// appear in it: this is the one department endpoint a member with no
// departments:read can reach.
func TestMyScopeLeaksNoDepartmentIdentity(t *testing.T) {
	rec := httptest.NewRecorder()
	req := withFilter(httptest.NewRequest(http.MethodGet, "/departments/scope", nil), &dept.DepartmentFilter{
		WorkspaceHasDepartments: true,
		DepartmentIDs:           []string{"dept-confidential-a", "dept-confidential-b"},
	})

	(&WorkspaceDepartmentHandler{}).MyScope(rec, req)

	if body := rec.Body.String(); strings.Contains(body, "dept-confidential") {
		t.Errorf("the scope response carried department identity: %s", body)
	}
}

// Registration order decides whether this route exists at all: /departments/{id}
// is greedy and would answer "scope" as a department id, which for a member
// without departments:read is a 403 on the one endpoint meant to explain their
// empty screen.
func TestScopeRouteIsNotSwallowedByTheIdRoute(t *testing.T) {
	router := mux.NewRouter()
	reached := ""
	handler := &WorkspaceDepartmentHandler{}
	RegisterRoutes(router, handler, func(_ workspace_domain.Resource, _ workspace_domain.Action, next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			reached = "gated"
			next(w, r)
		}
	})

	req := withFilter(httptest.NewRequest(http.MethodGet, "/departments/scope", nil), &dept.DepartmentFilter{})
	router.ServeHTTP(httptest.NewRecorder(), req)

	if reached == "gated" {
		t.Error("/departments/scope was matched by the permission-gated {id} route")
	}
}
