package lead

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/mux"

	leaddomain "vozko/domain/lead"
	"vozko/domain/leadcompany"
	"vozko/domain/shared"
	workspace_domain "vozko/domain/workspace"
	lead_usecase "vozko/usecases/lead"
)

const routeCompanyID = "3c2b1a00-7d1a-4c61-9a0e-2f8d4c1b2a10"

func (s *stubCommands) SetCompany(_ context.Context, _ lead_usecase.Actor, _ string, companyID string) (*leaddomain.Lead, error) {
	s.company = companyID
	return s.answer("company", nil)
}

type stubCompanies struct {
	err     error
	company leadcompany.Company
	draft   leadcompany.Draft
	patch   leadcompany.Patch
	query   leadcompany.ListQuery
	calls   []string
}

func (s *stubCompanies) answer(call string) (leadcompany.Company, error) {
	s.calls = append(s.calls, call)
	return s.company, s.err
}

func (s *stubCompanies) Create(_ context.Context, _ lead_usecase.Actor, d leadcompany.Draft) (leadcompany.Company, error) {
	s.draft = d
	return s.answer("create")
}

func (s *stubCompanies) Update(_ context.Context, _ lead_usecase.Actor, _ string, p leadcompany.Patch) (leadcompany.Company, error) {
	s.patch = p
	return s.answer("update")
}

func (s *stubCompanies) Get(context.Context, lead_usecase.Actor, string) (leadcompany.Company, error) {
	return s.answer("get")
}

func (s *stubCompanies) List(_ context.Context, _ lead_usecase.Actor, q leadcompany.ListQuery) (*shared.PaginatedResult[leadcompany.Company], error) {
	s.query = q
	if _, err := s.answer("list"); err != nil {
		return nil, err
	}
	return shared.NewPaginatedResult([]leadcompany.Company{s.company}, shared.Pagination{Page: 2, PageSize: 10}, 11), nil
}

func (s *stubCompanies) Delete(context.Context, lead_usecase.Actor, string) error {
	_, err := s.answer("delete")
	return err
}

func companyRouter(companies *stubCompanies, record func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc) http.Handler {
	router := mux.NewRouter()
	RegisterCompanyRoutes(router, NewCompanyHandler(companies), record)
	return router
}

func storedCompany() leadcompany.Company {
	at := time.Date(2026, time.October, 17, 12, 0, 0, 0, time.UTC)
	return leadcompany.Company{ID: routeCompanyID, WorkspaceID: "ws-1", Name: "Acme", AcceptsLeads: true, MembersCount: 3,
		ImageMediaID: "m-1", Image: &leadcompany.Image{MediaID: "m-1", URL: "https://cdn/logo.png", PreviewURL: "https://cdn/logo-preview.jpg"},
		CreatedAt: at, UpdatedAt: at}
}

func TestSetCompanyJoinsOrLeavesThroughOneCommand(t *testing.T) {
	cmds := &stubCommands{result: &leaddomain.Lead{ID: routeLeadID, Version: 5, CompanyID: routeCompanyID}}
	rec := send(t, routedHandler(cmds, &stubHistory{}), http.MethodPost, "/leads/"+routeLeadID+"/company", nil, map[string]any{"companyId": routeCompanyID})
	var out LeadRecordResponse
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil || out.CompanyID != routeCompanyID || cmds.company != routeCompanyID {
		t.Fatalf("status = %d body = %s company = %q", rec.Code, rec.Body.String(), cmds.company)
	}
	leaving := &stubCommands{result: &leaddomain.Lead{ID: routeLeadID, Version: 6}}
	if rec := send(t, routedHandler(leaving, &stubHistory{}), http.MethodPost, "/leads/"+routeLeadID+"/company", nil, map[string]any{"companyId": nil}); rec.Code != http.StatusOK || leaving.company != "" || len(leaving.calls) != 1 {
		t.Fatalf("leave = %d, company = %q", rec.Code, leaving.company)
	}
}

func TestSetCompanyNeedsTheCompanyKey(t *testing.T) {
	for _, body := range []map[string]any{{}, {"companyId": 3}, {"companyId": routeCompanyID, "other": true}} {
		refused := &stubCommands{}
		if rec := send(t, routedHandler(refused, &stubHistory{}), http.MethodPost, "/leads/"+routeLeadID+"/company", nil, body); rec.Code != http.StatusBadRequest || len(refused.calls) != 0 {
			t.Fatalf("body %v = %d", body, rec.Code)
		}
	}
}

func TestSetCompanyRefusalsCarryTheirCodes(t *testing.T) {
	cases := map[error]struct {
		status int
		code   string
	}{
		leadcompany.ErrClosed:       {http.StatusConflict, "lead_company_closed"},
		leadcompany.ErrNotFound:     {http.StatusNotFound, "lead_company_not_found"},
		leaddomain.ErrLeadForbidden: {http.StatusForbidden, "forbidden"},
	}
	for err, want := range cases {
		cmds := &stubCommands{err: err}
		rec := send(t, routedHandler(cmds, &stubHistory{}), http.MethodPost, "/leads/"+routeLeadID+"/company", nil, map[string]any{"companyId": routeCompanyID})
		if rec.Code != want.status || !bytes.Contains(rec.Body.Bytes(), []byte(want.code)) {
			t.Errorf("%v = %d %s, want %d %s", err, rec.Code, rec.Body.String(), want.status, want.code)
		}
	}
}

func TestCreateCompanyAcceptsLeadsUnlessToldOtherwise(t *testing.T) {
	companies := &stubCompanies{company: storedCompany()}
	rec := send(t, companyRouter(companies, passThrough), http.MethodPost, "/lead-companies", nil, map[string]any{"name": "Acme", "imageMediaId": "m-1"})
	if rec.Code != http.StatusCreated || companies.draft != (leadcompany.Draft{Name: "Acme", AcceptsLeads: true, ImageMediaID: "m-1"}) {
		t.Fatalf("status = %d draft = %+v", rec.Code, companies.draft)
	}
	var out LeadCompanyResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Image == nil || out.Image.PreviewURL != "https://cdn/logo-preview.jpg" || out.MembersCount != 3 {
		t.Fatalf("body = %s", rec.Body.String())
	}
	closed := &stubCompanies{company: storedCompany()}
	send(t, companyRouter(closed, passThrough), http.MethodPost, "/lead-companies", nil, map[string]any{"name": "Acme", "acceptsLeads": false})
	if closed.draft.AcceptsLeads {
		t.Fatal("acceptsLeads false must create a closed company")
	}
}

func TestUpdateCompanyPassesOnlyWhatWasSent(t *testing.T) {
	companies := &stubCompanies{company: storedCompany()}
	rec := send(t, companyRouter(companies, passThrough), http.MethodPatch, "/lead-companies/"+routeCompanyID, nil, map[string]any{"acceptsLeads": false, "imageMediaId": ""})
	if rec.Code != http.StatusOK || companies.patch.Name != nil || companies.patch.AcceptsLeads == nil || *companies.patch.AcceptsLeads ||
		companies.patch.ImageMediaID == nil || *companies.patch.ImageMediaID != "" {
		t.Fatalf("status = %d patch = %+v", rec.Code, companies.patch)
	}
}

func TestListCompaniesReadsTheSearchAndThePage(t *testing.T) {
	companies := &stubCompanies{company: storedCompany()}
	rec := send(t, companyRouter(companies, passThrough), http.MethodGet, "/lead-companies?search=ac&page=2&pageSize=10", nil, nil)
	var out LeadCompanyListResponse
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil || len(out.Data) != 1 || out.Meta.TotalItems != 11 || out.Meta.TotalPages != 2 {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if companies.query != (leadcompany.ListQuery{Search: "ac", Page: 2, PageSize: 10}) {
		t.Fatalf("query = %+v", companies.query)
	}
	if rec := send(t, companyRouter(&stubCompanies{}, passThrough), http.MethodGet, "/lead-companies?page=two", nil, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("a page that is not a number = %d", rec.Code)
	}
}

func TestCompanyRefusalsMapToTheirStatus(t *testing.T) {
	cases := map[error]int{
		leadcompany.ErrForbidden:    http.StatusForbidden,
		leadcompany.ErrNotFound:     http.StatusNotFound,
		leadcompany.ErrNameRequired: http.StatusBadRequest,
		leadcompany.ErrImageInvalid: http.StatusBadRequest,
	}
	for err, status := range cases {
		rec := send(t, companyRouter(&stubCompanies{err: err}, passThrough), http.MethodDelete, "/lead-companies/"+routeCompanyID, nil, nil)
		if rec.Code != status || !bytes.Contains(rec.Body.Bytes(), []byte(leadcompany.ErrorCode(err))) {
			t.Errorf("%v = %d %s, want %d", err, rec.Code, rec.Body.String(), status)
		}
	}
	if rec := send(t, companyRouter(&stubCompanies{}, passThrough), http.MethodDelete, "/lead-companies/"+routeCompanyID, nil, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d", rec.Code)
	}
}

func TestEveryCompanyRouteIsGatedByItsOwnPermission(t *testing.T) {
	gates := map[string]gatedRoute{}
	record := func(resource workspace_domain.Resource, action workspace_domain.Action, next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			path, _ := mux.CurrentRoute(r).GetPathTemplate()
			gates[r.Method+" "+path] = gatedRoute{resource, action}
		}
	}
	router := companyRouter(&stubCompanies{}, record)
	for _, call := range []struct{ method, path string }{
		{http.MethodGet, "/lead-companies"}, {http.MethodPost, "/lead-companies"},
		{http.MethodGet, "/lead-companies/" + routeCompanyID}, {http.MethodPatch, "/lead-companies/" + routeCompanyID},
		{http.MethodDelete, "/lead-companies/" + routeCompanyID},
	} {
		send(t, router, call.method, call.path, nil, nil)
	}
	companies := workspace_domain.ResourceLeadCompanies
	want := map[string]gatedRoute{
		"GET /lead-companies":                    {companies, workspace_domain.ActionRead},
		"POST /lead-companies":                   {companies, workspace_domain.ActionCreate},
		"GET /lead-companies" + companyIDPath:    {companies, workspace_domain.ActionRead},
		"PATCH /lead-companies" + companyIDPath:  {companies, workspace_domain.ActionUpdate},
		"DELETE /lead-companies" + companyIDPath: {companies, workspace_domain.ActionDelete},
	}
	for route, gate := range want {
		if got, ok := gates[route]; !ok || got != gate {
			t.Errorf("%s is gated by %+v, want %+v", route, got, gate)
		}
	}
	if gate := routeGates(t)["POST /leads"+leadIDPath+"/company"]; gate != (gatedRoute{workspace_domain.ResourceLeads, workspace_domain.ActionUpdate}) {
		t.Errorf("the company of a lead is gated by %+v, want leads:update", gate)
	}
}
