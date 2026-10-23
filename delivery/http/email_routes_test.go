package http

import (
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	emailoutreachhttp "vozko/delivery/http/emailoutreach"
)

func TestEmailRoutesResolveDespiteTheSendingSubrouter(t *testing.T) {
	r := &router{mux: mux.NewRouter()}
	r.setupRoutes()

	cases := []struct {
		method string
		path   string
	}{
		{method: nethttp.MethodGet, path: "/email/setup"},
		{method: nethttp.MethodPost, path: "/email/domains"},
		{method: nethttp.MethodGet, path: "/email/senders"},
		{method: nethttp.MethodGet, path: "/email/suppressions"},
		{method: nethttp.MethodGet, path: "/email/inboxes"},
		{method: nethttp.MethodPut, path: "/email/inboxes/2f0e4c1a-0000-4000-8000-000000000001"},
		{method: nethttp.MethodGet, path: "/email/overview"},
		{method: nethttp.MethodGet, path: "/email/campaigns"},
		{method: nethttp.MethodPost, path: "/email/render"},
		{method: nethttp.MethodGet, path: "/email/designs/starters"},
		{method: nethttp.MethodGet, path: "/email/pricing"},
		{method: nethttp.MethodGet, path: "/admin/email/accounts"},
		{method: nethttp.MethodPost, path: "/webhooks/email/sns"},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			var match mux.RouteMatch
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if !r.mux.Match(req, &match) || match.MatchErr != nil {
				t.Fatalf("%s %s does not resolve (err %v)", tc.method, tc.path, match.MatchErr)
			}
		})
	}
}

func TestRetiredEmailRoutesAreGone(t *testing.T) {
	r := &router{mux: mux.NewRouter()}
	r.setupRoutes()

	for _, path := range []string{
		"/webhooks/email/entri",
		"/email/domains/2f0e4c1a-0000-4000-8000-000000000001/entri-session",
		"/email/campaigns/2f0e4c1a-0000-4000-8000-000000000001/validate",
	} {
		t.Run(path, func(t *testing.T) {
			var match mux.RouteMatch
			if r.mux.Match(httptest.NewRequest(nethttp.MethodPost, path, nil), &match) && match.MatchErr == nil {
				t.Fatalf("POST %s still resolves", path)
			}
		})
	}
}

func TestEmailOutreachRoutesResolveBesideTheSendingSubrouter(t *testing.T) {
	r := &router{mux: mux.NewRouter()}
	r.email.Outreach = &emailoutreachhttp.Handler{}
	r.setupRoutes()

	for _, tc := range []struct {
		method string
		path   string
	}{
		{method: nethttp.MethodPost, path: "/email/outreach/conversations"},
		{method: nethttp.MethodGet, path: "/email/outreach/quote"},
	} {
		var match mux.RouteMatch
		if !r.mux.Match(httptest.NewRequest(tc.method, tc.path, nil), &match) || match.MatchErr != nil {
			t.Fatalf("%s %s does not resolve (err %v)", tc.method, tc.path, match.MatchErr)
		}
	}
}
