package leadmemory

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	leadmemory "vozko/domain/lead_memory"
	workspace_domain "vozko/domain/workspace"
	"vozko/infra/http/middleware"
)

// The delivery layer's job is translation: a frame in, a status and a code
// out. These pin the permission each route is gated on, where the actor comes
// from (claims, never the body), and which status each refusal carries.

type stubCreate struct {
	result *leadmemory.CreateResult
	err    error
	last   leadmemory.CreateInput
}

func (s *stubCreate) Execute(_ context.Context, in leadmemory.CreateInput) (*leadmemory.CreateResult, error) {
	s.last = in
	return s.result, s.err
}

type stubUpdate struct {
	result *leadmemory.LeadMemory
	err    error
	last   leadmemory.UpdateInput
}

func (s *stubUpdate) Execute(_ context.Context, in leadmemory.UpdateInput) (*leadmemory.LeadMemory, error) {
	s.last = in
	return s.result, s.err
}

type stubDelete struct {
	err  error
	last leadmemory.DeleteInput
}

func (s *stubDelete) Execute(_ context.Context, in leadmemory.DeleteInput) error {
	s.last = in
	return s.err
}

type stubList struct {
	result *leadmemory.ListResult
	err    error
	last   leadmemory.ListInput
}

func (s *stubList) Execute(_ context.Context, in leadmemory.ListInput) (*leadmemory.ListResult, error) {
	s.last = in
	return s.result, s.err
}

type gateCall struct {
	resource workspace_domain.Resource
	action   workspace_domain.Action
}

type handlerFixture struct {
	create *stubCreate
	update *stubUpdate
	delete *stubDelete
	list   *stubList
	router *mux.Router
	gates  []gateCall
}

// stubResolver stands in for the contact→lead bridge. An empty resolved value
// is the real state of an Instagram or Telegram conversation: a contact that
// keys no lead.
type stubResolver struct{ resolved string }

func (s stubResolver) ResolveLeadRef(_, _ string) string { return s.resolved }

func newFixture() *handlerFixture { return newFixtureWith(nil) }

func newFixtureWith(resolver LeadRefResolver) *handlerFixture {
	f := &handlerFixture{
		create: &stubCreate{result: &leadmemory.CreateResult{Memory: memory()}},
		update: &stubUpdate{result: memory()},
		delete: &stubDelete{},
		list:   &stubList{result: &leadmemory.ListResult{Items: []leadmemory.MemoryView{{LeadMemory: memory(), ActorLabel: "Agente Vendas"}}, Total: 1}},
	}
	h := NewLeadMemoryHandler(f.create, f.update, f.delete, f.list, resolver)
	f.router = mux.NewRouter()
	// The recording gate stands in for the workspace RBAC middleware: it lets
	// everything through while pinning WHICH permission each route asked for.
	ac := func(res workspace_domain.Resource, act workspace_domain.Action, next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			f.gates = append(f.gates, gateCall{resource: res, action: act})
			next(w, r)
		}
	}
	RegisterRoutes(f.router, h, ac)
	return f
}

func (f *handlerFixture) do(method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	// GetWorkspaceID falls back to this header, so no workspace middleware is
	// needed to exercise the handler.
	req.Header.Set("X-Workspace-ID", "ws-1")
	req = req.WithContext(context.WithValue(req.Context(),
		middleware.ClaimsContextKey, &auth.Claims{UserID: "user-1", Role: "member"}))

	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

func memory() *leadmemory.LeadMemory {
	return &leadmemory.LeadMemory{
		ID:          "11111111-2222-4333-8444-555555555555",
		WorkspaceID: "ws-1",
		LeadID:      "lead-1",
		Category:    leadmemory.CategoryPreference,
		Content:     "Prefere boleto.",
	}
}

func TestRoutesRideTheLeadsPermissions(t *testing.T) {
	f := newFixture()

	f.do(http.MethodGet, "/leads/lead-1/memories", "")
	f.do(http.MethodPost, "/leads/lead-1/memories", `{"content":"x","category":"other"}`)
	f.do(http.MethodPatch, "/lead-memories/11111111-2222-4333-8444-555555555555", `{"content":"y"}`)
	f.do(http.MethodDelete, "/lead-memories/11111111-2222-4333-8444-555555555555", "")

	want := []gateCall{
		{workspace_domain.ResourceLeads, workspace_domain.ActionRead},
		{workspace_domain.ResourceLeads, workspace_domain.ActionUpdate},
		{workspace_domain.ResourceLeads, workspace_domain.ActionUpdate},
		{workspace_domain.ResourceLeads, workspace_domain.ActionUpdate},
	}
	if len(f.gates) != len(want) {
		t.Fatalf("gates = %+v", f.gates)
	}
	for i, g := range f.gates {
		if g != want[i] {
			t.Fatalf("route %d gated on %+v, want %+v", i, g, want[i])
		}
	}
}

func TestCreateTakesActorFromClaimsAndLeadFromPath(t *testing.T) {
	f := newFixture()

	rec := f.do(http.MethodPost, "/leads/lead-1/memories", `{"content":"Prefere boleto.","category":"preference"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	in := f.create.last
	if in.WorkspaceID != "ws-1" || in.LeadID != "lead-1" {
		t.Fatalf("scope = %+v", in)
	}
	// The actor is the authenticated operator: the body cannot spoof it.
	if string(in.Actor.Kind) != "human" || in.Actor.ID != "user-1" {
		t.Fatalf("actor = %+v", in.Actor)
	}
}

func TestCreateAnswers200WhenDeduplicated(t *testing.T) {
	f := newFixture()
	f.create.result = &leadmemory.CreateResult{Memory: memory(), Deduplicated: true}

	rec := f.do(http.MethodPost, "/leads/lead-1/memories", `{"content":"Prefere boleto.","category":"preference"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("dedup create = %d, want 200", rec.Code)
	}
}

func TestListReturnsActorLabels(t *testing.T) {
	f := newFixture()
	rec := f.do(http.MethodGet, "/leads/lead-1/memories?category=preference", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body struct {
		Memories []LeadMemoryResponse `json:"memories"`
		Total    int64                `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Total != 1 || len(body.Memories) != 1 || body.Memories[0].ActorLabel != "Agente Vendas" {
		t.Fatalf("body = %+v", body)
	}
	if f.list.last.Query.Category == nil || *f.list.last.Query.Category != leadmemory.CategoryPreference {
		t.Fatalf("category filter not forwarded: %+v", f.list.last.Query)
	}
}

func TestListRejectsUnknownCategory(t *testing.T) {
	f := newFixture()
	if rec := f.do(http.MethodGet, "/leads/lead-1/memories?category=vibes", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown category = %d, want 400", rec.Code)
	}
}

func TestDeleteAnswers204(t *testing.T) {
	f := newFixture()
	rec := f.do(http.MethodDelete, "/lead-memories/11111111-2222-4333-8444-555555555555", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d, want 204", rec.Code)
	}
	if f.delete.last.MemoryRef != "11111111-2222-4333-8444-555555555555" {
		t.Fatalf("ref = %q", f.delete.last.MemoryRef)
	}
}

func TestDomainErrorsCarryTheirStatuses(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
	}{
		{"not found", leadmemory.ErrNotFound, http.StatusNotFound},
		{"duplicate", leadmemory.ErrDuplicate, http.StatusConflict},
		{"limit", leadmemory.ErrLimitReached, http.StatusUnprocessableEntity},
		{"ambiguous", leadmemory.ErrAmbiguousID, http.StatusBadRequest},
		{"too long", leadmemory.ErrContentTooLong, http.StatusBadRequest},
		{"unexpected", errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture()
			f.update.err = tc.err
			f.update.result = nil
			rec := f.do(http.MethodPatch, "/lead-memories/11111111-2222-4333-8444-555555555555", `{"content":"x"}`)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (body=%s)", rec.Code, tc.status, rec.Body.String())
			}
		})
	}
}

// A conversation with no lead behind it is an answerable state, not a failure.
// Before this, the unresolved ref passed through as if it were a lead: the list
// came back empty (indistinguishable from "no memories yet"), the operator
// typed a memory, and the write died on the lead foreign key as a 500.

func TestListWithoutLinkedLeadIsEmptyAndSaysSo(t *testing.T) {
	f := newFixtureWith(stubResolver{resolved: ""})

	rec := f.do(http.MethodGet, "/leads/contact-1/memories", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}

	var body LeadMemoryListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.LeadLinked {
		t.Fatal("leadLinked = true for a ref that resolves to no lead")
	}
	if len(body.Memories) != 0 || body.Total != 0 {
		t.Fatalf("body = %+v, want an empty list", body)
	}
	// The list is answered from the resolution alone: querying memories for an
	// id that keys nothing is work with a known result.
	if f.list.last.LeadID != "" {
		t.Fatalf("list usecase was called with %q, want no call", f.list.last.LeadID)
	}
}

func TestCreateWithoutLinkedLeadIsRefusedNotAttempted(t *testing.T) {
	f := newFixtureWith(stubResolver{resolved: ""})

	rec := f.do(http.MethodPost, "/leads/contact-1/memories", `{"content":"Prefere boleto.","category":"preference"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "lead_not_linked") {
		t.Fatalf("body = %s, want the lead_not_linked code", rec.Body.String())
	}
	// Refused before storage: the old path reached the lead foreign key and
	// surfaced as an opaque 500.
	if f.create.last.LeadID != "" {
		t.Fatalf("create usecase was called with %q, want no call", f.create.last.LeadID)
	}
}

func TestResolvedContactRefBecomesTheLeadID(t *testing.T) {
	f := newFixtureWith(stubResolver{resolved: "lead-9"})

	rec := f.do(http.MethodPost, "/leads/contact-1/memories", `{"content":"Prefere boleto.","category":"preference"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body=%s)", rec.Code, rec.Body.String())
	}
	if f.create.last.LeadID != "lead-9" {
		t.Fatalf("create lead = %q, want the resolved lead", f.create.last.LeadID)
	}

	rec = f.do(http.MethodGet, "/leads/contact-1/memories", "")
	var body LeadMemoryListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.LeadLinked || f.list.last.LeadID != "lead-9" {
		t.Fatalf("list lead = %q linked = %v", f.list.last.LeadID, body.LeadLinked)
	}
}

// A nil resolver means "not configured", not "no lead": plain lead ids must
// keep working untouched.
func TestNilResolverKeepsPlainLeadIDsLinked(t *testing.T) {
	f := newFixture()

	rec := f.do(http.MethodGet, "/leads/lead-1/memories", "")
	var body LeadMemoryListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.LeadLinked || f.list.last.LeadID != "lead-1" {
		t.Fatalf("list lead = %q linked = %v", f.list.last.LeadID, body.LeadLinked)
	}
	if rec := f.do(http.MethodPost, "/leads/lead-1/memories", `{"content":"x","category":"other"}`); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, want 201", rec.Code)
	}
}
