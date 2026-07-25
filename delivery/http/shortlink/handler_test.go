package shortlink

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	shortlinkdomain "vozko/domain/shortlink"
	"vozko/infra/http/middleware"
)

func newRequest(method, target, body string, authed bool, vars map[string]string) *http.Request {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	if authed {
		r = r.WithContext(context.WithValue(r.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: "user-1"}))
	}
	r.Header.Set("X-Workspace-ID", "ws-1")
	if vars != nil {
		r = mux.SetURLVars(r, vars)
	}
	return r
}

func TestCreateHandler(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		rec := httptest.NewRecorder()
		defaultDeps().build().Create(rec, newRequest(http.MethodPost, "/short-links", `{"targetUrl":"https://x.com"}`, true, nil))
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d body %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "https://vx.co/r/abc123") {
			t.Fatalf("shortUrl not decorated: %s", rec.Body.String())
		}
	})
	t.Run("bad body", func(t *testing.T) {
		rec := httptest.NewRecorder()
		defaultDeps().build().Create(rec, newRequest(http.MethodPost, "/short-links", `{bad`, true, nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", rec.Code)
		}
	})
	t.Run("validation error", func(t *testing.T) {
		rec := httptest.NewRecorder()
		defaultDeps().build().Create(rec, newRequest(http.MethodPost, "/short-links", `{"targetUrl":""}`, true, nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", rec.Code)
		}
	})
	t.Run("unauthorized", func(t *testing.T) {
		rec := httptest.NewRecorder()
		defaultDeps().build().Create(rec, newRequest(http.MethodPost, "/short-links", `{"targetUrl":"https://x.com"}`, false, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d", rec.Code)
		}
	})
}

func TestHandleDomainError(t *testing.T) {
	cases := []struct {
		err    error
		status int
	}{
		{shortlinkdomain.ErrShortLinkNotFound, http.StatusNotFound},
		{shortlinkdomain.ErrCodeTaken, http.StatusConflict},
		{shortlinkdomain.ErrReservedAlias, http.StatusConflict},
		{shortlinkdomain.ErrMaxShortLinksReached, http.StatusUnprocessableEntity},
		{shortlinkdomain.ErrThreatDetected, http.StatusBadRequest},
		{shortlinkdomain.ErrTargetURLBlocked, http.StatusBadRequest},
		{shortlinkdomain.ErrInvalidAliasChar, http.StatusBadRequest},
		{context.DeadlineExceeded, http.StatusInternalServerError},
	}
	for _, c := range cases {
		deps := defaultDeps()
		deps.create = fakeCreate{err: c.err}
		rec := httptest.NewRecorder()
		deps.build().Create(rec, newRequest(http.MethodPost, "/short-links", `{"targetUrl":"https://x.com"}`, true, nil))
		if rec.Code != c.status {
			t.Fatalf("err %v => status %d want %d", c.err, rec.Code, c.status)
		}
	}
}

func TestListStatsGet(t *testing.T) {
	t.Run("list", func(t *testing.T) {
		rec := httptest.NewRecorder()
		defaultDeps().build().List(rec, newRequest(http.MethodGet, "/short-links?page=1&pageSize=20&departmentId=dep", "", true, nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "https://vx.co/r/abc123") {
			t.Fatalf("list = %d %s", rec.Code, rec.Body.String())
		}
	})
	t.Run("list error", func(t *testing.T) {
		deps := defaultDeps()
		deps.list = fakeList{err: context.DeadlineExceeded}
		rec := httptest.NewRecorder()
		deps.build().List(rec, newRequest(http.MethodGet, "/short-links", "", true, nil))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", rec.Code)
		}
	})
	t.Run("stats", func(t *testing.T) {
		rec := httptest.NewRecorder()
		defaultDeps().build().Stats(rec, newRequest(http.MethodGet, "/short-links/stats", "", true, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
	})
	t.Run("stats error", func(t *testing.T) {
		deps := defaultDeps()
		deps.stats = fakeStats{err: context.DeadlineExceeded}
		rec := httptest.NewRecorder()
		deps.build().Stats(rec, newRequest(http.MethodGet, "/short-links/stats", "", true, nil))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", rec.Code)
		}
	})
	t.Run("get success", func(t *testing.T) {
		rec := httptest.NewRecorder()
		defaultDeps().build().Get(rec, newRequest(http.MethodGet, "/short-links/id-1", "", true, map[string]string{"id": "id-1"}))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
	})
	t.Run("get not found", func(t *testing.T) {
		deps := defaultDeps()
		deps.get = fakeGet{err: shortlinkdomain.ErrShortLinkNotFound}
		rec := httptest.NewRecorder()
		deps.build().Get(rec, newRequest(http.MethodGet, "/short-links/x", "", true, map[string]string{"id": "x"}))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d", rec.Code)
		}
	})
}

func TestUpdateDelete(t *testing.T) {
	t.Run("update success", func(t *testing.T) {
		rec := httptest.NewRecorder()
		defaultDeps().build().Update(rec, newRequest(http.MethodPatch, "/short-links/id-1", `{"title":"x"}`, true, map[string]string{"id": "id-1"}))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
	})
	t.Run("update bad body", func(t *testing.T) {
		rec := httptest.NewRecorder()
		defaultDeps().build().Update(rec, newRequest(http.MethodPatch, "/short-links/id-1", `{bad`, true, map[string]string{"id": "id-1"}))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", rec.Code)
		}
	})
	t.Run("update validation", func(t *testing.T) {
		rec := httptest.NewRecorder()
		defaultDeps().build().Update(rec, newRequest(http.MethodPatch, "/short-links/id-1", `{"status":"bogus"}`, true, map[string]string{"id": "id-1"}))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", rec.Code)
		}
	})
	t.Run("update error", func(t *testing.T) {
		deps := defaultDeps()
		deps.update = fakeUpdate{err: shortlinkdomain.ErrShortLinkNotFound}
		rec := httptest.NewRecorder()
		deps.build().Update(rec, newRequest(http.MethodPatch, "/short-links/x", `{}`, true, map[string]string{"id": "x"}))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d", rec.Code)
		}
	})
	t.Run("delete success", func(t *testing.T) {
		rec := httptest.NewRecorder()
		defaultDeps().build().Delete(rec, newRequest(http.MethodDelete, "/short-links/id-1", "", true, map[string]string{"id": "id-1"}))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d", rec.Code)
		}
	})
	t.Run("delete error", func(t *testing.T) {
		deps := defaultDeps()
		deps.del = fakeDelete{err: shortlinkdomain.ErrShortLinkNotFound}
		rec := httptest.NewRecorder()
		deps.build().Delete(rec, newRequest(http.MethodDelete, "/short-links/x", "", true, map[string]string{"id": "x"}))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d", rec.Code)
		}
	})
}

func TestAnalyticsRecentQR(t *testing.T) {
	t.Run("analytics success with window", func(t *testing.T) {
		rec := httptest.NewRecorder()
		defaultDeps().build().Analytics(rec, newRequest(http.MethodGet, "/short-links/id-1/analytics?from=2026-07-01T00:00:00Z&to=bad", "", true, map[string]string{"id": "id-1"}))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
	})
	t.Run("analytics link not found", func(t *testing.T) {
		deps := defaultDeps()
		deps.get = fakeGet{err: shortlinkdomain.ErrShortLinkNotFound}
		rec := httptest.NewRecorder()
		deps.build().Analytics(rec, newRequest(http.MethodGet, "/short-links/x/analytics", "", true, map[string]string{"id": "x"}))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d", rec.Code)
		}
	})
	t.Run("analytics compute error", func(t *testing.T) {
		deps := defaultDeps()
		deps.analytics = fakeAnalytics{err: context.DeadlineExceeded}
		rec := httptest.NewRecorder()
		deps.build().Analytics(rec, newRequest(http.MethodGet, "/short-links/id-1/analytics", "", true, map[string]string{"id": "id-1"}))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", rec.Code)
		}
	})
	t.Run("recent success", func(t *testing.T) {
		rec := httptest.NewRecorder()
		defaultDeps().build().RecentClicks(rec, newRequest(http.MethodGet, "/short-links/id-1/clicks", "", true, map[string]string{"id": "id-1"}))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
	})
	t.Run("recent error", func(t *testing.T) {
		deps := defaultDeps()
		deps.recent = fakeRecent{err: context.DeadlineExceeded}
		rec := httptest.NewRecorder()
		deps.build().RecentClicks(rec, newRequest(http.MethodGet, "/short-links/id-1/clicks", "", true, map[string]string{"id": "id-1"}))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", rec.Code)
		}
	})
	t.Run("qr success", func(t *testing.T) {
		rec := httptest.NewRecorder()
		defaultDeps().build().QR(rec, newRequest(http.MethodGet, "/short-links/id-1/qr?size=200", "", true, map[string]string{"id": "id-1"}))
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
			t.Fatalf("qr = %d %s", rec.Code, rec.Header().Get("Content-Type"))
		}
	})
	t.Run("qr error", func(t *testing.T) {
		deps := defaultDeps()
		deps.qr = fakeQR{err: shortlinkdomain.ErrShortLinkNotFound}
		rec := httptest.NewRecorder()
		deps.build().QR(rec, newRequest(http.MethodGet, "/short-links/x/qr", "", true, map[string]string{"id": "x"}))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d", rec.Code)
		}
	})
}
