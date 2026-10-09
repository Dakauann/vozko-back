package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"vozko/domain/auth"
	"vozko/domain/conversation"
	"vozko/infra/http/middleware"
	lead_usecase "vozko/usecases/lead"
)

func requestAs(claims *auth.Claims, workspaceID string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := request.Context()
	if claims != nil {
		ctx = context.WithValue(ctx, middleware.ClaimsContextKey, claims)
	}
	if workspaceID != "" {
		ctx = context.WithValue(ctx, middleware.WorkspaceIDContextKey, workspaceID)
	}
	return request.WithContext(ctx)
}

func TestLeadActorReadsTheCallerOfTheRequest(t *testing.T) {
	cases := []struct {
		name    string
		request *http.Request
		want    lead_usecase.Actor
		ok      bool
	}{
		{name: "a member", request: requestAs(&auth.Claims{UserID: " ana ", Role: "user"}, "ws-1"), want: lead_usecase.Actor{UserID: "ana", WorkspaceID: "ws-1"}, ok: true},
		{name: "a platform admin", request: requestAs(&auth.Claims{UserID: "root", Role: "admin"}, "ws-1"), want: lead_usecase.Actor{UserID: "root", WorkspaceID: "ws-1", IsAdmin: true}, ok: true},
		{name: "no workspace", request: requestAs(&auth.Claims{UserID: "ana", Role: "user"}, "")},
		{name: "no claims", request: requestAs(nil, "ws-1")},
		{name: "no user", request: requestAs(&auth.Claims{UserID: " ", Role: "admin"}, "ws-1")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := LeadActor(tc.request)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("LeadActor = %+v, %v, want %+v, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestWorkspaceActorIsTheOneActorShapeOfEveryWorkspaceUseCase(t *testing.T) {
	got, ok := WorkspaceActor(requestAs(&auth.Claims{UserID: " ana ", Role: "admin"}, "ws-1"))
	if !ok || got != (conversation.Viewer{UserID: "ana", WorkspaceID: "ws-1", IsAdmin: true}) {
		t.Fatalf("WorkspaceActor = %+v, %v", got, ok)
	}
	if _, ok := WorkspaceActor(requestAs(&auth.Claims{UserID: "", Role: "user"}, "ws-1")); ok {
		t.Fatal("a request without a user must not build an actor")
	}
	if _, ok := WorkspaceActor(requestAs(&auth.Claims{UserID: "ana", Role: "user"}, " ")); ok {
		t.Fatal("a request without a workspace must not build an actor")
	}
}
