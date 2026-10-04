package webchat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/shared"
	wcdomain "vozko/domain/webchat"
	wcuc "vozko/usecases/webchat"
)

const apiOrigin = "https://api.vozko.example"

type widgetStore struct{ w *wcdomain.Widget }

func (s widgetStore) Create(context.Context, *wcdomain.Widget) error { return nil }
func (s widgetStore) Update(context.Context, *wcdomain.Widget) error { return nil }
func (s widgetStore) FindByID(context.Context, string, string) (*wcdomain.Widget, error) {
	return s.w, nil
}
func (s widgetStore) FindByIDUnscoped(context.Context, string) (*wcdomain.Widget, error) {
	return s.w, nil
}
func (s widgetStore) FindByPublicKey(_ context.Context, key string) (*wcdomain.Widget, error) {
	if s.w == nil || s.w.PublicKey != key {
		return nil, wcdomain.ErrWidgetNotFound
	}
	return s.w, nil
}
func (s widgetStore) ListByWorkspace(context.Context, wcdomain.ListWidgetsInput) (*shared.PaginatedResult[*wcdomain.Widget], error) {
	return nil, nil
}
func (s widgetStore) Delete(context.Context, string, string) error { return nil }

func publicRouter(w *wcdomain.Widget) *mux.Router {
	svc := wcuc.NewVisitorService(wcuc.VisitorDeps{Widgets: widgetStore{w: w}})
	r := mux.NewRouter()
	RegisterPublicRoutes(r, NewPublicHandler(svc, nil, apiOrigin))
	return r
}

func activeWidget() *wcdomain.Widget {
	w := &wcdomain.Widget{ID: "w1", WorkspaceID: "ws", Name: "Loja", PublicKey: "pk1",
		AllowedOrigins: []string{"https://loja.example.com", "https://*.example.org"}}
	w.Normalize()
	return w
}

func TestFrameMayOnlyBeEmbeddedOnTheAllowedSites(t *testing.T) {
	rec := httptest.NewRecorder()
	publicRouter(activeWidget()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, PublicPrefix+"/pk1/frame", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "frame-ancestors https://loja.example.com https://*.example.org") {
		t.Fatalf("csp = %q", csp)
	}
	if !strings.Contains(csp, "script-src 'self'") || strings.Contains(csp, "unsafe-inline") {
		t.Fatalf("csp allows inline script: %q", csp)
	}
	if !strings.Contains(rec.Body.String(), `data-key="pk1"`) {
		t.Fatal("frame does not carry its key")
	}
}

func TestPausedOrUnknownWidgetHasNoFrame(t *testing.T) {
	paused := activeWidget()
	paused.Status = wcdomain.StatusPaused
	for name, r := range map[string]*mux.Router{"paused": publicRouter(paused), "unknown": publicRouter(nil)} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, PublicPrefix+"/pk1/frame", nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s widget frame status = %d", name, rec.Code)
		}
	}
}

func TestFrameEscapesTheWidgetName(t *testing.T) {
	w := activeWidget()
	w.Name = `"><script>alert(1)</script>`
	rec := httptest.NewRecorder()
	publicRouter(w).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, PublicPrefix+"/pk1/frame", nil))
	if strings.Contains(rec.Body.String(), "<script>alert(1)") {
		t.Fatal("widget name reached the page unescaped")
	}
}

func TestVisitorEndpointsRefuseCrossSiteRequests(t *testing.T) {
	cases := map[string]struct {
		site, origin string
		want         int
	}{
		"other site via fetch metadata": {"cross-site", "https://evil.example", http.StatusForbidden},
		"same site but other origin":    {"same-site", "https://www.vozko.example", http.StatusForbidden},
		"foreign origin only":           {"", "https://evil.example", http.StatusForbidden},
		"no signal at all":              {"", "", http.StatusForbidden},
		"the frame itself":              {"same-origin", apiOrigin, http.StatusUnauthorized},
		"older browser, origin only":    {"", apiOrigin, http.StatusUnauthorized},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, PublicPrefix+"/session/messages", strings.NewReader(`{}`))
			if tc.site != "" {
				req.Header.Set("Sec-Fetch-Site", tc.site)
			}
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			rec := httptest.NewRecorder()
			publicRouter(activeWidget()).ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestLoaderIsServedAsScript(t *testing.T) {
	rec := httptest.NewRecorder()
	publicRouter(activeWidget()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, LoaderPath, nil))
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/javascript") {
		t.Fatalf("loader = %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("loader must not be sniffed")
	}
}

func TestOnlyKnownAssetsAreServed(t *testing.T) {
	rec := httptest.NewRecorder()
	publicRouter(activeWidget()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, PublicPrefix+"/assets/frame.html", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("frame.html as asset = %d", rec.Code)
	}
}

func TestFrameAssetsAreVersionedByContent(t *testing.T) {
	rec := httptest.NewRecorder()
	publicRouter(activeWidget()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, PublicPrefix+"/pk1/frame", nil))
	page := rec.Body.String()
	for _, asset := range []string{"/assets/frame.css?v=" + assetVersion, "/assets/frame.js?v=" + assetVersion} {
		if !strings.Contains(page, asset) {
			t.Fatalf("frame does not reference %s", asset)
		}
	}
}

func TestOnlyTheCurrentAssetVersionIsCachedForever(t *testing.T) {
	cases := map[string]string{
		"/assets/frame.css?v=" + assetVersion: assetsImmutable,
		"/assets/frame.css?v=stale":           "no-cache",
		"/assets/frame.css":                   "no-cache",
	}
	for path, want := range cases {
		rec := httptest.NewRecorder()
		publicRouter(activeWidget()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, PublicPrefix+path, nil))
		if got := rec.Header().Get("Cache-Control"); got != want {
			t.Errorf("%s Cache-Control = %q, want %q", path, got, want)
		}
	}
}
