package choicestatshttp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func TestEveryChoiceStatsRouteIsGuardedByItsResource(t *testing.T) {
	r := mux.NewRouter()
	guarded := map[string]string{}
	ac := func(res workspace_domain.Resource, act workspace_domain.Action, hf http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, req *http.Request) {
			guarded[req.Method+" "+req.URL.Path] = string(res) + ":" + string(act)
		}
	}
	RegisterRoutes(r, NewHandler(&fakeReader{}, allowAll{}), ac)
	cases := []struct{ method, path, want string }{
		{http.MethodGet, "/workflows/wf-1/choice-stats", string(workspace_domain.ResourceWorkflows) + ":" + string(workspace_domain.ActionRead)},
		{http.MethodGet, "/whatsapp-campaigns/c-1/choice-stats", string(workspace_domain.ResourceWhatsAppCampaigns) + ":" + string(workspace_domain.ActionRead)},
		{http.MethodGet, "/unofficial-whatsapp-campaigns/c-1/choice-stats", string(workspace_domain.ResourceUnofficialWhatsAppCampaigns) + ":" + string(workspace_domain.ActionRead)},
		{http.MethodGet, "/whatsapp-templates/t-1/choice-stats", string(workspace_domain.ResourceWhatsAppTemplates) + ":" + string(workspace_domain.ActionRead)},
	}
	for _, tc := range cases {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(tc.method, tc.path, nil))
		if got := guarded[tc.method+" "+tc.path]; got != tc.want {
			t.Fatalf("%s %s guarded by %q, want %q", tc.method, tc.path, got, tc.want)
		}
	}
}
