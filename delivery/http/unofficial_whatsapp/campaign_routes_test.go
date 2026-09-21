package unofficial_whatsapp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
)

func TestRegisterCampaignRoutes_AppliesRBAC(t *testing.T) {
	router := mux.NewRouter()
	ac := &recordingAC{calls: map[string]string{}}

	RegisterCampaignRoutes(router, &CampaignHandler{}, ac.fn)

	const res = "unofficial_whatsapp_campaigns"
	cases := []struct {
		method string
		path   string
		want   string
	}{
		{http.MethodGet, "/unofficial-whatsapp/campaigns", res + ":read"},
		{http.MethodGet, "/unofficial-whatsapp/campaigns/archived", res + ":read"},
		{http.MethodGet, "/unofficial-whatsapp/campaigns/summary", res + ":read"},
		{http.MethodGet, "/unofficial-whatsapp/campaigns/c-1", res + ":read"},
		{http.MethodGet, "/unofficial-whatsapp/campaigns/c-1/entries", res + ":read"},

		{http.MethodPost, "/unofficial-whatsapp/campaigns", res + ":create"},
		{http.MethodPut, "/unofficial-whatsapp/campaigns/c-1", res + ":update"},
		{http.MethodDelete, "/unofficial-whatsapp/campaigns/c-1", res + ":delete"},
		{http.MethodPatch, "/unofficial-whatsapp/campaigns/c-1/department", res + ":update"},
		{http.MethodPatch, "/unofficial-whatsapp/campaigns/c-1/archive", res + ":update"},
		{http.MethodPatch, "/unofficial-whatsapp/campaigns/c-1/unarchive", res + ":update"},

		{http.MethodPost, "/unofficial-whatsapp/campaigns/c-1/entries", res + ":update"},
		{http.MethodPatch, "/unofficial-whatsapp/campaigns/c-1/entries/e-1", res + ":update"},
		{http.MethodDelete, "/unofficial-whatsapp/campaigns/c-1/entries/e-1", res + ":delete"},

		{http.MethodPost, "/unofficial-whatsapp/campaigns/c-1/start", res + ":start"},
		{http.MethodPost, "/unofficial-whatsapp/campaigns/c-1/quick-send", res + ":start"},
		{http.MethodPost, "/unofficial-whatsapp/campaigns/c-1/pause", res + ":stop"},
		{http.MethodPost, "/unofficial-whatsapp/campaigns/c-1/stop", res + ":stop"},

		{http.MethodPost, "/unofficial-whatsapp/campaigns/c-1/validate", res + ":update"},

		{http.MethodPost, "/unofficial-whatsapp/campaigns/c-1/reset/prepare", res + ":update"},
		{http.MethodPost, "/unofficial-whatsapp/campaigns/c-1/reset", res + ":update"},
		{http.MethodPost, "/unofficial-whatsapp/campaigns/c-1/clear-history/prepare", res + ":update"},
		{http.MethodPost, "/unofficial-whatsapp/campaigns/c-1/clear-history", res + ":update"},
	}

	for _, c := range cases {
		req := httptest.NewRequest(c.method, c.path, nil)
		router.ServeHTTP(httptest.NewRecorder(), req)

		got, registered := ac.calls[c.method+" "+c.path]
		if !registered {
			t.Errorf("%s %s is not registered", c.method, c.path)
			continue
		}
		if got != c.want {
			t.Errorf("%s %s guarded by %s, want %s", c.method, c.path, got, c.want)
		}
	}
}

func TestRegisterCampaignRoutes_NilHandlerRegistersNothing(t *testing.T) {
	router := mux.NewRouter()
	ac := &recordingAC{calls: map[string]string{}}

	RegisterCampaignRoutes(router, nil, ac.fn)

	req := httptest.NewRequest(http.MethodGet, "/unofficial-whatsapp/campaigns", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 when the channel is not wired", rec.Code)
	}
	if len(ac.calls) != 0 {
		t.Fatalf("routes were registered with a nil handler: %v", ac.calls)
	}
}

func TestCampaignsAreASeparateResourceFromInstances(t *testing.T) {
	router := mux.NewRouter()
	ac := &recordingAC{calls: map[string]string{}}

	RegisterCampaignRoutes(router, &CampaignHandler{}, ac.fn)
	router.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodPost, "/unofficial-whatsapp/campaigns", nil))

	if got := ac.calls["POST /unofficial-whatsapp/campaigns"]; got == "unofficial_whatsapp_instances:create" {
		t.Fatal("campaigns are guarded by the instances resource")
	}
}
