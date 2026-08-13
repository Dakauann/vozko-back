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

// TestExportRoutesRegistered pins every CSV export path to the router.
//
// These handlers were built, wired into the container and threaded into the
// router struct — and then never registered, so every export in the product
// answered 404 for as long as the feature had existed. The frontend reads a 404
// from this path as "nothing to export", so operators were told their campaigns
// were empty instead of being told the endpoint was missing. Nothing failed;
// there was simply no test that asked whether the route existed.
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

// TestWorkspaceExportRouteDoesNotShadowCampaignRoute guards the one collision
// this path shape could have: /whatsapp/campaigns/entries/export sits where a
// campaign id would, so a router that matched it against /{id}/entries would
// send workspace exports to the single-campaign handler with the id "entries".
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
