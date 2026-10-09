package workspaceconfig

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	"vozko/domain/cache"
	"vozko/domain/geocoding"
	"vozko/domain/leadmap"
	"vozko/infra/http/middleware"
)

type stubPlatformCoverage struct {
	page   geocoding.PlatformPage
	err    error
	caller geocoding.Caller
	query  geocoding.PlatformQuery
}

func (s *stubPlatformCoverage) List(_ context.Context, caller geocoding.Caller, query geocoding.PlatformQuery) (geocoding.PlatformPage, error) {
	s.caller, s.query = caller, query
	return s.page, s.err
}

func platformRouter(service GeocodingPlatformService) http.Handler {
	h := NewWorkspaceConfigHandler(nil, nil, nil)
	if service != nil {
		h.SetGeocodingPlatformCoverage(service)
	}
	r := mux.NewRouter()
	RegisterAdminRoutes(r, h)
	RegisterProtectedRoutes(r, h)
	return r
}

func callPlatform(handler http.Handler, claims *auth.Claims, query string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/admin/geocoding/workspaces"+query, nil)
	if claims != nil {
		req = req.WithContext(context.WithValue(req.Context(), middleware.ClaimsContextKey, claims))
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

var platformStaff = &auth.Claims{UserID: "staff-1", Role: "admin"}

func TestPlatformGeocodingListsTheAddressCoverageOfEachWorkspace(t *testing.T) {
	service := &stubPlatformCoverage{page: geocoding.PlatformPage{
		Page: 2, PageSize: 20, TotalItems: 41,
		Items: []geocoding.WorkspaceCoverage{{
			Workspace: geocoding.PlatformWorkspace{ID: "ws-1", Name: "Escola"},
			Coverage:  geocoding.Coverage{Summary: leadmap.Summary{Total: 10, OnMap: 4, Approximate: 2, WithoutAddress: 4, Pending: 1}},
		}},
	}}
	rec := callPlatform(platformRouter(service), platformStaff, "?page=2&pageSize=20&search=esc")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !service.caller.CanReadEveryWorkspace() || service.query != (geocoding.PlatformQuery{Page: 2, PageSize: 20, Search: "esc"}) {
		t.Fatalf("service got %+v %+v", service.caller, service.query)
	}
	var got GeocodingPlatformResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.TotalItems != 41 || got.TotalPages != 3 || got.Page != 2 || len(got.Items) != 1 {
		t.Fatalf("response = %+v", got)
	}
	item := got.Items[0]
	if item.WorkspaceID != "ws-1" || item.WorkspaceName != "Escola" {
		t.Fatalf("item = %+v", item)
	}
	c := item.Coverage
	if c.Total != 10 || c.WithAddress != 6 || c.WithoutAddress != 4 || c.OnMap != 4 || c.Approximate != 2 || c.Pending != 1 || c.AddressShare != 0.6 || c.MapShare != 0.4 {
		t.Fatalf("coverage = %+v", c)
	}
	body := rec.Body.String()
	for _, retired := range []string{"cycleStart", "nextCycleStart", "cycles", "provider", "monthlyCeiling", "usedThisCycle", "usedToday", "months", "quotaExceeded", "refused"} {
		if strings.Contains(body, `"`+retired+`"`) {
			t.Fatalf("body = %s, still carries %q from the retired external provider", body, retired)
		}
	}
}

func TestPlatformGeocodingRefusals(t *testing.T) {
	tests := []struct {
		name    string
		service *stubPlatformCoverage
		claims  *auth.Claims
		status  int
		code    string
	}{
		{"no session", &stubPlatformCoverage{}, nil, http.StatusUnauthorized, ""},
		{"not a platform admin", &stubPlatformCoverage{err: geocoding.ErrPlatformForbidden}, owner, http.StatusForbidden, "geocoding_platform_forbidden"},
		{"busy", &stubPlatformCoverage{err: cache.ErrGateBusy}, platformStaff, http.StatusServiceUnavailable, ""},
		{"too slow", &stubPlatformCoverage{err: context.DeadlineExceeded}, platformStaff, http.StatusGatewayTimeout, ""},
		{"unreadable", &stubPlatformCoverage{err: errors.New("db down")}, platformStaff, http.StatusInternalServerError, "geocoding_coverage_unreadable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := callPlatform(platformRouter(tt.service), tt.claims, "")
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, tt.status, rec.Body.String())
			}
			if tt.code != "" && !strings.Contains(rec.Body.String(), tt.code) {
				t.Fatalf("body = %s, want code %s", rec.Body.String(), tt.code)
			}
		})
	}
	if rec := callPlatform(platformRouter(nil), platformStaff, ""); rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "geocoding_unavailable") {
		t.Fatalf("a server without the service = %d %s", rec.Code, rec.Body.String())
	}
}

func TestTheWorkspaceGeocodingSettingsRouteIsGone(t *testing.T) {
	router := platformRouter(&stubPlatformCoverage{})
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		req := httptest.NewRequest(method, "/workspaces/5b0f4a7e-3c2d-4e8a-9f61-2d7c1b9e0a44/geocoding", strings.NewReader(`{"provider":"opencage"}`))
		req = req.WithContext(context.WithValue(req.Context(), middleware.ClaimsContextKey, owner))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s /workspaces/{id}/geocoding = %d, want no route: positions come only from the IBGE reference", method, rec.Code)
		}
	}
}

func TestPlatformGeocodingHandlerIsNotInThePublicDocs(t *testing.T) {
	source, err := os.ReadFile("geocoding_platform_handler.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(source), "@Router") || strings.Contains(string(source), "@Summary") {
		t.Fatal("a platform admin handler carries swag annotations, which would publish it")
	}
}
