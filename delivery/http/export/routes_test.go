package export

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func TestAccountExportsAreGuardedByTheirAccountsResource(t *testing.T) {
	router := mux.NewRouter()
	guards := map[string]string{}
	RegisterRoutes(router, &ExportHandler{}, func(res workspace_domain.Resource, action workspace_domain.Action, h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			guards[r.URL.Path] = string(res) + ":" + string(action)
		}
	})
	for path, want := range map[string]string{
		"/mercadolivre/accounts/acct-1/entries/export": "mercadolivre_accounts:read",
		"/tiktok/accounts/acct-1/entries/export":       "tiktok_accounts:read",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		var match mux.RouteMatch
		if !router.Match(req, &match) {
			t.Fatalf("%s is not registered", path)
		}
		match.Handler.ServeHTTP(httptest.NewRecorder(), req)
		if guards[path] != want {
			t.Fatalf("%s guarded by %q, want %q", path, guards[path], want)
		}
	}
}
