package googleoauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"vozko/domain/googleaccount"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := New(Config{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RedirectURI:  "https://api.example.com/oauth/google/callback",
		Endpoints: Endpoints{
			Authorize: server.URL + "/auth",
			Token:     server.URL + "/token",
			UserInfo:  server.URL + "/userinfo",
			Revoke:    server.URL + "/revoke",
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}

func TestNewRefusesIncompleteConfig(t *testing.T) {
	cases := []Config{
		{ClientSecret: "s", RedirectURI: "https://a.example.com/cb"},
		{ClientID: "c", RedirectURI: "https://a.example.com/cb"},
		{ClientID: "c", ClientSecret: "s"},
		{ClientID: "c", ClientSecret: "s", RedirectURI: "http://a.example.com/cb"},
		{ClientID: "c", ClientSecret: "s", RedirectURI: "https://a.example.com/cb?x=1"},
	}
	for _, cfg := range cases {
		if _, err := New(cfg); err == nil {
			t.Fatalf("New(%+v) accepted an incomplete config", cfg)
		}
	}
}

func TestAuthorizeURLAsksForOfflineIncrementalConsent(t *testing.T) {
	client := newTestClient(t, func(http.ResponseWriter, *http.Request) {})
	raw := client.AuthorizeURL("state-1", googleaccount.ProductSheets.AuthorizationScopes())
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	q := parsed.Query()
	want := map[string]string{
		"client_id":              "client-id",
		"redirect_uri":           "https://api.example.com/oauth/google/callback",
		"response_type":          "code",
		"access_type":            "offline",
		"prompt":                 "consent",
		"include_granted_scopes": "true",
		"state":                  "state-1",
	}
	for key, value := range want {
		if q.Get(key) != value {
			t.Fatalf("%s = %q, want %q", key, q.Get(key), value)
		}
	}
	if !strings.Contains(q.Get("scope"), googleaccount.ScopeDriveFile) {
		t.Fatalf("scope %q does not ask for drive.file", q.Get("scope"))
	}
}

func TestExchangeCodeReturnsTheGrantedScopes(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" || r.Method != http.MethodPost {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") != "code-1" || r.Form.Get("redirect_uri") == "" {
			t.Fatalf("unexpected form %v", r.Form)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "access", "refresh_token": "refresh", "expires_in": 3599,
			"scope": googleaccount.ScopeDriveFile + " openid",
		})
	})
	grant, err := client.ExchangeCode(context.Background(), "code-1")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if grant.AccessToken != "access" || grant.RefreshToken != "refresh" || grant.ExpiresIn != 3599*time.Second {
		t.Fatalf("grant = %+v", grant)
	}
	if !googleaccount.GrantCovers(grant.Scopes, googleaccount.ProductSheets) {
		t.Fatalf("scopes %v lost drive.file", grant.Scopes)
	}
}

func TestRefreshInvalidGrantIsAnAuthFailure(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant", "error_description": "Token has been expired or revoked."})
	})
	_, err := client.Refresh(context.Background(), "refresh")
	if err == nil {
		t.Fatal("Refresh accepted a revoked token")
	}
	if !(Classifier{}).IsAuth(err) {
		t.Fatalf("invalid_grant not classified as auth: %v", err)
	}
}

func TestRefreshServerErrorIsNotAnAuthFailure(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	_, err := client.Refresh(context.Background(), "refresh")
	if err == nil {
		t.Fatal("Refresh accepted a 503")
	}
	if (Classifier{}).IsAuth(err) {
		t.Fatal("a 503 must be retried, not treated as a lost grant")
	}
}

func TestProfileRequiresAnEmail(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer access" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"name": "Ana"})
	})
	_, err := client.Profile(context.Background(), "access")
	if !errors.Is(err, googleaccount.ErrEmailMissing) {
		t.Fatalf("Profile err = %v, want ErrEmailMissing", err)
	}
}

func TestProfileNormalizesTheEmail(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"email": " Ana@Example.com ", "name": "Ana"})
	})
	profile, err := client.Profile(context.Background(), "access")
	if err != nil || profile.Email != "ana@example.com" {
		t.Fatalf("Profile = %+v, %v", profile, err)
	}
}

func TestRevokeReportsRefusals(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	})
	if err := client.Revoke(context.Background(), "refresh"); err == nil {
		t.Fatal("Revoke hid a refusal")
	}
}

func TestNewAcceptsLoopbackHTTPForDevelopment(t *testing.T) {
	if _, err := New(Config{ClientID: "c", ClientSecret: "s", RedirectURI: "http://localhost:8080/oauth/google/callback"}); err != nil {
		t.Fatalf("New refused a loopback redirect: %v", err)
	}
}
