package mercadolivre

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
)

func TestCatalogRoutesAreGuardedPerThePlan(t *testing.T) {
	router := mux.NewRouter()
	ac := &recordingAC{calls: map[string]string{}}
	RegisterCatalogRoutes(router, &CatalogHandler{}, ac.fn)

	const acc = "/mercadolivre/accounts/acct-1"
	cases := []struct {
		method, path, want string
	}{
		{http.MethodGet, acc + "/listings", "mercadolivre_accounts:read"},
		{http.MethodGet, acc + "/listings/MLB1/thumbnail", "mercadolivre_accounts:read"},
		{http.MethodGet, acc + "/listings/MLB1/questions", "mercadolivre_accounts:read"},
		{http.MethodGet, acc + "/questions", "mercadolivre_accounts:read"},
		{http.MethodGet, acc + "/questions/response-time", "mercadolivre_accounts:read"},
		{http.MethodGet, acc + "/questions/13000000001", "mercadolivre_accounts:read"},
		{http.MethodPost, acc + "/questions/13000000001/answer", "mercadolivre_accounts:update"},
		{http.MethodDelete, acc + "/questions/13000000001", "mercadolivre_accounts:update"},
		{http.MethodPost, acc + "/questions/13000000001/block-author", "mercadolivre_accounts:update"},
		{http.MethodDelete, acc + "/questions/13000000001/block-author", "mercadolivre_accounts:update"},
		{http.MethodGet, acc + "/comment-rules", "mercadolivre_accounts:read"},
		{http.MethodPost, acc + "/comment-rules", "mercadolivre_accounts:update"},
		{http.MethodPatch, acc + "/comment-rules/rule-1", "mercadolivre_accounts:update"},
		{http.MethodDelete, acc + "/comment-rules/rule-1", "mercadolivre_accounts:update"},
		{http.MethodGet, "/mercadolivre/conversations/conv-1/reasons", "conversations:read"},
		{http.MethodPost, "/mercadolivre/conversations/conv-1/reasons", "conversations:send"},
		{http.MethodGet, "/mercadolivre/conversations/conv-1/pack", "conversations:read"},
	}
	for _, c := range cases {
		req := httptest.NewRequest(c.method, c.path, nil)
		var match mux.RouteMatch
		if !router.Match(req, &match) || match.MatchErr != nil {
			t.Errorf("%s %s is not registered", c.method, c.path)
			continue
		}
		router.ServeHTTP(httptest.NewRecorder(), req)
		if got := ac.calls[c.method+" "+c.path]; got != c.want {
			t.Errorf("%s %s guarded by %q, want %q", c.method, c.path, got, c.want)
		}
	}
	for _, c := range []struct{ method, path string }{
		{http.MethodPut, acc + "/questions/13000000001"},
		{http.MethodPut, acc + "/comment-rules/rule-1"},
		{http.MethodDelete, "/mercadolivre/conversations/conv-1/reasons"},
		{http.MethodPost, "/mercadolivre/conversations/conv-1/pack"},
	} {
		var match mux.RouteMatch
		if router.Match(httptest.NewRequest(c.method, c.path, nil), &match) && match.MatchErr == nil {
			t.Errorf("%s %s must not be routed", c.method, c.path)
		}
	}
}

func TestANilCatalogRegistersNothing(t *testing.T) {
	router := mux.NewRouter()
	RegisterCatalogRoutes(router, nil, (&recordingAC{calls: map[string]string{}}).fn)
	var match mux.RouteMatch
	if router.Match(httptest.NewRequest(http.MethodGet, "/mercadolivre/accounts/a/listings", nil), &match) {
		t.Fatal("a nil catalog registered routes")
	}
}
