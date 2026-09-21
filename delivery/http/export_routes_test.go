package http

import (
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
)

func newGetRequest(t *testing.T, path string) *nethttp.Request {
	t.Helper()
	return httptest.NewRequest(nethttp.MethodGet, path, nil)
}

func TestExportRoutesRegistered(t *testing.T) {
	r := &router{mux: mux.NewRouter()}
	r.setupRoutes()

	registered := make(map[string]bool)
	err := r.mux.Walk(func(route *mux.Route, _ *mux.Router, _ []*mux.Route) error {
		tmpl, err := route.GetPathTemplate()
		if err != nil {
			return nil
		}
		registered[tmpl] = true
		return nil
	})
	if err != nil {
		t.Fatalf("walk routes: %v", err)
	}

	for _, path := range []string{
		"/whatsapp/campaigns/entries/export",
		"/whatsapp/campaigns/{id}/entries/export",
		"/instagram/accounts/{id}/entries/export",
		"/telegram/accounts/{id}/entries/export",
	} {
		if !registered[path] {
			t.Errorf("export route not registered: %s", path)
		}
	}
}

func TestWorkspaceExportRouteDoesNotShadowCampaignRoute(t *testing.T) {
	r := &router{mux: mux.NewRouter()}
	r.setupRoutes()

	var match mux.RouteMatch
	req := newGetRequest(t, "/whatsapp/campaigns/entries/export")
	if !r.mux.Match(req, &match) {
		t.Fatalf("workspace export path did not match any route")
	}
	if match.Vars["id"] != "" {
		t.Errorf("workspace export matched the per-campaign route with id=%q", match.Vars["id"])
	}

	var campaignMatch mux.RouteMatch
	campaignReq := newGetRequest(t, "/whatsapp/campaigns/abc-123/entries/export")
	if !r.mux.Match(campaignReq, &campaignMatch) {
		t.Fatalf("per-campaign export path did not match any route")
	}
	if got := campaignMatch.Vars["id"]; got != "abc-123" {
		t.Errorf("per-campaign export id = %q, want abc-123", got)
	}
}
