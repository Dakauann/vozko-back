package http

import (
	"context"
	"errors"
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	workspace_domain "vozko/domain/workspace"
	"vozko/infra/http/middleware"
)

type recordingAccess struct {
	asked []string
	grant map[string]bool
}

func (a *recordingAccess) Execute(_, _ string, resource workspace_domain.Resource, action workspace_domain.Action) error {
	key := string(resource) + ":" + string(action)
	a.asked = append(a.asked, key)
	if a.grant[key] {
		return nil
	}
	return errors.New("forbidden")
}

func callSessionGate(t *testing.T, grant map[string]bool) (*recordingAccess, int) {
	t.Helper()
	access := &recordingAccess{grant: grant}
	r := &router{workspaceMiddleware: middleware.NewWorkspaceMiddleware(access, nil, nil)}
	mr := mux.NewRouter()
	r.setupConversationRoutes(mr)

	request := httptest.NewRequest(nethttp.MethodGet, "/ws/call-session?workspace_id=ws-1", nil)
	request = request.WithContext(context.WithValue(request.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: "operator", Role: "user"}))
	recorder := httptest.NewRecorder()
	mr.ServeHTTP(recorder, request)
	return access, recorder.Code
}

func TestTheCallSocketOpensWithCallSessionUseAlone(t *testing.T) {
	access, code := callSessionGate(t, map[string]bool{"call_session:use": true})
	if len(access.asked) != 1 || access.asked[0] != "call_session:use" {
		t.Fatalf("the socket asked for %v, want call_session:use only", access.asked)
	}
	if code == nethttp.StatusForbidden {
		t.Fatal("call_session:use must be enough to reach the call socket")
	}
}

func TestTheCallSocketNoLongerOpensWithConversationsUpdate(t *testing.T) {
	_, code := callSessionGate(t, map[string]bool{"conversations:update": true})
	if code != nethttp.StatusForbidden {
		t.Fatalf("status = %d, want 403 for a member who may edit conversations but not use calls", code)
	}
}
