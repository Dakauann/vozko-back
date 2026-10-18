package customerportfoliohttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	cp "vozko/domain/customerportfolio"
	"vozko/domain/shared"
	workspace_domain "vozko/domain/workspace"
	"vozko/infra/http/middleware"
	customerportfolio_usecase "vozko/usecases/customerportfolio"
)

const (
	someLead  = "3c2b1a00-7d1a-4c61-9a0e-2f8d4c1b2a10"
	someEntry = "9f8e7d6c-5b4a-4321-8fed-cba987654321"
	someConv  = "11111111-2222-4333-8444-555555555555"
)

var at = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

type spy struct {
	err     error
	actor   customerportfolio_usecase.Actor
	query   cp.ListQuery
	ref     shared.EntryRef
	leadID  string
	holder  string
	entryID string
	removed bool
}

func held() cp.Holding {
	entry := cp.Entry{ID: someEntry, WorkspaceID: "ws-1", HolderID: "user-1", Subject: cp.ContactSubject(shared.EntryTypeInstagram, someConv), AddedBy: "user-1", AddedAt: at, UpdatedAt: at}
	return cp.Holding{Subject: entry.Subject, Entry: &entry, HolderName: "Ana"}
}

func (s *spy) List(_ context.Context, a customerportfolio_usecase.Actor, q cp.ListQuery) (*shared.PaginatedResult[cp.Row], error) {
	s.actor, s.query = a, q
	h := held()
	row := cp.Row{Entry: *h.Entry, HolderName: "Ana", AddedByName: "Ana", HolderReceives: true,
		Display: cp.Display{Channel: shared.EntryTypeInstagram, Name: "Joana", Handle: "joana.ig"}}
	return &shared.PaginatedResult[cp.Row]{Items: []cp.Row{row}, Page: 1, PageSize: 20, TotalItems: 1, TotalPages: 1}, s.err
}

func (s *spy) ForConversation(_ context.Context, a customerportfolio_usecase.Actor, ref shared.EntryRef) (cp.Holding, error) {
	s.actor, s.ref = a, ref
	return held(), s.err
}

func (s *spy) ForLead(_ context.Context, a customerportfolio_usecase.Actor, leadID string) (cp.Holding, error) {
	s.actor, s.leadID = a, leadID
	return cp.Holding{Subject: cp.LeadSubject(leadID)}, s.err
}

func (s *spy) PlaceConversation(_ context.Context, a customerportfolio_usecase.Actor, ref shared.EntryRef, holderID string) (cp.Holding, error) {
	s.actor, s.ref, s.holder = a, ref, holderID
	return held(), s.err
}

func (s *spy) PlaceLead(_ context.Context, a customerportfolio_usecase.Actor, leadID, holderID string) (cp.Holding, error) {
	s.actor, s.leadID, s.holder = a, leadID, holderID
	return held(), s.err
}

func (s *spy) Move(_ context.Context, a customerportfolio_usecase.Actor, entryID, holderID string) (cp.Holding, error) {
	s.actor, s.entryID, s.holder = a, entryID, holderID
	return held(), s.err
}

func (s *spy) Remove(_ context.Context, a customerportfolio_usecase.Actor, entryID string) error {
	s.actor, s.entryID, s.removed = a, entryID, true
	return s.err
}

func serve(t *testing.T, service Service, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	router := mux.NewRouter()
	allow := func(_ workspace_domain.Resource, _ workspace_domain.Action, next http.HandlerFunc) http.HandlerFunc {
		return next
	}
	RegisterRoutes(router, NewHandler(service), allow)
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	ctx := context.WithValue(request.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: "user-1", Role: "user"})
	ctx = context.WithValue(ctx, middleware.WorkspaceIDContextKey, "ws-1")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request.WithContext(ctx))
	return recorder
}

func decode(t *testing.T, r *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %q: %v", r.Body.String(), err)
	}
	return body
}

func TestEveryRouteIsGatedByTheCarteiraPermission(t *testing.T) {
	gates := map[string]workspace_domain.Action{}
	router := mux.NewRouter()
	record := func(resource workspace_domain.Resource, action workspace_domain.Action, next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if resource != workspace_domain.ResourceCustomerPortfolios {
				t.Errorf("%s %s is gated by resource %s", r.Method, r.URL.Path, resource)
			}
			path, _ := mux.CurrentRoute(r).GetPathTemplate()
			gates[r.Method+" "+path] = action
		}
	}
	RegisterRoutes(router, NewHandler(&spy{}), record)
	calls := []struct{ method, path string }{
		{http.MethodGet, "/customer-portfolio/entries"},
		{http.MethodPut, "/customer-portfolio/entries/" + someEntry},
		{http.MethodDelete, "/customer-portfolio/entries/" + someEntry},
		{http.MethodGet, "/customer-portfolio/leads/" + someLead},
		{http.MethodPost, "/customer-portfolio/leads/" + someLead},
		{http.MethodGet, "/customer-portfolio/conversations/instagram/" + someConv},
		{http.MethodPost, "/customer-portfolio/conversations/instagram/" + someConv},
	}
	for _, c := range calls {
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(c.method, c.path, nil))
	}
	read, use, manage := workspace_domain.ActionRead, workspace_domain.ActionUse, workspace_domain.ActionManage
	want := map[string]workspace_domain.Action{
		"GET " + entriesPath:       read,
		"PUT " + entryPath:         manage,
		"DELETE " + entryPath:      use,
		"GET " + leadPath:          read,
		"POST " + leadPath:         use,
		"GET " + conversationPath:  read,
		"POST " + conversationPath: use,
	}
	for route, action := range want {
		if got, ok := gates[route]; !ok || got != action {
			t.Errorf("%s gated by %q, want %q", route, got, action)
		}
	}
}

func TestListDefaultsToTheCallersOwnCarteira(t *testing.T) {
	s := &spy{}
	r := serve(t, s, http.MethodGet, "/customer-portfolio/entries?search=jo&page=2&pageSize=10", "")

	if r.Code != http.StatusOK {
		t.Fatalf("status = %d %s", r.Code, r.Body.String())
	}
	if s.query != (cp.ListQuery{HolderID: "user-1", Search: "jo", Page: 2, PageSize: 10}) {
		t.Fatalf("query = %+v", s.query)
	}
	body := decode(t, r)
	row := body["data"].([]any)[0].(map[string]any)
	display := row["display"].(map[string]any)
	if row["holderName"] != "Ana" || row["holderReceives"] != true || display["handle"] != "joana.ig" || display["channel"] != "instagram" {
		t.Fatalf("row = %+v", row)
	}
	if body["meta"].(map[string]any)["totalItems"] != float64(1) {
		t.Fatalf("meta = %+v", body["meta"])
	}
}

func TestListAllAndASpecificHolder(t *testing.T) {
	s := &spy{}
	serve(t, s, http.MethodGet, "/customer-portfolio/entries?holderId=all", "")
	if s.query.HolderID != "" {
		t.Fatalf("all = %+v", s.query)
	}
	serve(t, s, http.MethodGet, "/customer-portfolio/entries?holderId="+someLead, "")
	if s.query.HolderID != someLead {
		t.Fatalf("holder = %+v", s.query)
	}
	if r := serve(t, s, http.MethodGet, "/customer-portfolio/entries?page=x", ""); r.Code != http.StatusBadRequest {
		t.Fatalf("bad page = %d", r.Code)
	}
}

func TestPlacingAConversationReadsTheChannelFromThePath(t *testing.T) {
	s := &spy{}
	r := serve(t, s, http.MethodPost, "/customer-portfolio/conversations/instagram/"+someConv, `{}`)

	if r.Code != http.StatusOK || s.ref != (shared.EntryRef{EntryID: someConv, EntryType: shared.EntryTypeInstagram}) || s.holder != "" {
		t.Fatalf("status = %d ref=%+v holder=%q", r.Code, s.ref, s.holder)
	}
	body := decode(t, r)
	entry := body["entry"].(map[string]any)
	subject := body["subject"].(map[string]any)
	if entry["holderName"] != "Ana" || subject["kind"] != "contact" || subject["contactType"] != "instagram" {
		t.Fatalf("body = %+v", body)
	}
	if r := serve(t, s, http.MethodPost, "/customer-portfolio/conversations/fax/"+someConv, `{}`); r.Code != http.StatusBadRequest {
		t.Fatalf("unknown channel = %d", r.Code)
	}
}

func TestAManagerNamesTheHolder(t *testing.T) {
	s := &spy{}
	serve(t, s, http.MethodPost, "/customer-portfolio/leads/"+someLead, `{"holderId":"user-2"}`)
	if s.leadID != someLead || s.holder != "user-2" {
		t.Fatalf("lead=%q holder=%q", s.leadID, s.holder)
	}
	if r := serve(t, s, http.MethodPost, "/customer-portfolio/leads/"+someLead, `{"owner":"x"}`); r.Code != http.StatusBadRequest {
		t.Fatalf("unknown field = %d", r.Code)
	}
}

func TestAFreeLeadAnswersAnEmptyEntry(t *testing.T) {
	r := serve(t, &spy{}, http.MethodGet, "/customer-portfolio/leads/"+someLead, "")

	body := decode(t, r)
	if r.Code != http.StatusOK || body["entry"] != nil || body["subject"].(map[string]any)["leadId"] != someLead {
		t.Fatalf("status = %d body = %+v", r.Code, body)
	}
}

func TestMoveNeedsAHolderAndRemoveAnswersNoContent(t *testing.T) {
	s := &spy{}
	if r := serve(t, s, http.MethodPut, "/customer-portfolio/entries/"+someEntry, `{}`); r.Code != http.StatusBadRequest {
		t.Fatalf("move without holder = %d", r.Code)
	}
	if r := serve(t, s, http.MethodPut, "/customer-portfolio/entries/"+someEntry, `{"holderId":"user-2"}`); r.Code != http.StatusOK || s.holder != "user-2" || s.entryID != someEntry {
		t.Fatalf("move = %d %+v", r.Code, s)
	}
	if r := serve(t, s, http.MethodDelete, "/customer-portfolio/entries/"+someEntry, ""); r.Code != http.StatusNoContent || !s.removed {
		t.Fatalf("remove = %d", r.Code)
	}
}

func TestRefusalsMapToStatusAndCode(t *testing.T) {
	heldByBob := &cp.HeldByOther{Entry: cp.Entry{ID: someEntry, HolderID: "user-2"}, HolderName: "Bob"}
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{cp.ErrForbidden, http.StatusForbidden, "forbidden"},
		{cp.ErrHolderOutOfReach, http.StatusForbidden, "customer_portfolio_holder_out_of_reach"},
		{cp.ErrEntryNotFound, http.StatusNotFound, "customer_portfolio_entry_not_found"},
		{cp.ErrSubjectNotFound, http.StatusNotFound, "customer_portfolio_subject_not_found"},
		{heldByBob, http.StatusConflict, "customer_portfolio_held_by_other"},
		{cp.ErrHolderIneligible, http.StatusConflict, "customer_portfolio_holder_ineligible"},
		{cp.ErrListQueryInvalid, http.StatusBadRequest, "customer_portfolio_list_invalid"},
		{errors.New("db down"), http.StatusInternalServerError, ""},
	}
	for _, c := range cases {
		r := serve(t, &spy{err: c.err}, http.MethodPost, "/customer-portfolio/leads/"+someLead, `{}`)
		body := decode(t, r)
		if r.Code != c.status || (c.code != "" && body["code"] != c.code) {
			t.Errorf("%v = %d %v, want %d %s", c.err, r.Code, body["code"], c.status, c.code)
		}
	}
	body := decode(t, serve(t, &spy{err: heldByBob}, http.MethodPost, "/customer-portfolio/leads/"+someLead, `{}`))
	expected := body["expected"].(map[string]any)
	if expected["entryId"] != someEntry || expected["holderId"] != "user-2" || expected["holderName"] != "Bob" {
		t.Fatalf("the refusal must name the holder so the screen can offer a move: %+v", expected)
	}
}

func TestWithoutAServiceTheRoutesAnswerUnavailable(t *testing.T) {
	if r := serve(t, nil, http.MethodGet, "/customer-portfolio/entries", ""); r.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", r.Code)
	}
}
