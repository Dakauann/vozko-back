package instagram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	igdomain "vozko/domain/instagram"
)

func newTestOAuth(t *testing.T, handler http.HandlerFunc) (igdomain.OAuthService, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	svc := NewOAuthService(OAuthConfig{
		AppID:       "app-1",
		AppSecret:   "secret-1",
		RedirectURI: "https://api.example.com" + igdomain.OAuthCallbackPath,
		HTTPClient:  srv.Client(),
	})

	base, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse stub url: %v", err)
	}
	svc.(*oauthService).http = &http.Client{
		Transport: rewriteTransport{host: base.Host, inner: srv.Client().Transport},
	}
	return svc, srv
}

type rewriteTransport struct {
	host  string
	inner http.RoundTripper
}

func (r rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = "http"
	req.URL.Host = r.host
	inner := r.inner
	if inner == nil {
		inner = http.DefaultTransport
	}
	return inner.RoundTrip(req)
}

func TestExchangeCode_PermissionsAsArray(t *testing.T) {
	svc, _ := newTestOAuth(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": [{
				"access_token": "IGAA-short-lived",
				"user_id": 17841458366137975,
				"permissions": [
					"instagram_business_basic",
					"instagram_business_manage_messages",
					"instagram_business_manage_comments",
					"instagram_business_content_publish"
				]
			}]
		}`))
	})

	grant, err := svc.ExchangeCode(context.Background(), "the-code")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if grant.AccessToken != "IGAA-short-lived" {
		t.Errorf("token = %q", grant.AccessToken)
	}
	if grant.UserID != "17841458366137975" {
		t.Errorf("user_id = %q, want 17841458366137975", grant.UserID)
	}
	if len(grant.Permissions) != 4 {
		t.Fatalf("permissions = %v, want 4 entries", grant.Permissions)
	}
	if grant.Permissions[1] != igdomain.ScopeManageMessages {
		t.Errorf("permissions[1] = %q, want %q", grant.Permissions[1], igdomain.ScopeManageMessages)
	}
}

func TestExchangeCode_PermissionsAsString(t *testing.T) {
	svc, _ := newTestOAuth(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{
			"access_token":"tok",
			"user_id":"123",
			"permissions":"instagram_business_basic,instagram_business_manage_messages"
		}]}`))
	})

	grant, err := svc.ExchangeCode(context.Background(), "code")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if len(grant.Permissions) != 2 {
		t.Fatalf("permissions = %v, want 2", grant.Permissions)
	}
}

func TestExchangeCode_FlatResponse(t *testing.T) {
	svc, _ := newTestOAuth(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"flat-tok","user_id":"999","permissions":["instagram_business_basic"]}`))
	})

	grant, err := svc.ExchangeCode(context.Background(), "code")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if grant.AccessToken != "flat-tok" || grant.UserID != "999" {
		t.Errorf("flat response not decoded: %+v", grant)
	}
}

func TestExchangeCode_EmptyTokenIsAnError(t *testing.T) {
	svc, _ := newTestOAuth(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"user_id":"1"}]}`))
	})

	if _, err := svc.ExchangeCode(context.Background(), "code"); err == nil {
		t.Fatal("an empty access token was accepted")
	}
}

func TestExchangeCode_SendsCorrectForm(t *testing.T) {
	var got url.Values
	var gotPath, gotContentType string

	svc, _ := newTestOAuth(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		_ = r.ParseForm()
		got = r.PostForm
		_, _ = w.Write([]byte(`{"data":[{"access_token":"t","user_id":"1","permissions":[]}]}`))
	})

	if _, err := svc.ExchangeCode(context.Background(), "the-code"); err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}

	if gotPath != "/oauth/access_token" {
		t.Errorf("path = %q", gotPath)
	}
	if !strings.HasPrefix(gotContentType, "application/x-www-form-urlencoded") {
		t.Errorf("content-type = %q, want form encoding", gotContentType)
	}
	if got.Get("grant_type") != "authorization_code" {
		t.Errorf("grant_type = %q", got.Get("grant_type"))
	}
	if got.Get("code") != "the-code" {
		t.Errorf("code = %q", got.Get("code"))
	}
	if got.Get("redirect_uri") != "https://api.example.com"+igdomain.OAuthCallbackPath {
		t.Errorf("redirect_uri = %q", got.Get("redirect_uri"))
	}
}

func TestRefreshToken_OmitsClientSecret(t *testing.T) {
	var query url.Values
	svc, _ := newTestOAuth(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		_, _ = w.Write([]byte(`{"access_token":"refreshed","token_type":"bearer","expires_in":5183944}`))
	})

	grant, err := svc.RefreshToken(context.Background(), "old-token")
	if err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}
	if grant.AccessToken != "refreshed" {
		t.Errorf("token = %q", grant.AccessToken)
	}
	if query.Get("grant_type") != "ig_refresh_token" {
		t.Errorf("grant_type = %q", query.Get("grant_type"))
	}
	if query.Get("client_secret") != "" {
		t.Error("refresh must not send client_secret")
	}
	if grant.ExpiresIn.Hours() < 24*59 {
		t.Errorf("expires_in = %v, want ~60 days", grant.ExpiresIn)
	}
}

func TestGetProfile_UsesUserIDNotID(t *testing.T) {
	svc, _ := newTestOAuth(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"user_id": "17841458366137975",
			"id": "9999999999",
			"username": "dakauanncavalcantede",
			"account_type": "BUSINESS",
			"followers_count": 12
		}`))
	})

	profile, err := svc.GetProfile(context.Background(), "tok")
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if profile.IGUserID != "17841458366137975" {
		t.Errorf("IGUserID = %q, want user_id (not the app-scoped id)", profile.IGUserID)
	}
	if profile.IGUserID == "9999999999" {
		t.Error("IGUserID was taken from `id`, which is app-scoped and unusable in paths")
	}
	if profile.Username != "dakauanncavalcantede" {
		t.Errorf("username = %q", profile.Username)
	}
}

func TestGetProfile_MissingUserIDIsAnError(t *testing.T) {
	svc, _ := newTestOAuth(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"9999999999","username":"x"}`))
	})

	if _, err := svc.GetProfile(context.Background(), "tok"); err == nil {
		t.Fatal("a profile without user_id was accepted")
	}
}

func TestBuildAuthorizeURL(t *testing.T) {
	svc := NewOAuthService(OAuthConfig{
		AppID:       "app-1",
		AppSecret:   "s",
		RedirectURI: "https://api.example.com" + igdomain.OAuthCallbackPath,
	})

	raw := svc.BuildAuthorizeURL("the-state")
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if parsed.Host != AuthorizeHost {
		t.Errorf("host = %q, want %q", parsed.Host, AuthorizeHost)
	}
	q := parsed.Query()
	if q.Get("response_type") != "code" {
		t.Errorf("response_type = %q", q.Get("response_type"))
	}
	if q.Get("state") != "the-state" {
		t.Errorf("state = %q", q.Get("state"))
	}
	scope := q.Get("scope")
	if strings.Contains(scope, " ") {
		t.Errorf("scope %q is space-separated; Instagram requires commas", scope)
	}
	if len(strings.Split(scope, ",")) != len(igdomain.RequiredScopes()) {
		t.Errorf("scope = %q, want %d comma-separated scopes", scope, len(igdomain.RequiredScopes()))
	}
}

func TestPermissionList_Shapes(t *testing.T) {
	cases := []struct {
		raw  string
		want []string
	}{
		{`["a","b"]`, []string{"a", "b"}},
		{`"a,b"`, []string{"a", "b"}},
		{`"a, b ,, a"`, []string{"a", "b"}},
		{`""`, nil},
		{`[]`, nil},
		{`null`, nil},
		{`123`, nil},
		{`{"unexpected":true}`, nil},
	}

	for _, c := range cases {
		var got permissionList
		if err := json.Unmarshal([]byte(c.raw), &got); err != nil {
			t.Errorf("Unmarshal(%s) = %v, want nil", c.raw, err)
			continue
		}
		if len(got) != len(c.want) {
			t.Errorf("Unmarshal(%s) = %v, want %v", c.raw, got.Strings(), c.want)
			continue
		}
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Errorf("Unmarshal(%s)[%d] = %q, want %q", c.raw, i, got[i], c.want[i])
			}
		}
	}
}

func TestGraphID_PreservesExactDigits(t *testing.T) {
	cases := map[string]string{
		`17841458366137975`:   "17841458366137975",
		`"17841458366137975"`: "17841458366137975",
		`123`:                 "123",
		`"123"`:               "123",
		`"  42  "`:            "42",
		`null`:                "",
		`""`:                  "",
	}

	for raw, want := range cases {
		var got graphID
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			t.Errorf("Unmarshal(%s) = %v, want nil", raw, err)
			continue
		}
		if got.String() != want {
			t.Errorf("Unmarshal(%s) = %q, want %q", raw, got.String(), want)
		}
	}
}

func TestGraphID_BeatsFloat64(t *testing.T) {
	const raw = `17841458366137975`

	var viaAny any
	if err := json.Unmarshal([]byte(raw), &viaAny); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	lossy := int64(viaAny.(float64))
	if lossy == 17841458366137975 {
		t.Skip("float64 happens to be exact on this platform; the invariant still holds")
	}

	var exact graphID
	if err := json.Unmarshal([]byte(raw), &exact); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if exact.String() != raw {
		t.Fatalf("graphID lost precision: %q, want %q", exact.String(), raw)
	}
	t.Logf("float64 path would have produced %d; graphID preserved %s", lossy, exact)
}
