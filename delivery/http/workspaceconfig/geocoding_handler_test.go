package workspaceconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	"vozko/domain/geocoding"
	wsc "vozko/domain/workspace_config"
	"vozko/infra/http/middleware"
	workspace_config_usecase "vozko/usecases/workspace_config"
)

type stubGeocoding struct {
	view   workspace_config_usecase.GeocodingSettingsView
	err    error
	editor wsc.Caller
	change *geocoding.Change
}

func (s *stubGeocoding) Get(_ context.Context, _ string, editor wsc.Caller) (workspace_config_usecase.GeocodingSettingsView, error) {
	s.editor = editor
	return s.view, s.err
}

func (s *stubGeocoding) Change(_ context.Context, _ string, editor wsc.Caller, change geocoding.Change) (workspace_config_usecase.GeocodingSettingsView, error) {
	s.editor, s.change = editor, &change
	return s.view, s.err
}

func geocodingRouter(service GeocodingSettingsService) http.Handler {
	h := NewWorkspaceConfigHandler(nil, nil, nil)
	if service != nil {
		h.SetGeocodingSettings(service)
	}
	r := mux.NewRouter()
	RegisterProtectedRoutes(r, h)
	return r
}

func callGeocoding(handler http.Handler, method string, claims *auth.Claims, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/workspaces/ws-1/geocoding", bytes.NewReader([]byte(body)))
	if claims != nil {
		req = req.WithContext(context.WithValue(req.Context(), middleware.ClaimsContextKey, claims))
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestGetGeocodingSettingsAnswersTheOptInWithUsage(t *testing.T) {
	opted := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	settings := geocoding.Settings{Provider: geocoding.ProviderOpenCage, ProviderChangedBy: "owner-1", ProviderChangedAt: &opted}
	slot, _ := settings.SlotAt(time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC))
	service := &stubGeocoding{view: workspace_config_usecase.GeocodingSettingsView{
		Settings: settings, ProviderChangedByName: "Ana Dona", Slot: &slot,
		Usage:     geocoding.Usage{CycleStart: slot.CycleStart, Requests: 42, Day: slot.Day, DayRequests: 5},
		Providers: []geocoding.Provider{geocoding.ProviderOpenCage}, CanChangeProvider: true,
	}}
	rec := callGeocoding(geocodingRouter(service), http.MethodGet, owner, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got GeocodingSettingsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Provider != "opencage" || !got.Enabled || got.ProviderChangedByName != "Ana Dona" || got.ProviderChangedAt == nil || *got.ProviderChangedAt != "2026-10-02T09:00:00Z" {
		t.Fatalf("response = %+v", got)
	}
	if got.MonthlyCeiling != 5000 || got.DailyShare != slot.DailyLimit || got.UsedThisCycle != 42 || got.UsedToday != 5 || got.CycleStart == nil || got.NextCycleStart == nil {
		t.Fatalf("usage = %+v", got)
	}
	if !got.CanChangeProvider || got.CanChangeCeiling || len(got.AvailableProviders) != 1 || got.Attribution != "IBGE, CNEFE 2022" {
		t.Fatalf("rights and sources = %+v", got)
	}
	if service.editor.UserID != "owner-1" || service.editor.PlatformAdmin {
		t.Fatalf("editor = %+v", service.editor)
	}
}

func TestUpdateGeocodingSettingsPassesOnlyWhatWasSent(t *testing.T) {
	service := &stubGeocoding{}
	rec := callGeocoding(geocodingRouter(service), http.MethodPut, owner, `{"provider":"opencage"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if service.change == nil || service.change.Provider == nil || *service.change.Provider != geocoding.ProviderOpenCage || service.change.MonthlyCeiling != nil {
		t.Fatalf("change = %+v", service.change)
	}
	off := callGeocoding(geocodingRouter(service), http.MethodPut, owner, `{"provider":""}`)
	if off.Code != http.StatusOK || service.change.Provider == nil || *service.change.Provider != "" {
		t.Fatalf("an opt-out must reach the use case as an empty provider: %d %+v", off.Code, service.change)
	}
}

func TestGeocodingSettingsErrors(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		method string
		body   string
		status int
		code   string
	}{
		{"not a member", workspace_config_usecase.ErrNotWorkspaceMember, http.MethodGet, "", http.StatusForbidden, "geocoding_forbidden"},
		{"a member choosing the provider", geocoding.ErrSettingsForbidden, http.MethodPut, `{"provider":"opencage"}`, http.StatusForbidden, "geocoding_forbidden"},
		{"ceiling by the owner", geocoding.ErrCeilingForbidden, http.MethodPut, `{"monthlyCeiling":9000}`, http.StatusForbidden, "geocoding_ceiling_forbidden"},
		{"empty change", geocoding.ErrChangeEmpty, http.MethodPut, `{}`, http.StatusBadRequest, "geocoding_change_empty"},
		{"unknown provider", geocoding.ErrProviderUnknown, http.MethodPut, `{"provider":"google"}`, http.StatusBadRequest, "geocoding_provider_unknown"},
		{"provider missing on the server", geocoding.ErrProviderNotConfigured, http.MethodPut, `{"provider":"opencage"}`, http.StatusConflict, "geocoding_provider_not_configured"},
		{"invalid ceiling", geocoding.ErrCeilingInvalid, http.MethodPut, `{"monthlyCeiling":-1}`, http.StatusBadRequest, "geocoding_ceiling_invalid"},
		{"a read failure", errors.New("db down"), http.MethodGet, "", http.StatusInternalServerError, "geocoding_settings_unreadable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := callGeocoding(geocodingRouter(&stubGeocoding{err: tt.err}), tt.method, owner, tt.body)
			if rec.Code != tt.status || codeOf(t, rec) != tt.code {
				t.Fatalf("status = %d code = %q, want %d %q", rec.Code, codeOf(t, rec), tt.status, tt.code)
			}
		})
	}
	stranger := callGeocoding(geocodingRouter(&stubGeocoding{err: workspace_config_usecase.ErrNotWorkspaceMember}), http.MethodGet, owner, "")
	if !strings.Contains(stranger.Body.String(), "Você não é membro deste workspace") {
		t.Fatalf("a non member reading = %s, want the membership refusal, not the provider rule", stranger.Body.String())
	}
	if rec := callGeocoding(geocodingRouter(&stubGeocoding{}), http.MethodPut, owner, `{"provider":"opencage","extra":1}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("an unknown key = %d, want 400", rec.Code)
	}
	if rec := callGeocoding(geocodingRouter(nil), http.MethodGet, owner, ""); rec.Code != http.StatusServiceUnavailable || codeOf(t, rec) != "geocoding_unavailable" {
		t.Fatalf("an unwired route = %d %q, want 503 geocoding_unavailable", rec.Code, codeOf(t, rec))
	}
	if rec := callGeocoding(geocodingRouter(&stubGeocoding{}), http.MethodGet, nil, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no session = %d, want 401", rec.Code)
	}
}

func TestGeocodingSettingsResponseCarriesTheExhaustionVerdict(t *testing.T) {
	settings := geocoding.Settings{Provider: geocoding.ProviderOpenCage}
	slot, _ := settings.SlotAt(time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC))
	tests := []struct {
		name  string
		slot  *geocoding.Slot
		usage geocoding.Usage
		want  string
	}{
		{"room left", &slot, geocoding.Usage{CycleStart: slot.CycleStart, Requests: 1, Day: slot.Day, DayRequests: 1}, ""},
		{"today's share used", &slot, geocoding.Usage{CycleStart: slot.CycleStart, Requests: slot.DailyLimit, Day: slot.Day, DayRequests: slot.DailyLimit}, "daily"},
		{"the month used", &slot, geocoding.Usage{CycleStart: slot.CycleStart, Requests: slot.MonthlyLimit, Day: slot.Day, DayRequests: 0}, "monthly"},
		{"no slot", nil, geocoding.Usage{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toGeocodingSettingsResponse(workspace_config_usecase.GeocodingSettingsView{Settings: settings, Slot: tt.slot, Usage: tt.usage})
			if got.Exhausted != tt.want {
				t.Fatalf("exhausted = %q, want %q", got.Exhausted, tt.want)
			}
		})
	}
	raw, _ := json.Marshal(toGeocodingSettingsResponse(workspace_config_usecase.GeocodingSettingsView{Settings: settings}))
	if !strings.Contains(string(raw), `"exhausted":""`) {
		t.Fatalf("body = %s, want the verdict always present", raw)
	}
}

func TestGeocodingSettingsResponseSaysWhenTheProviderIsPaused(t *testing.T) {
	settings := geocoding.Settings{Provider: geocoding.ProviderOpenCage}
	since := time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC)
	pause := geocoding.ProviderPause{Provider: geocoding.ProviderOpenCage, Reason: "key_rejected", Since: since, Until: since.Add(geocoding.AccountPause)}
	tests := []struct {
		name string
		view workspace_config_usecase.GeocodingSettingsView
		want string
	}{
		{"no pause", workspace_config_usecase.GeocodingSettingsView{Settings: settings}, `"providerPause":null`},
		{"paused", workspace_config_usecase.GeocodingSettingsView{Settings: settings, Pause: &pause},
			`"providerPause":{"state":"paused","reason":"key_rejected","since":"2026-10-08T15:00:00Z","until":"2026-10-08T16:00:00Z"}`},
		{"unreadable", workspace_config_usecase.GeocodingSettingsView{Settings: settings, PauseUnreadable: true}, `"providerPause":{"state":"unknown"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, _ := json.Marshal(toGeocodingSettingsResponse(tt.view))
			if !strings.Contains(string(raw), tt.want) {
				t.Fatalf("body = %s, want %s", raw, tt.want)
			}
		})
	}
}

func TestGeocodingPauseReasonEnumListsEveryAccountRefusal(t *testing.T) {
	field, _ := reflect.TypeOf(GeocodingProviderPauseResponse{}).FieldByName("Reason")
	documented := strings.Split(field.Tag.Get("enums"), ",")
	reasons := geocoding.AccountRefusalReasons()
	if len(documented) != len(reasons) {
		t.Fatalf("documented reasons = %v, want %v", documented, reasons)
	}
	for i, reason := range reasons {
		if documented[i] != string(reason) {
			t.Fatalf("documented reasons = %v, want %v", documented, reasons)
		}
	}
}
