package oauthpopup

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestPopupPostsToTheExactFrontendOrigin(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteResult(rec, "https://app.vozko.example", map[string]any{"source": "fb-business-login", "status": "connected"})
	body := rec.Body.String()
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("code=%d cache=%q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	if !strings.Contains(body, `var target = "https://app.vozko.example";`) {
		t.Fatalf("target origin not pinned: %s", body)
	}
	if strings.Contains(body, `"*"`) {
		t.Fatal("wildcard origin must never be used")
	}
	if !strings.Contains(body, `"source":"fb-business-login"`) {
		t.Fatalf("payload missing: %s", body)
	}
}

func TestPopupWithoutFrontendOriginDoesNotPost(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteResult(rec, "", map[string]any{"status": "error"})
	if !strings.Contains(rec.Body.String(), `var target = "null";`) {
		t.Fatalf("empty origin must disable posting: %s", rec.Body.String())
	}
}

func TestPopupEscapesScriptBreakout(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteResult(rec, "https://app.example", map[string]any{"name": "</script><script>alert(1)</script>"})
	if strings.Contains(rec.Body.String(), "</script><script>alert(1)") {
		t.Fatal("payload broke out of the script tag")
	}
}

func TestRedirectAddsResultParamsAndBlocksOpenRedirects(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/dashboard/facebook-pages/abc", "/dashboard/facebook-pages/abc"},
		{"https://evil.example", "/dashboard/facebook-pages"},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/oauth/facebook/callback", nil)
		Redirect(rec, req, "https://app.example", tc.path, "/dashboard/facebook-pages", url.Values{"facebook": {"connected"}, "connected": {"2"}})
		loc, err := url.Parse(rec.Header().Get("Location"))
		if err != nil || rec.Code != http.StatusFound {
			t.Fatalf("code=%d loc=%q", rec.Code, rec.Header().Get("Location"))
		}
		if loc.Host != "app.example" || loc.Path != tc.want || loc.Query().Get("facebook") != "connected" || loc.Query().Get("connected") != "2" {
			t.Fatalf("location = %s", loc)
		}
	}
}
