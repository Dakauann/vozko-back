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
	"time"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	"vozko/domain/cache"
	"vozko/domain/geocoding"
	"vozko/domain/leadmap"
	"vozko/infra/http/middleware"
)

type stubPlatformUsage struct {
	page   geocoding.PlatformPage
	err    error
	editor geocoding.Editor
	query  geocoding.PlatformQuery
}

func (s *stubPlatformUsage) List(_ context.Context, editor geocoding.Editor, query geocoding.PlatformQuery) (geocoding.PlatformPage, error) {
	s.editor, s.query = editor, query
	return s.page, s.err
}

func platformRouter(service GeocodingPlatformService) http.Handler {
	h := NewWorkspaceConfigHandler(nil, nil, nil)
	if service != nil {
		h.SetGeocodingPlatformUsage(service)
	}
	r := mux.NewRouter()
	RegisterAdminRoutes(r, h)
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

func TestPlatformGeocodingListsUsageHistoryAndCoverage(t *testing.T) {
	now := time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC)
	period := geocoding.PeriodAt(now)
	ceilingValue := int64(800)
	months := geocoding.MonthsOf(period, []geocoding.MonthUsage{{CycleStart: period.CycleStart, Requests: 120}, {CycleStart: period.HistoryCycles()[1], Requests: 300}})
	service := &stubPlatformUsage{page: geocoding.PlatformPage{
		Period: period, Page: 2, PageSize: 20, TotalItems: 41,
		Items: []geocoding.WorkspaceGeocoding{{
			Workspace: geocoding.PlatformWorkspace{ID: "ws-1", Name: "Escola"},
			Settings:  geocoding.Settings{WorkspaceID: "ws-1", Provider: geocoding.ProviderOpenCage, MonthlyCeiling: &ceilingValue},
			Usage:     geocoding.Usage{CycleStart: period.CycleStart, Requests: 120, Day: period.Day, DayRequests: 9},
			Months:    months,
			Coverage:  geocoding.Coverage{Summary: leadmap.Summary{Total: 10, OnMap: 4, Approximate: 2, WithoutAddress: 4, Pending: 1}},
		}},
	}}
	rec := callPlatform(platformRouter(service), platformStaff, "?page=2&pageSize=20&search=esc")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !service.editor.CanReadPlatformUsage() || service.query != (geocoding.PlatformQuery{Page: 2, PageSize: 20, Search: "esc"}) {
		t.Fatalf("service got %+v %+v", service.editor, service.query)
	}
	var got GeocodingPlatformResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.TotalItems != 41 || got.TotalPages != 3 || got.Page != 2 || len(got.Cycles) != geocoding.HistoryCycles || got.CycleStart != period.CycleStart.UTC().Format(time.RFC3339) {
		t.Fatalf("response = %+v", got)
	}
	if len(got.Items) != 1 {
		t.Fatalf("items = %+v", got.Items)
	}
	item := got.Items[0]
	if item.WorkspaceID != "ws-1" || item.WorkspaceName != "Escola" || item.Provider != "opencage" || !item.Enabled || item.MonthlyCeiling != 800 ||
		item.UsedThisCycle != 120 || item.UsedToday != 9 || len(item.Months) != geocoding.HistoryCycles || item.Months[1].Requests != 300 {
		t.Fatalf("item = %+v", item)
	}
	c := item.Coverage
	if c.Total != 10 || c.WithAddress != 6 || c.WithoutAddress != 4 || c.OnMap != 4 || c.Approximate != 2 || c.Pending != 1 || c.AddressShare != 0.6 || c.MapShare != 0.4 {
		t.Fatalf("coverage = %+v", c)
	}
}

func TestPlatformGeocodingRefusals(t *testing.T) {
	tests := []struct {
		name    string
		service *stubPlatformUsage
		claims  *auth.Claims
		status  int
		code    string
	}{
		{"no session", &stubPlatformUsage{}, nil, http.StatusUnauthorized, ""},
		{"not a platform admin", &stubPlatformUsage{err: geocoding.ErrPlatformForbidden}, owner, http.StatusForbidden, "geocoding_platform_forbidden"},
		{"busy", &stubPlatformUsage{err: cache.ErrGateBusy}, platformStaff, http.StatusServiceUnavailable, ""},
		{"too slow", &stubPlatformUsage{err: context.DeadlineExceeded}, platformStaff, http.StatusGatewayTimeout, ""},
		{"unreadable", &stubPlatformUsage{err: errors.New("db down")}, platformStaff, http.StatusInternalServerError, "geocoding_usage_unreadable"},
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

func TestPlatformGeocodingHandlerIsNotInThePublicDocs(t *testing.T) {
	source, err := os.ReadFile("geocoding_platform_handler.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(source), "@Router") || strings.Contains(string(source), "@Summary") {
		t.Fatal("a platform admin handler carries swag annotations, which would publish it")
	}
}
