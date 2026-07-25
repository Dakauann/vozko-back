package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type stubLogout struct{ err error }

func (s *stubLogout) Execute(userID, sessionID, callerJTI string) error { return s.err }

func newTestHandlerWithCookies() *AuthHandler {
	h := newTestHandler()
	h.SetCookieConfig(CookieConfig{
		Domain:        ".vozko.com.br",
		Secure:        true,
		AccessMaxAge:  15 * time.Minute,
		RefreshMaxAge: 30 * 24 * time.Hour,
	})
	return h
}

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, c := range cookies {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// postJSONCookieMode posts as the browser SPA does: with the X-Auth-Mode: cookie
// header that opts into cookie-based token delivery. Without this header the API
// keeps the default JSON/Bearer contract (see the API-mode tests).
func postJSONCookieMode(handler http.HandlerFunc, path string, body interface{}) *httptest.ResponseRecorder {
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Auth-Mode", "cookie")
	rr := httptest.NewRecorder()
	handler(rr, req)
	return rr
}

func TestLogin_SetsCookies(t *testing.T) {
	h := newTestHandlerWithCookies()
	rr := postJSONCookieMode(h.Login, "/auth/login", map[string]string{
		"email":    "user@test.com",
		"password": "password123",
	})

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	cookies := rr.Result().Cookies()

	at := findCookie(cookies, "accessToken")
	if at == nil {
		t.Fatal("accessToken cookie not set")
	}
	if at.SameSite != http.SameSiteLaxMode {
		t.Errorf("accessToken cookie should be SameSite=Lax, got %v", at.SameSite)
	}
	if at.Value != "at-123" {
		t.Errorf("expected at-123, got %s", at.Value)
	}
	if !at.HttpOnly {
		t.Error("accessToken should be httpOnly")
	}
	if !at.Secure {
		t.Error("accessToken should be secure")
	}
	if at.MaxAge != int((15 * time.Minute).Seconds()) {
		t.Errorf("expected MaxAge %d, got %d", int((15 * time.Minute).Seconds()), at.MaxAge)
	}
	if at.Domain != ".vozko.com.br" && at.Domain != "vozko.com.br" {
		t.Errorf("expected domain vozko.com.br, got %s", at.Domain)
	}

	rt := findCookie(cookies, "refreshToken")
	if rt == nil {
		t.Fatal("refreshToken cookie not set")
	}
	if rt.Value != "rt-123" {
		t.Errorf("expected rt-123, got %s", rt.Value)
	}
	if !rt.HttpOnly {
		t.Error("refreshToken should be httpOnly")
	}
	if rt.Path != "/auth/refresh" {
		t.Errorf("refreshToken path should be /auth/refresh, got %s", rt.Path)
	}

	ud := findCookie(cookies, "userData")
	if ud == nil {
		t.Fatal("userData cookie not set")
	}
	if ud.HttpOnly {
		t.Error("userData should NOT be httpOnly (browser needs to read it)")
	}

	decodedUserData, err := url.QueryUnescape(ud.Value)
	if err != nil {
		t.Fatalf("expected encoded userData cookie, got decode error: %v", err)
	}

	var parsedUserData map[string]string
	if err := json.Unmarshal([]byte(decodedUserData), &parsedUserData); err != nil {
		t.Fatalf("expected JSON userData cookie, got %q: %v", decodedUserData, err)
	}

	if parsedUserData["id"] != "user-1" {
		t.Errorf("expected userData id user-1, got %q", parsedUserData["id"])
	}
	if parsedUserData["email"] != "user@test.com" {
		t.Errorf("expected userData email user@test.com, got %q", parsedUserData["email"])
	}
	if parsedUserData["name"] != "Test User" {
		t.Errorf("expected userData name Test User, got %q", parsedUserData["name"])
	}
	if parsedUserData["role"] != "user" {
		t.Errorf("expected userData role user, got %q", parsedUserData["role"])
	}
	if parsedUserData["customerType"] != "individual" {
		t.Errorf("expected userData customerType individual, got %q", parsedUserData["customerType"])
	}
}

func TestLogin_UserDataCookie_EncodesUnicodeSafely(t *testing.T) {
	h := newTestHandlerWithCookies()
	pair := defaultTokenPair()
	pair.Name = "Jos\u00e9 da Silva"
	h.credentials = &stubCredentialsLogin{pair: pair}

	rr := postJSONCookieMode(h.Login, "/auth/login", map[string]string{
		"email":    "user@test.com",
		"password": "password123",
	})

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	ud := findCookie(rr.Result().Cookies(), "userData")
	if ud == nil {
		t.Fatal("userData cookie not set")
	}
	if strings.Contains(ud.Value, "Jos\u00e9") {
		t.Fatalf("expected encoded cookie value, got %q", ud.Value)
	}

	decodedUserData, err := url.QueryUnescape(ud.Value)
	if err != nil {
		t.Fatalf("expected encoded userData cookie, got decode error: %v", err)
	}

	var parsedUserData map[string]string
	if err := json.Unmarshal([]byte(decodedUserData), &parsedUserData); err != nil {
		t.Fatalf("expected JSON userData cookie, got %q: %v", decodedUserData, err)
	}
	if parsedUserData["name"] != "Jos\u00e9 da Silva" {
		t.Fatalf("expected unicode name preserved, got %q", parsedUserData["name"])
	}
}

func TestLogin_NoCookies_WhenConfigEmpty(t *testing.T) {
	h := newTestHandler()
	rr := postJSON(h.Login, "/auth/login", map[string]string{
		"email":    "user@test.com",
		"password": "password123",
	})

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	cookies := rr.Result().Cookies()
	if len(cookies) != 0 {
		t.Errorf("expected no cookies without CookieConfig, got %d", len(cookies))
	}
}

func TestRefreshToken_FromCookie(t *testing.T) {
	h := newTestHandlerWithCookies()

	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Auth-Mode", "cookie")
	req.AddCookie(&http.Cookie{Name: "refreshToken", Value: "rt-123"})
	rr := httptest.NewRecorder()
	h.RefreshToken(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}

	// In cookie mode the rotated tokens ride cookies, not the JSON body.
	var resp map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&resp)
	if _, present := resp["accessToken"]; present {
		t.Errorf("cookie mode must omit accessToken from body, got %v", resp["accessToken"])
	}

	cookies := rr.Result().Cookies()
	newAT := findCookie(cookies, "accessToken")
	if newAT == nil {
		t.Fatal("expected new accessToken cookie on refresh")
	}
	if newAT.Value != "at-123" {
		t.Errorf("expected rotated accessToken cookie at-123, got %s", newAT.Value)
	}
}

func TestRefreshToken_BodyTakesPrecedenceOverCookie(t *testing.T) {
	h := newTestHandlerWithCookies()

	body, _ := json.Marshal(map[string]string{"refreshToken": "rt-123"})
	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "refreshToken", Value: "wrong-token"})
	rr := httptest.NewRecorder()
	h.RefreshToken(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestRefreshToken_MissingBothBodyAndCookie(t *testing.T) {
	h := newTestHandlerWithCookies()

	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.RefreshToken(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestLogout_ClearsCookies(t *testing.T) {
	h := NewAuthHandler(
		nil, nil, nil, nil, nil, nil, nil, nil, nil,
		&stubLogout{},
		nil, nil, nil)

	h.SetCookieConfig(CookieConfig{
		Domain:        ".vozko.com.br",
		Secure:        true,
		AccessMaxAge:  15 * time.Minute,
		RefreshMaxAge: 30 * 24 * time.Hour,
	})

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", bytes.NewReader([]byte("{}")))
	req.Header.Set("X-User-ID", "user-1")
	rr := httptest.NewRecorder()
	h.Logout(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}

	cookies := rr.Result().Cookies()
	at := findCookie(cookies, "accessToken")
	if at == nil {
		t.Fatal("expected accessToken clear cookie")
	}
	if at.MaxAge != -1 {
		t.Errorf("expected MaxAge -1, got %d", at.MaxAge)
	}
	if at.Value != "" {
		t.Errorf("expected empty value, got %s", at.Value)
	}

	rt := findCookie(cookies, "refreshToken")
	if rt == nil {
		t.Fatal("expected refreshToken clear cookie")
	}
	if rt.MaxAge != -1 {
		t.Errorf("expected MaxAge -1, got %d", rt.MaxAge)
	}

	ud := findCookie(cookies, "userData")
	if ud == nil {
		t.Fatal("expected userData clear cookie")
	}
	if ud.MaxAge != -1 {
		t.Errorf("expected MaxAge -1, got %d", ud.MaxAge)
	}
}

func TestRegister_SetsCookies(t *testing.T) {
	h := newTestHandlerWithCookies()
	rr := postJSONCookieMode(h.Register, "/auth/register", map[string]string{
		"name":              "Test User",
		"email":             "user@test.com",
		"password":          "StrongPass1",
		"customerType":      "individual",
		"cpf":               "12345678901",
		"verificationToken": "valid-token",
	})

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}

	cookies := rr.Result().Cookies()
	if findCookie(cookies, "accessToken") == nil {
		t.Error("expected accessToken cookie on register")
	}
	if findCookie(cookies, "refreshToken") == nil {
		t.Error("expected refreshToken cookie on register")
	}
}

// API mode (no X-Auth-Mode header): tokens in the JSON body and, crucially, NO
// cookies set even though the cookie domain is configured. This is the contract
// third-party API and mobile clients rely on.
func TestLogin_APIMode_ReturnsJSONTokensAndNoCookies(t *testing.T) {
	h := newTestHandlerWithCookies()
	rr := postJSON(h.Login, "/auth/login", map[string]string{
		"email":    "user@test.com",
		"password": "password123",
	})

	var resp map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&resp)

	if resp["accessToken"] != "at-123" {
		t.Errorf("JSON body should contain accessToken, got %v", resp["accessToken"])
	}
	if resp["refreshToken"] != "rt-123" {
		t.Errorf("JSON body should contain refreshToken, got %v", resp["refreshToken"])
	}
	if resp["tokenType"] != "Bearer" {
		t.Errorf("JSON body should contain tokenType, got %v", resp["tokenType"])
	}

	if cookies := rr.Result().Cookies(); len(cookies) != 0 {
		t.Errorf("API mode must not set cookies, got %d", len(cookies))
	}
}

// Cookie mode: tokens are delivered ONLY as cookies and never appear in the JSON
// body, so they never reach JavaScript.
func TestLogin_CookieMode_OmitsTokensFromBody(t *testing.T) {
	h := newTestHandlerWithCookies()
	rr := postJSONCookieMode(h.Login, "/auth/login", map[string]string{
		"email":    "user@test.com",
		"password": "password123",
	})

	var resp map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&resp)

	if _, present := resp["accessToken"]; present {
		t.Errorf("cookie mode must omit accessToken from body, got %v", resp["accessToken"])
	}
	if _, present := resp["refreshToken"]; present {
		t.Errorf("cookie mode must omit refreshToken from body, got %v", resp["refreshToken"])
	}
	// User fields still returned so the SPA can paint immediately.
	if resp["userId"] != "user-1" {
		t.Errorf("cookie mode should still return userId, got %v", resp["userId"])
	}
	if findCookie(rr.Result().Cookies(), "accessToken") == nil {
		t.Error("cookie mode should set the accessToken cookie")
	}
}
