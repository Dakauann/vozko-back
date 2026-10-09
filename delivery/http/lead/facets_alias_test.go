package lead

import (
	"net/http"
	"testing"
)

func TestTheRetiredFacetsAliasIsGone(t *testing.T) {
	pages := &stubPages{}
	rec := send(t, listHandler(pages), http.MethodGet, "/leads/facets", nil, nil)
	if rec.Code != http.StatusNotFound || pages.asked.UserID != "" {
		t.Fatalf("GET /leads/facets = %d %s, asked %+v; want 404 without reaching the lead pages", rec.Code, rec.Body.String(), pages.asked)
	}
}
