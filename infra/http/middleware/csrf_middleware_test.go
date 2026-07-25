package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func csrfOKHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestCSRFMiddleware(t *testing.T) {
	mw := NewCSRFMiddleware([]string{"https://app.example.com/"})
	h := mw.Handler(csrfOKHandler())

	req := func(method string, opts ...func(*http.Request)) *http.Request {
		r := httptest.NewRequest(method, "https://api.example.com/thing", nil)
		for _, o := range opts {
			o(r)
		}
		return r
	}
	accessCookie := func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "accessToken", Value: "x"}) }
	refreshCookie := func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "refreshToken", Value: "x"}) }
	origin := func(v string) func(*http.Request) { return func(r *http.Request) { r.Header.Set("Origin", v) } }
	referer := func(v string) func(*http.Request) { return func(r *http.Request) { r.Header.Set("Referer", v) } }
	bearer := func(r *http.Request) { r.Header.Set("Authorization", "Bearer t") }

	cases := []struct {
		name string
		req  *http.Request
		want int
	}{
		{"GET with cookie is exempt (safe method)", req(http.MethodGet, accessCookie), http.StatusOK},
		{"POST with Bearer is exempt (not ambient)", req(http.MethodPost, bearer, origin("https://evil.example")), http.StatusOK},
		{"POST without any auth cookie is exempt (login)", req(http.MethodPost), http.StatusOK},
		{"POST cookie + trusted Origin passes", req(http.MethodPost, accessCookie, origin("https://app.example.com")), http.StatusOK},
		{"POST cookie + untrusted Origin blocked", req(http.MethodPost, accessCookie, origin("https://evil.example")), http.StatusForbidden},
		{"POST cookie + missing Origin/Referer blocked", req(http.MethodPost, accessCookie), http.StatusForbidden},
		{"POST cookie + trusted Referer fallback passes", req(http.MethodPost, accessCookie, referer("https://app.example.com/page")), http.StatusOK},
		{"POST refresh cookie + untrusted Origin blocked", req(http.MethodPost, refreshCookie, origin("https://evil.example")), http.StatusForbidden},
		{"DELETE cookie + trusted Origin passes", req(http.MethodDelete, accessCookie, origin("https://app.example.com")), http.StatusOK},
		// Same-origin: the API serves its own pages (e.g. the Meta Embedded Signup
		// popup) that POST back to the API with the API's own origin, which is not in
		// the frontend allowlist. These are not cross-site and must pass.
		{"POST cookie + same-origin (API's own page) passes", req(http.MethodPost, accessCookie, origin("https://api.example.com")), http.StatusOK},
		{"POST cookie + same-origin via Referer fallback passes", req(http.MethodPost, accessCookie, referer("https://api.example.com/oauth/meta/embedded")), http.StatusOK},
		{"POST cookie + same host but wrong scheme blocked", req(http.MethodPost, accessCookie, origin("http://api.example.com")), http.StatusForbidden},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, tc.req)
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}

// TestCSRFMiddlewareSameOriginScheme exercises the scheme reconstruction used by
// the same-origin trust: TLS is terminated at the proxy (r.TLS nil in prod), so
// the scheme comes from X-Forwarded-Proto, with a localhost fallback for dev.
func TestCSRFMiddlewareSameOriginScheme(t *testing.T) {
	// No frontend origins trusted at all: any pass here is the same-origin path.
	mw := NewCSRFMiddleware(nil)
	h := mw.Handler(csrfOKHandler())

	cases := []struct {
		name    string
		target  string
		xfproto string
		origin  string
		want    int
	}{
		{"localhost dev, http same-origin passes", "http://localhost:3001/thing", "", "http://localhost:3001", http.StatusOK},
		{"proxy forwards https, https origin passes", "http://api.example.com/thing", "https", "https://api.example.com", http.StatusOK},
		{"proxy forwards https, http origin blocked (scheme mismatch)", "http://api.example.com/thing", "https", "http://api.example.com", http.StatusForbidden},
		{"proxy forwards list, first scheme wins", "http://api.example.com/thing", "https, http", "https://api.example.com", http.StatusOK},
		{"different host blocked", "http://api.example.com/thing", "https", "https://evil.example", http.StatusForbidden},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, tc.target, nil)
			r.AddCookie(&http.Cookie{Name: "accessToken", Value: "x"})
			if tc.xfproto != "" {
				r.Header.Set("X-Forwarded-Proto", tc.xfproto)
			}
			r.Header.Set("Origin", tc.origin)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}
