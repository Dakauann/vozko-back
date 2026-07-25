package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"vozko/domain/auth"
)

type authTestVerifier struct {
	lastToken string
}

func (m *authTestVerifier) Verify(token string) (*auth.Claims, error) {
	m.lastToken = token
	return &auth.Claims{UserID: "user-1", Role: "admin"}, nil
}

func TestAuthenticate_UsesAccessTokenCookie(t *testing.T) {
	verifier := &authTestVerifier{}
	middleware := NewAuthMiddleware(verifier, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/oauth/meta/embedded", nil)
	req.AddCookie(&http.Cookie{Name: "accessToken", Value: "cookie-token"})
	rr := httptest.NewRecorder()

	handler := middleware.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := GetClaims(r)
		if claims == nil || claims.UserID != "user-1" {
			t.Fatalf("expected authenticated claims, got %+v", claims)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", rr.Code)
	}
	if verifier.lastToken != "cookie-token" {
		t.Fatalf("expected verifier to receive cookie token, got %q", verifier.lastToken)
	}
}
