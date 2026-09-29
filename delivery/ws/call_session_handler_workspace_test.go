package ws

import (
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gorilla/websocket"

	"vozko/domain/auth"
	"vozko/domain/conversation"
	"vozko/infra/http/middleware"
)

type workspaceScopedAuthorizer struct {
	granted map[string]bool
}

func (a workspaceScopedAuthorizer) HasWorkspacePermission(userID, workspaceID, resource, action string, _ bool) bool {
	return a.granted[userID+"|"+workspaceID+"|"+resource+"|"+action]
}
func (workspaceScopedAuthorizer) CanAccessEntry(_, _, _, _ string, _ bool) bool    { return true }
func (workspaceScopedAuthorizer) CanAccessCampaign(_, _, _, _ string, _ bool) bool { return true }
func (workspaceScopedAuthorizer) GetAccessibleEntryIDs(_, _ string, _ bool) []string {
	return nil
}
func (workspaceScopedAuthorizer) GetDepartmentScope(_, _ string, _ bool) (conversation.DepartmentAccessScope, bool) {
	return conversation.DepartmentAccessScope{}, true
}
func (workspaceScopedAuthorizer) IsWorkspaceMember(_, _ string) bool       { return true }
func (workspaceScopedAuthorizer) IsWorkspaceOwnerOrAdmin(_, _ string) bool { return false }

func dialScopedCallSession(t *testing.T, authorizer workspaceScopedAuthorizer, resolvedWorkspace, query string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	h := NewCallSessionWSHandler(&fakeStartUseCase{}, &fakeEndUseCase{}, nil, authorizer, log.Default(), noopWSMetricsRecorder{}).
		WithRegistries(&stubSessionRegistry{}, &stubCallRegistry{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: "member", Role: "user"})
		ctx = context.WithValue(ctx, middleware.WorkspaceIDContextKey, resolvedWorkspace)
		h.HandleWebSocket(w, r.WithContext(ctx))
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	u.Scheme = "ws"
	u.RawQuery = query
	conn, res, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if conn != nil {
		t.Cleanup(func() { _ = conn.Close() })
	}
	return conn, res, err
}

func TestCallSessionWSRefusesAWorkspaceTheMemberCannotCallIn(t *testing.T) {
	authorizer := workspaceScopedAuthorizer{granted: map[string]bool{"member|ws-a|call_session|use": true}}

	if _, res, err := dialScopedCallSession(t, authorizer, "ws-a", "workspaceId=ws-b"); err == nil || res == nil || res.StatusCode != http.StatusForbidden {
		t.Fatalf("joining ws-b while authorised for ws-a = %v (status %v), want 403", err, statusOf(res))
	}
	if _, _, err := dialScopedCallSession(t, authorizer, "ws-a", "workspaceId=ws-a"); err != nil {
		t.Fatalf("joining the authorised workspace failed: %v", err)
	}
}

func TestCallSessionWSRefusesMembersWithoutCallingPermission(t *testing.T) {
	authorizer := workspaceScopedAuthorizer{granted: map[string]bool{}}
	if _, res, err := dialScopedCallSession(t, authorizer, "ws-a", ""); err == nil || res == nil || res.StatusCode != http.StatusForbidden {
		t.Fatalf("connecting without call_session:use = %v (status %v), want 403", err, statusOf(res))
	}
}

func statusOf(res *http.Response) int {
	if res == nil {
		return 0
	}
	return res.StatusCode
}
