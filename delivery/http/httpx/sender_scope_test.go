package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vozko/domain/auth"
	"vozko/domain/conversation"
	"vozko/infra/http/middleware"
)

type scopeResolver struct {
	scope   conversation.DepartmentAccessScope
	allowed bool
	asked   string
}

func (s *scopeResolver) GetDepartmentScope(userID, workspaceID string, isAdmin bool) (conversation.DepartmentAccessScope, bool) {
	s.asked = userID + "|" + workspaceID
	if isAdmin {
		s.asked += "|admin"
	}
	return s.scope, s.allowed
}

func scopedRequest(workspaceID string, claims *auth.Claims) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/x", nil)
	ctx := r.Context()
	if claims != nil {
		ctx = context.WithValue(ctx, middleware.ClaimsContextKey, claims)
	}
	if workspaceID != "" {
		ctx = context.WithValue(ctx, middleware.WorkspaceIDContextKey, workspaceID)
	}
	return r.WithContext(ctx)
}

func TestRequireSenderScope(t *testing.T) {
	restricted := conversation.DepartmentAccessScope{Restrict: true, DepartmentIDs: []string{"dep-1"}}
	cases := []struct {
		name     string
		ws       string
		claims   *auth.Claims
		resolver DepartmentScopeResolver
		status   int
		code     string
	}{
		{"no workspace", "", &auth.Claims{UserID: "u1"}, &scopeResolver{allowed: true}, http.StatusForbidden, "workspace_required"},
		{"no claims", "ws1", nil, &scopeResolver{allowed: true}, http.StatusForbidden, "forbidden"},
		{"no resolver fails closed", "ws1", &auth.Claims{UserID: "u1"}, nil, http.StatusInternalServerError, "scope_unavailable"},
		{"outside the workspace", "ws1", &auth.Claims{UserID: "u1"}, &scopeResolver{allowed: false}, http.StatusForbidden, "forbidden"},
		{"in scope", "ws1", &auth.Claims{UserID: "u1"}, &scopeResolver{allowed: true, scope: restricted}, 0, ""},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		scope, ok := RequireSenderScope(rec, scopedRequest(tc.ws, tc.claims), tc.resolver)
		if tc.status == 0 {
			if !ok || scope.WorkspaceID != "ws1" || scope.UserID != "u1" || scope.IsAdmin || !scope.Departments.Restrict || scope.Departments.DepartmentIDs[0] != "dep-1" {
				t.Errorf("%s: scope = %+v, ok %v", tc.name, scope, ok)
			}
			continue
		}
		if ok || rec.Code != tc.status || !strings.Contains(rec.Body.String(), `"`+tc.code+`"`) {
			t.Errorf("%s: ok %v status %d body %s", tc.name, ok, rec.Code, rec.Body.String())
		}
	}
}

func TestRequireSenderScopeAsksAsAnAdmin(t *testing.T) {
	resolver := &scopeResolver{allowed: true}
	scope, ok := RequireSenderScope(httptest.NewRecorder(), scopedRequest("ws1", &auth.Claims{UserID: "u1", Role: "admin"}), resolver)
	if !ok || !scope.IsAdmin || resolver.asked != "u1|ws1|admin" {
		t.Fatalf("scope = %+v asked %q", scope, resolver.asked)
	}
}
