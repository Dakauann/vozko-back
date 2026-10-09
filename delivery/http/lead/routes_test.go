package lead

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/address"
	leaddomain "vozko/domain/lead"
	"vozko/domain/shared"
	workspace_domain "vozko/domain/workspace"
	lead_usecase "vozko/usecases/lead"
)

type gatedRoute struct {
	resource workspace_domain.Resource
	action   workspace_domain.Action
}

func routeGates(t *testing.T) map[string]gatedRoute {
	t.Helper()
	gates := map[string]gatedRoute{}
	h := NewLeadHandler(HandlerDeps{Commands: &stubCommands{}, History: &stubHistory{}})
	router := mux.NewRouter()
	record := func(resource workspace_domain.Resource, action workspace_domain.Action, next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			route := mux.CurrentRoute(r)
			path, _ := route.GetPathTemplate()
			gates[r.Method+" "+path] = gatedRoute{resource, action}
		}
	}
	RegisterRoutes(router, h, record)
	RegisterEntryConversationRoutes(router, h, record)
	for _, call := range []struct{ method, path string }{
		{http.MethodPost, "/leads/" + routeLeadID + "/anonymize"},
		{http.MethodGet, "/leads/" + routeLeadID + "/relatives"},
		{http.MethodPost, "/leads/" + routeLeadID + "/district"},
		{http.MethodGet, "/entries/" + relativeLeadID + "/lead"},
	} {
		send(t, router, call.method, call.path, nil, nil)
	}
	return gates
}

func TestTheNewLeadRoutesAreGatedByTheirOwnPermission(t *testing.T) {
	gates := routeGates(t)
	want := map[string]gatedRoute{
		"POST /leads" + leadIDPath + "/anonymize": {workspace_domain.ResourceLeads, workspace_domain.ActionAnonymize},
		"GET /leads" + leadIDPath + "/relatives":  {workspace_domain.ResourceLeads, workspace_domain.ActionRead},
		"POST /leads" + leadIDPath + "/district":  {workspace_domain.ResourceLeads, workspace_domain.ActionUpdate},
		"GET /entries/{entryId}/lead":             {workspace_domain.ResourceConversations, workspace_domain.ActionRead},
	}
	for route, gate := range want {
		if got, ok := gates[route]; !ok || got != gate {
			t.Errorf("%s is gated by %+v, want %+v", route, got, gate)
		}
	}
}

type stubPages struct {
	err   error
	asked lead_usecase.Actor
}

func (p *stubPages) List(_ context.Context, a lead_usecase.Actor, _ leaddomain.ListLeadsInput) (*shared.PaginatedResult[*leaddomain.LeadWithSummary], error) {
	p.asked = a
	if p.err != nil {
		return nil, p.err
	}
	row := &leaddomain.LeadWithSummary{
		Lead: &leaddomain.Lead{ID: routeLeadID, Name: "Ana", Owner: "u-1", RelativesCount: 2, ReferredCount: 7,
			CustomFields: map[string]any{"interesse": "alto"},
			Phones:       []leaddomain.ContactPhone{{ID: "p-1", Number: "551133334444", Label: leaddomain.PhoneLandline}},
			Addresses: []leaddomain.Address{
				{ID: "a-2", Label: leaddomain.AddressWork, Postal: address.Postal{District: "Centro", City: "Belo Horizonte", State: "MG"}},
				{ID: "a-1", Label: leaddomain.AddressHome, Primary: true, Postal: address.Postal{District: "Bela Vista", City: "São Paulo", State: "SP"}},
			}},
		Summary: &leaddomain.LeadSummary{},
	}
	return &shared.PaginatedResult[*leaddomain.LeadWithSummary]{Items: []*leaddomain.LeadWithSummary{row}, TotalItems: 1, Page: 1, PageSize: 20, TotalPages: 1}, nil
}

func listHandler(pages Pages) http.Handler {
	h := NewLeadHandler(HandlerDeps{Commands: &stubCommands{}, History: &stubHistory{}, Pages: pages})
	router := mux.NewRouter()
	RegisterRoutes(router, h, passThrough)
	return router
}

func TestTheLeadListCarriesTheNewColumns(t *testing.T) {
	pages := &stubPages{}
	rec := send(t, listHandler(pages), http.MethodGet, "/leads", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if pages.asked.UserID != "user-1" || pages.asked.WorkspaceID != "ws-1" {
		t.Fatalf("the list is read as the caller, got %+v", pages.asked)
	}
	var out struct {
		Data []LeadListResponseItem `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out.Data) != 1 {
		t.Fatalf("body = %s", rec.Body.String())
	}
	item := out.Data[0]
	if item.Owner != "u-1" || item.RelativesCount != 2 || item.ReferredCount != 7 || len(item.Phones) != 1 || item.CustomFields["interesse"] != "alto" {
		t.Fatalf("item = %+v", item)
	}
	if item.PrimaryAddress == nil || item.PrimaryAddress.ID != "a-1" || item.PrimaryAddress.District != "Bela Vista" {
		t.Fatalf("primary address = %+v", item.PrimaryAddress)
	}
}

func TestTheLeadListRefusesWithoutItsUseCaseOrPermission(t *testing.T) {
	if rec := send(t, listHandler(nil), http.MethodGet, "/leads", nil, nil); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("without the pages use case = %d", rec.Code)
	}
	if rec := send(t, listHandler(&stubPages{err: leaddomain.ErrLeadForbidden}), http.MethodGet, "/leads", nil, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("without leads:read = %d", rec.Code)
	}
}
