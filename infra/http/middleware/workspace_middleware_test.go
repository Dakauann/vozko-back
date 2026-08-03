package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"vozko/domain/auth"
	"vozko/domain/workspace"
)

type stubMembershipChecker struct {
	member *workspace.Member
	err    error
}

func (s *stubMembershipChecker) GetMember(string, string) (*workspace.Member, error) {
	return s.member, s.err
}

type stubDefaultResolver struct {
	ws  *workspace.Workspace
	err error
}

func (s *stubDefaultResolver) Execute(string, string, string) (*workspace.Workspace, error) {
	return s.ws, s.err
}

// Reproduces the production incident: a user (two accounts) sends a stale/foreign
// X-Workspace-ID left in a cookie from another account. The middleware must NOT
// 403 the whole session, it must ignore the invalid id and fall back to the
// user's default workspace, so workspace-agnostic endpoints (pending invites)
// keep working.
func TestResolveWorkspace_StaleWorkspace_FallsBackToDefault_NoBrick(t *testing.T) {
	membership := &stubMembershipChecker{member: nil} // NOT a member of requested ws
	resolver := &stubDefaultResolver{ws: &workspace.Workspace{ID: "default-ws"}}
	mw := NewWorkspaceMiddleware(nil, membership, resolver)

	req := httptest.NewRequest(http.MethodGet, "/workspaces/invites", nil)
	req.Header.Set("X-Workspace-ID", "stale-foreign-ws")
	ctx := context.WithValue(req.Context(), ClaimsContextKey,
		&auth.Claims{UserID: "user-1", Email: "u@e.com", Role: "user"})
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	reached := false
	var resolved string
	handler := mw.ResolveWorkspace()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		resolved = GetWorkspaceID(r)
		w.WriteHeader(http.StatusNoContent)
	}))
	handler.ServeHTTP(rr, req)

	if rr.Code == http.StatusForbidden {
		t.Fatal("stale workspace must NOT 403, that bricks the whole session")
	}
	if !reached {
		t.Fatal("handler must be reached (request not blocked)")
	}
	if resolved != "default-ws" {
		t.Fatalf("expected fallback to default workspace, got %q", resolved)
	}
}

func TestResolveWorkspace_MemberWorkspace_PassesThrough(t *testing.T) {
	membership := &stubMembershipChecker{member: &workspace.Member{ID: "m1"}}
	mw := NewWorkspaceMiddleware(nil, membership, nil)

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-Workspace-ID", "my-ws")
	ctx := context.WithValue(req.Context(), ClaimsContextKey,
		&auth.Claims{UserID: "user-1", Role: "user"})
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	var resolved string
	handler := mw.ResolveWorkspace()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resolved = GetWorkspaceID(r)
		w.WriteHeader(http.StatusNoContent)
	}))
	handler.ServeHTTP(rr, req)

	if resolved != "my-ws" {
		t.Fatalf("member workspace should pass through, got %q", resolved)
	}
}

// A real DB error verifying membership must still fail closed (403), NOT fall back.
func TestResolveWorkspace_MembershipCheckError_Still403(t *testing.T) {
	membership := &stubMembershipChecker{err: errors.New("db down")}
	mw := NewWorkspaceMiddleware(nil, membership, &stubDefaultResolver{ws: &workspace.Workspace{ID: "default-ws"}})

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-Workspace-ID", "some-ws")
	ctx := context.WithValue(req.Context(), ClaimsContextKey,
		&auth.Claims{UserID: "user-1", Role: "user"})
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	handler := mw.ResolveWorkspace()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("membership-check DB error must fail closed with 403, got %d", rr.Code)
	}
}

func TestResolveWorkspace_UsesWorkspaceIDQueryParam(t *testing.T) {
	middleware := NewWorkspaceMiddleware(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/oauth/meta/embedded?workspace_id=ws-123", nil)
	ctx := context.WithValue(req.Context(), ClaimsContextKey, &auth.Claims{UserID: "user-1", Role: "admin"})
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	var resolvedWorkspaceID string
	handler := middleware.ResolveWorkspace()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resolvedWorkspaceID = GetWorkspaceID(r)
		w.WriteHeader(http.StatusNoContent)
	}))

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", rr.Code)
	}
	if resolvedWorkspaceID != "ws-123" {
		t.Fatalf("expected workspace ws-123, got %q", resolvedWorkspaceID)
	}
}

func TestExtractReferralCode_HeaderPreferred(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-Affiliate-Ref", "HDRCODE")
	req.AddCookie(&http.Cookie{Name: "affiliate_ref", Value: "COOKIECODE"})

	if got := ExtractReferralCode(req); got != "HDRCODE" {
		t.Fatalf("expected HDRCODE (header beats cookie), got %q", got)
	}
}

func TestExtractReferralCode_CookieFallback(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.AddCookie(&http.Cookie{Name: "affiliate_ref", Value: "  COOKIECODE  "})

	if got := ExtractReferralCode(req); got != "COOKIECODE" {
		t.Fatalf("expected trimmed cookie COOKIECODE, got %q", got)
	}
}

func TestExtractReferralCode_Empty(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	if got := ExtractReferralCode(req); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestExtractReferralCode_NilRequest(t *testing.T) {
	if got := ExtractReferralCode(nil); got != "" {
		t.Fatalf("expected empty for nil request, got %q", got)
	}
}

func TestExtractReferralCode_HeaderWhitespaceOnly_FallsBackToCookie(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-Affiliate-Ref", "   ")
	req.AddCookie(&http.Cookie{Name: "affiliate_ref", Value: "COOKIE"})
	if got := ExtractReferralCode(req); got != "COOKIE" {
		t.Fatalf("expected cookie fallback when header is whitespace, got %q", got)
	}
}
