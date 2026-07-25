package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"vozko/domain/auth"
)

type stubVerifier struct {
	claims *auth.Claims
	err    error
}

func (v *stubVerifier) Verify(token string) (*auth.Claims, error) {
	return v.claims, v.err
}

type stubRoleFetcher struct {
	role string
	err  error
}

func (f *stubRoleFetcher) GetUserRole(userID string) (string, error) {
	return f.role, f.err
}

type stubSharedState struct {
	store map[string]string
}

func newStubSharedState() *stubSharedState {
	return &stubSharedState{store: make(map[string]string)}
}

func (s *stubSharedState) SetNX(key, value string, ttl time.Duration) (bool, error) { return true, nil }
func (s *stubSharedState) SetString(key, value string, ttl time.Duration) error {
	s.store[key] = value
	return nil
}
func (s *stubSharedState) GetString(key string) (string, error) {
	v, ok := s.store[key]
	if !ok {
		return "", errors.New("not found")
	}
	return v, nil
}
func (s *stubSharedState) Del(keys ...string) error        { return nil }
func (s *stubSharedState) Exists(key string) (bool, error) { return false, nil }
func (s *stubSharedState) Incr(key string) (int64, error)  { return 0, nil }
func (s *stubSharedState) Decr(key string) (int64, error)  { return 0, nil }
func (s *stubSharedState) IncrWithTTL(key string, ttl time.Duration) (int64, error) {
	return 0, nil
}
func (s *stubSharedState) TryIncr(key string, max int64) (bool, error) { return true, nil }
func (s *stubSharedState) SAdd(key string, members ...string) error    { return nil }
func (s *stubSharedState) SRem(key string, members ...string) error    { return nil }
func (s *stubSharedState) SMembers(key string) ([]string, error)       { return nil, nil }
func (s *stubSharedState) Publish(channel string, data []byte) error   { return nil }
func (s *stubSharedState) Subscribe(ctx context.Context, channel string, handler func(data []byte)) {
}
func (s *stubSharedState) HSet(key, field, value string) error           { return nil }
func (s *stubSharedState) HDel(key, field string) error                  { return nil }
func (s *stubSharedState) HGetAll(key string) (map[string]string, error) { return nil, nil }
func (s *stubSharedState) HIncrBy(key, field string, incr int64) (int64, error) {
	return 0, nil
}
func (s *stubSharedState) IncrBy(key string, amount int64) (int64, error) { return 0, nil }
func (s *stubSharedState) DecrBy(key string, amount int64) (int64, error) { return 0, nil }
func (s *stubSharedState) TryIncrBy(key string, delta int64, max int64) (bool, error) {
	return true, nil
}
func (s *stubSharedState) Expire(key string, ttl time.Duration) (bool, error) {
	return true, nil
}

func okHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func TestAuthenticate_BearerHeader(t *testing.T) {
	verifier := &stubVerifier{claims: &auth.Claims{UserID: "u1", Email: "u@e.com", Role: "user"}}
	mw := NewAuthMiddleware(verifier, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rr := httptest.NewRecorder()

	handler := mw.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := GetClaims(r)
		if claims == nil || claims.UserID != "u1" {
			t.Fatalf("expected claims with UserID u1, got %+v", claims)
		}
		if r.Header.Get("X-User-ID") != "u1" {
			t.Error("expected X-User-ID header to be set")
		}
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestAuthenticate_QueryParam(t *testing.T) {
	verifier := &stubVerifier{claims: &auth.Claims{UserID: "u2", Role: "user"}}
	mw := NewAuthMiddleware(verifier, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/?token=query-token", nil)
	rr := httptest.NewRecorder()

	handler := mw.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := GetClaims(r)
		if claims == nil || claims.UserID != "u2" {
			t.Fatalf("expected claims with UserID u2")
		}
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestAuthenticate_CookieToken(t *testing.T) {
	verifier := &stubVerifier{claims: &auth.Claims{UserID: "u3", Role: "user"}}
	mw := NewAuthMiddleware(verifier, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "accessToken", Value: "cookie-token"})
	rr := httptest.NewRecorder()

	handler := mw.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := GetClaims(r)
		if claims == nil || claims.UserID != "u3" {
			t.Fatalf("expected claims with UserID u3")
		}
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestAuthenticate_BearerTakesPrecedenceOverQuery(t *testing.T) {
	var receivedToken string
	verifier := &stubVerifier{claims: &auth.Claims{UserID: "u1", Role: "user"}}

	customVerifier := &tokenCapturingVerifier{
		inner: verifier,
	}
	mw := NewAuthMiddleware(customVerifier, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/?token=query-token", nil)
	req.Header.Set("Authorization", "Bearer header-token")
	rr := httptest.NewRecorder()

	handler := mw.Authenticate(http.HandlerFunc(okHandler))
	handler.ServeHTTP(rr, req)

	receivedToken = customVerifier.lastToken
	if receivedToken != "header-token" {
		t.Errorf("expected bearer header to take precedence, got token: %s", receivedToken)
	}
}

type tokenCapturingVerifier struct {
	inner     auth.TokenVerifier
	lastToken string
}

func (v *tokenCapturingVerifier) Verify(token string) (*auth.Claims, error) {
	v.lastToken = token
	return v.inner.Verify(token)
}

func TestAuthenticate_NoToken_Returns401(t *testing.T) {
	verifier := &stubVerifier{claims: &auth.Claims{}}
	mw := NewAuthMiddleware(verifier, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	handler := mw.Authenticate(http.HandlerFunc(okHandler))
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestAuthenticate_InvalidToken_Returns401(t *testing.T) {
	verifier := &stubVerifier{err: errors.New("invalid token")}
	mw := NewAuthMiddleware(verifier, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer bad-token")
	rr := httptest.NewRecorder()

	handler := mw.Authenticate(http.HandlerFunc(okHandler))
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestAuthenticate_MalformedAuthorizationHeader(t *testing.T) {
	verifier := &stubVerifier{claims: &auth.Claims{UserID: "u1"}}
	mw := NewAuthMiddleware(verifier, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer")
	rr := httptest.NewRecorder()

	handler := mw.Authenticate(http.HandlerFunc(okHandler))
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestAuthenticate_NonBearerScheme(t *testing.T) {
	verifier := &stubVerifier{claims: &auth.Claims{UserID: "u1"}}
	mw := NewAuthMiddleware(verifier, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	rr := httptest.NewRecorder()

	handler := mw.Authenticate(http.HandlerFunc(okHandler))
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestAuthenticate_WithRoleFetcher_OverridesTokenRole(t *testing.T) {
	verifier := &stubVerifier{claims: &auth.Claims{UserID: "u1", Role: "user"}}
	fetcher := &stubRoleFetcher{role: "admin"}
	shared := newStubSharedState()
	mw := NewAuthMiddleware(verifier, fetcher, nil, shared)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rr := httptest.NewRecorder()

	handler := mw.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := GetClaims(r)
		if claims.Role != "admin" {
			t.Errorf("expected role to be admin from fetcher, got %s", claims.Role)
		}
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestAuthenticate_WithRoleFetcher_UserNotFound_Returns401(t *testing.T) {
	verifier := &stubVerifier{claims: &auth.Claims{UserID: "deleted-user", Role: "user"}}
	fetcher := &stubRoleFetcher{err: errors.New("not found")}
	mw := NewAuthMiddleware(verifier, fetcher, nil, newStubSharedState())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rr := httptest.NewRecorder()

	handler := mw.Authenticate(http.HandlerFunc(okHandler))
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestAuthenticate_RoleCachedInRedis(t *testing.T) {
	verifier := &stubVerifier{claims: &auth.Claims{UserID: "u1", Role: "user"}}
	fetcher := &stubRoleFetcher{role: "admin"}
	shared := newStubSharedState()
	mw := NewAuthMiddleware(verifier, fetcher, nil, shared)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rr := httptest.NewRecorder()

	mw.Authenticate(http.HandlerFunc(okHandler)).ServeHTTP(rr, req)

	cachedRole, err := shared.GetString("cache:role:u1")
	if err != nil {
		t.Fatalf("expected cached role, got error: %v", err)
	}
	if cachedRole != "admin" {
		t.Errorf("expected cached role admin, got %s", cachedRole)
	}
}

func TestAuthenticate_RoleFromCache(t *testing.T) {
	verifier := &stubVerifier{claims: &auth.Claims{UserID: "u1", Role: "user"}}
	fetcher := &stubRoleFetcher{role: "admin"}
	shared := newStubSharedState()

	shared.SetString("cache:role:u1", "super-admin", 30*time.Second)
	mw := NewAuthMiddleware(verifier, fetcher, nil, shared)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rr := httptest.NewRecorder()

	handler := mw.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := GetClaims(r)
		if claims.Role != "super-admin" {
			t.Errorf("expected cached role super-admin, got %s", claims.Role)
		}
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(rr, req)
}

func TestRequireRole_CorrectRole_Passes(t *testing.T) {
	mw := NewAuthMiddleware(nil, nil, nil, nil)
	claims := &auth.Claims{UserID: "u1", Role: "admin"}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := context.WithValue(req.Context(), ClaimsContextKey, claims)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	handler := mw.RequireRole("admin")(http.HandlerFunc(okHandler))
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestRequireRole_WrongRole_Returns403(t *testing.T) {
	mw := NewAuthMiddleware(nil, nil, nil, nil)
	claims := &auth.Claims{UserID: "u1", Role: "user"}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := context.WithValue(req.Context(), ClaimsContextKey, claims)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	handler := mw.RequireRole("admin")(http.HandlerFunc(okHandler))
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

func TestRequireRole_NoClaims_Returns401(t *testing.T) {
	mw := NewAuthMiddleware(nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	handler := mw.RequireRole("admin")(http.HandlerFunc(okHandler))
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestRequireRole_NilClaimsInContext_Returns401(t *testing.T) {
	mw := NewAuthMiddleware(nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := context.WithValue(req.Context(), ClaimsContextKey, (*auth.Claims)(nil))
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	handler := mw.RequireRole("admin")(http.HandlerFunc(okHandler))
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestGetClaims_WithClaims(t *testing.T) {
	claims := &auth.Claims{UserID: "u1", Email: "u@e.com", Role: "admin"}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := context.WithValue(req.Context(), ClaimsContextKey, claims)
	req = req.WithContext(ctx)

	result := GetClaims(req)
	if result == nil {
		t.Fatal("expected non-nil claims")
	}
	if result.UserID != "u1" {
		t.Errorf("expected UserID u1, got %s", result.UserID)
	}
}

func TestGetClaims_WithoutClaims(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	result := GetClaims(req)
	if result != nil {
		t.Error("expected nil claims when not set in context")
	}
}

func TestGetClaims_WrongTypeInContext(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := context.WithValue(req.Context(), ClaimsContextKey, "not claims")
	req = req.WithContext(ctx)

	result := GetClaims(req)
	if result != nil {
		t.Error("expected nil claims for wrong type")
	}
}
