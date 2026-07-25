package shortlink

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	shortlinkdomain "vozko/domain/shortlink"
)

func codeRequest(method, target string, headers map[string]string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return mux.SetURLVars(r, map[string]string{"code": "abc"})
}

func TestRedirect_OK_CFHeaders(t *testing.T) {
	pub := &fakePublish{}
	deps := defaultDeps()
	deps.publish = pub
	rec := httptest.NewRecorder()
	deps.build().Redirect(rec, codeRequest(http.MethodGet, "/r/abc?utm_source=news", map[string]string{
		"CF-IPCountry":    "BR",
		"Accept-Language": "pt-BR,pt;q=0.9",
	}))
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Header().Get("Location") != "https://x.com" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("headers = %v", rec.Header())
	}
	if !pub.called {
		t.Fatal("click should be enqueued")
	}
}

func TestRedirect_OK_VercelHeaders(t *testing.T) {
	pub := &fakePublish{}
	deps := defaultDeps()
	deps.publish = pub
	rec := httptest.NewRecorder()
	deps.build().Redirect(rec, codeRequest(http.MethodGet, "/r/abc", map[string]string{
		"X-Vercel-IP-Country": "US",
	}))
	if rec.Code != http.StatusFound || !pub.called {
		t.Fatalf("vercel redirect = %d %v", rec.Code, pub.called)
	}
}

func TestRedirect_Password(t *testing.T) {
	deps := defaultDeps()
	deps.resolve = fakeResolve{resolved: &shortlinkdomain.ResolvedLink{State: shortlinkdomain.ResolvePassword, Code: "abc"}}
	rec := httptest.NewRecorder()
	deps.build().Redirect(rec, codeRequest(http.MethodGet, "/r/abc", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `action="/r/abc/unlock"`) {
		t.Fatalf("interstitial missing form: %s", rec.Body.String())
	}
}

func TestRedirect_NotFoundAndGone(t *testing.T) {
	for _, state := range []shortlinkdomain.ResolveState{shortlinkdomain.ResolveNotFound, shortlinkdomain.ResolveGone} {
		deps := defaultDeps()
		deps.resolve = fakeResolve{resolved: &shortlinkdomain.ResolvedLink{State: state}}
		rec := httptest.NewRecorder()
		deps.build().Redirect(rec, codeRequest(http.MethodGet, "/r/abc", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("state %s => status %d", state, rec.Code)
		}
	}
}

func TestRedirect_ResolveError(t *testing.T) {
	deps := defaultDeps()
	deps.resolve = fakeResolve{err: shortlinkdomain.ErrShortLinkNotFound}
	rec := httptest.NewRecorder()
	deps.build().Redirect(rec, codeRequest(http.MethodGet, "/r/abc", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestUnlock(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		pub := &fakePublish{}
		deps := defaultDeps()
		deps.publish = pub
		rec := httptest.NewRecorder()
		deps.build().Unlock(rec, codeRequest(http.MethodPost, "/r/abc/unlock", nil))
		if rec.Code != http.StatusFound || !pub.called {
			t.Fatalf("unlock = %d %v", rec.Code, pub.called)
		}
	})
	t.Run("wrong password", func(t *testing.T) {
		deps := defaultDeps()
		deps.unlock = fakeUnlock{err: shortlinkdomain.ErrInvalidPassword}
		rec := httptest.NewRecorder()
		deps.build().Unlock(rec, codeRequest(http.MethodPost, "/r/abc/unlock", nil))
		if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "Senha incorreta") {
			t.Fatalf("wrong password = %d %s", rec.Code, rec.Body.String())
		}
	})
	t.Run("password required", func(t *testing.T) {
		deps := defaultDeps()
		deps.unlock = fakeUnlock{err: shortlinkdomain.ErrPasswordRequired}
		rec := httptest.NewRecorder()
		deps.build().Unlock(rec, codeRequest(http.MethodPost, "/r/abc/unlock", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d", rec.Code)
		}
	})
	t.Run("not found", func(t *testing.T) {
		deps := defaultDeps()
		deps.unlock = fakeUnlock{err: shortlinkdomain.ErrShortLinkNotFound}
		rec := httptest.NewRecorder()
		deps.build().Unlock(rec, codeRequest(http.MethodPost, "/r/abc/unlock", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d", rec.Code)
		}
	})
	t.Run("other error", func(t *testing.T) {
		deps := defaultDeps()
		deps.unlock = fakeUnlock{err: shortlinkdomain.ErrInvalidRedirectType}
		rec := httptest.NewRecorder()
		deps.build().Unlock(rec, codeRequest(http.MethodPost, "/r/abc/unlock", nil))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", rec.Code)
		}
	})
}
