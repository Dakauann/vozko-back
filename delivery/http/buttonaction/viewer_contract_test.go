package buttonactionhttp

import (
	"net/http"
	"strings"
	"testing"
)

func TestTheVerdictSaysWhichGatesNeedTheAcknowledgement(t *testing.T) {
	rec := serve(t, &fakeService{}, http.MethodGet, "/whatsapp/templates/tmpl-1/exit-verdict", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	for _, want := range []string{`"templateExitRequired":false`, `"campaignAcknowledgementRequired":true`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("body misses %s: %s", want, rec.Body)
		}
	}
}

func TestTheActionsListTheKindsThisViewerMaySet(t *testing.T) {
	rec := serve(t, &fakeService{}, http.MethodGet, "/whatsapp/templates/tmpl-1/button-actions", "", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"allowedKinds":["opt_out","classify"]`) {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
}
