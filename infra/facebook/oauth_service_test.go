package facebook

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	fbdomain "vozko/domain/facebook"
	"vozko/infra/meta/metatest"
)

const redirect = "https://api.example.com/oauth/facebook/callback"

func newOAuth(t *testing.T, handler http.HandlerFunc) fbdomain.OAuthService {
	t.Helper()
	svc, err := NewOAuthService(OAuthConfig{
		AppID: "app-1", AppSecret: "secret-1", ConfigID: "cfg-1", RedirectURI: redirect,
		HTTPClient: metatest.Server(t, handler),
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestAuthorizeURLUsesTheConfigurationNotScopes(t *testing.T) {
	svc := newOAuth(t, func(http.ResponseWriter, *http.Request) {})
	raw := svc.BuildAuthorizeURL("st")
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Host != "www.facebook.com" || u.Path != "/v25.0/dialog/oauth" {
		t.Fatalf("url = %s", raw)
	}
	if q.Get("client_id") != "app-1" || q.Get("config_id") != "cfg-1" || q.Get("response_type") != "code" ||
		q.Get("redirect_uri") != redirect || q.Get("state") != "st" || q.Get("override_default_response_type") != "true" {
		t.Fatalf("query = %v", q)
	}
	if q.Has("scope") {
		t.Fatal("scope must not be sent with a configuration")
	}
}

func TestExchangeCodeSendsTheSameRedirectURI(t *testing.T) {
	svc := newOAuth(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/v25.0/oauth/access_token" || q.Get("redirect_uri") != redirect || q.Get("code") != "c" || q.Get("client_secret") != "secret-1" {
			t.Errorf("request = %s", r.URL)
		}
		_, _ = w.Write([]byte(`{"access_token":"bisu-token","token_type":"bearer"}`))
	})
	grant, err := svc.ExchangeCode(context.Background(), "c")
	if err != nil {
		t.Fatal(err)
	}
	if grant.AccessToken != "bisu-token" || grant.ExpiresAt != nil {
		t.Fatalf("grant = %+v", grant)
	}
}

func TestExchangeCodeRejectsAnEmptyToken(t *testing.T) {
	svc := newOAuth(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) })
	if _, err := svc.ExchangeCode(context.Background(), "c"); err == nil {
		t.Fatal("empty token accepted")
	}
}

func TestExchangeCodeReadsExpiry(t *testing.T) {
	svc := newOAuth(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"t","expires_in":5183944}`))
	})
	grant, err := svc.ExchangeCode(context.Background(), "c")
	if err != nil || grant.ExpiresAt == nil {
		t.Fatalf("grant = %+v, %v", grant, err)
	}
}

func TestDebugTokenParsesGranularScopes(t *testing.T) {
	svc := newOAuth(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("access_token") != "app-1|secret-1" || r.URL.Query().Get("input_token") != "bisu" {
			t.Errorf("debug_token must be called with the app token: %s", r.URL)
		}
		_, _ = w.Write([]byte(`{"data":{"is_valid":true,"type":"SYSTEM_USER","user_id":"asid-1","expires_at":0,
			"scopes":["pages_show_list","pages_messaging","business_management"],
			"granular_scopes":[{"scope":"pages_show_list","target_ids":["1","2"]},{"scope":"pages_messaging","target_ids":["1"]},{"scope":"business_management"}]}}`))
	})
	d, err := svc.DebugToken(context.Background(), "bisu")
	if err != nil {
		t.Fatal(err)
	}
	if !d.Valid || d.AppScopedUser != "asid-1" || d.ExpiresAt != nil || len(d.Scopes) != 3 {
		t.Fatalf("debug = %+v", d)
	}
	if len(d.GranularScopes["pages_show_list"]) != 2 || len(d.GranularScopes["pages_messaging"]) != 1 {
		t.Fatalf("granular = %+v", d.GranularScopes)
	}
	if v, ok := d.GranularScopes["business_management"]; !ok || v != nil {
		t.Fatalf("scope without targets must be present with nil targets: %+v", d.GranularScopes)
	}
}

func TestListPagesFollowsCursorsUntilNoNext(t *testing.T) {
	calls := 0
	svc := newOAuth(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("after") == "" {
			_, _ = w.Write([]byte(`{"data":[{"id":"1","name":"A","access_token":"pt-1","tasks":["MANAGE"],"picture":{"data":{"url":"https://cdn/a.jpg"}},"instagram_business_account":{"id":"178"}}],
				"paging":{"cursors":{"after":"CUR"},"next":"https://graph.facebook.com/v25.0/me/accounts?after=CUR"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"2","name":"B","access_token":"pt-2","tasks":["MESSAGING"]}],"paging":{"cursors":{"after":"END"}}}`))
	})
	pages, err := svc.ListPages(context.Background(), "bisu")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(pages) != 2 {
		t.Fatalf("calls=%d pages=%d", calls, len(pages))
	}
	if pages[0].AccessToken != "pt-1" || pages[0].PictureURL != "https://cdn/a.jpg" || pages[0].LinkedIGUserID != "178" || pages[0].Tasks[0] != fbdomain.TaskManage {
		t.Fatalf("page = %+v", pages[0])
	}
}

func TestIdentifyReadsClientBusiness(t *testing.T) {
	svc := newOAuth(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Query().Get("fields"), "client_business_id") {
			t.Errorf("fields = %s", r.URL.Query().Get("fields"))
		}
		_, _ = w.Write([]byte(`{"id":"asid-1","name":"Vozko Integration","client_business_id":"biz-9"}`))
	})
	id, err := svc.Identify(context.Background(), "bisu", fbdomain.TokenSystemUser)
	if err != nil || id.AppScopedUserID != "asid-1" || id.ClientBusinessID != "biz-9" {
		t.Fatalf("identity = %+v, %v", id, err)
	}
}

func TestIdentifyAsksAUserTokenOnlyForWhatItCanRead(t *testing.T) {
	svc := newOAuth(t, func(w http.ResponseWriter, r *http.Request) {
		if fields := r.URL.Query().Get("fields"); fields != "id,name" {
			t.Errorf("fields = %s", fields)
		}
		_, _ = w.Write([]byte(`{"id":"asid-2","name":"Ana"}`))
	})
	id, err := svc.Identify(context.Background(), "user", fbdomain.TokenUser)
	if err != nil || id.AppScopedUserID != "asid-2" || id.ClientBusinessID != "" {
		t.Fatalf("identity = %+v, %v", id, err)
	}
}

func TestNewOAuthServiceRequiresConfiguration(t *testing.T) {
	if _, err := NewOAuthService(OAuthConfig{AppID: "a", AppSecret: "s", RedirectURI: redirect}); err == nil {
		t.Fatal("missing config id accepted")
	}
}
