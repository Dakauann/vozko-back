package audience

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	ca "vozko/domain/audience"
	"vozko/domain/shared"
	workspace_domain "vozko/domain/workspace"
	"vozko/infra/http/middleware"
)

type recordingAC struct{ calls map[string]string }

func (r *recordingAC) fn(resource workspace_domain.Resource, action workspace_domain.Action, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		r.calls[req.Method+" "+req.URL.Path] = string(resource) + ":" + string(action)
		h(w, req)
	}
}

// Every route carries the audience resource with the right action:
// reads are read, anything that changes state or spends money is update.
func TestRegisterProtectedRoutes_AppliesRBAC(t *testing.T) {
	router := mux.NewRouter()
	ac := &recordingAC{calls: map[string]string{}}
	RegisterProtectedRoutes(router, &Handler{}, ac.fn)

	cases := []struct{ method, path, want string }{
		{http.MethodGet, "/audience", "audience:read"},
		{http.MethodGet, "/audience/stats", "audience:read"},
		{http.MethodGet, "/audience/trends", "audience:read"},
		{http.MethodGet, "/audience/spend", "audience:read"},
		{http.MethodGet, "/audience/authors", "audience:read"},
		{http.MethodGet, "/audience/authors/a-1", "audience:read"},
		{http.MethodGet, "/audience/authors/a-1/containers", "audience:read"},
		{http.MethodPatch, "/audience/authors/a-1", "audience:update"},
		{http.MethodGet, "/audience/settings/instagram/acc-1", "audience:read"},
		{http.MethodPatch, "/audience/settings/instagram/acc-1", "audience:update"},
		{http.MethodGet, "/audience/settings", "audience:read"},
		{http.MethodGet, "/audience/settings/instagram/acc-1/containers/m-1", "audience:read"},
		{http.MethodPut, "/audience/settings/instagram/acc-1/containers/m-1", "audience:update"},
		{http.MethodDelete, "/audience/settings/instagram/acc-1/containers/m-1", "audience:update"},
		{http.MethodGet, "/audience/backfill/instagram/acc-1/estimate", "audience:read"},
		{http.MethodPost, "/audience/backfill/instagram/acc-1", "audience:update"},
		{http.MethodGet, "/audience/backfill/b-1", "audience:read"},
		{http.MethodPost, "/audience/backfill/b-1/cancel", "audience:update"},
		{http.MethodPost, "/audience/row-1/retry", "audience:update"},
	}
	for _, c := range cases {
		var match mux.RouteMatch
		req := httptest.NewRequest(c.method, c.path, nil)
		if !router.Match(req, &match) {
			t.Errorf("%s %s: no route", c.method, c.path)
			continue
		}
		// Invoke through the recorded AC; the zero handler may panic on a nil
		// use case, which is fine here: registration is what is asserted.
		func() {
			defer func() { _ = recover() }()
			match.Handler.ServeHTTP(httptest.NewRecorder(), req)
		}()
		if got := ac.calls[c.method+" "+c.path]; got != c.want {
			t.Errorf("%s %s: rbac = %q, want %q", c.method, c.path, got, c.want)
		}
	}
}

func TestRegisterProtectedRoutes_NilHandlerRegistersNothing(t *testing.T) {
	router := mux.NewRouter()
	RegisterProtectedRoutes(router, nil, (&recordingAC{calls: map[string]string{}}).fn)
	var match mux.RouteMatch
	if router.Match(httptest.NewRequest(http.MethodGet, "/audience", nil), &match) {
		t.Fatal("an unwired feature must expose no routes")
	}
}

// ---- scoping ----

type captureList struct{ in ca.ListInput }

func (c *captureList) Execute(_ context.Context, in ca.ListInput) (*shared.PaginatedResult[*ca.Analysis], error) {
	c.in = in
	return shared.NewPaginatedResult([]*ca.Analysis{}, in.Options.Pagination, 0), nil
}

// The workspace comes from the session, never from the query. A caller who
// sends ?workspaceId=other still lists their own rows.
func TestList_WorkspaceCannotBeOverriddenByQuery(t *testing.T) {
	uc := &captureList{}
	h := NewHandler(Deps{List: uc})
	req := httptest.NewRequest(http.MethodGet, "/audience?workspaceId=other-ws&accountId=acc-1&stance=hostile&severityMin=60&requiresAction=true&status=analyzed,failed&page=2&pageSize=10&sort=severity:desc&from=2026-09-01", nil)
	req = withWorkspace(req, "session-ws")

	rec := httptest.NewRecorder()
	h.List(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	in := uc.in
	if in.WorkspaceID != "session-ws" {
		t.Fatalf("workspace = %q, must be the session's", in.WorkspaceID)
	}
	if in.AccountID != "acc-1" || in.Stance != ca.StanceHostile || in.SeverityMin == nil || *in.SeverityMin != 60 ||
		in.RequiresAction == nil || !*in.RequiresAction || len(in.Statuses) != 2 || in.From == nil ||
		in.Options.Pagination.Page != 2 || in.Options.Pagination.PageSize != 10 || len(in.Options.Sorts) != 1 {
		t.Fatalf("filters not parsed: %+v", in)
	}
	var body struct {
		Data []any `json:"data"`
		Meta struct{ Page int }
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data == nil {
		t.Fatal("an empty page must serialise as [] not null")
	}
}

type captureStats struct{ in ca.ListInput }

func (c *captureStats) Execute(_ context.Context, in ca.ListInput) (*ca.Stats, error) {
	c.in = in
	s := &ca.Stats{Counters: ca.Counters{Analyzed: 30, StanceSupporter: 30}}
	s.Finalize()
	return s, nil
}

func TestStats_ScopedAndSerialised(t *testing.T) {
	uc := &captureStats{}
	h := NewHandler(Deps{Stats: uc})
	req := withWorkspace(httptest.NewRequest(http.MethodGet, "/audience/stats?workspaceId=x", nil), "ws-1")
	rec := httptest.NewRecorder()
	h.Stats(rec, req)
	if uc.in.WorkspaceID != "ws-1" {
		t.Fatalf("workspace = %q", uc.in.WorkspaceID)
	}
	if !strings.Contains(rec.Body.String(), `"acceptanceScore":100`) || !strings.Contains(rec.Body.String(), `"topics":[]`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

type captureModeration struct{ in ca.SetModerationStateInput }

func (c *captureModeration) Execute(_ context.Context, in ca.SetModerationStateInput) (*ca.AuthorStats, error) {
	c.in = in
	if !in.State.Valid() {
		return nil, ca.ErrInvalidFilter
	}
	return &ca.AuthorStats{ID: in.AuthorID, ModerationState: in.State, TopTopics: []ca.TopicCount{}}, nil
}

func TestSetModeration_BadStateIs400(t *testing.T) {
	uc := &captureModeration{}
	h := NewHandler(Deps{Moderate: uc})
	router := mux.NewRouter()
	router.HandleFunc("/audience/authors/{id}", h.SetModeration).Methods(http.MethodPatch)

	req := withWorkspace(httptest.NewRequest(http.MethodPatch, "/audience/authors/a-1", strings.NewReader(`{"state":"banned"}`)), "ws-1")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	req = withWorkspace(httptest.NewRequest(http.MethodPatch, "/audience/authors/a-1", strings.NewReader(`{"state":"blocked"}`)), "ws-1")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || uc.in.WorkspaceID != "ws-1" || uc.in.AuthorID != "a-1" {
		t.Fatalf("status = %d in=%+v", rec.Code, uc.in)
	}
}

func TestErrorMapping(t *testing.T) {
	cases := map[error]int{
		ca.ErrNotFound:         http.StatusNotFound,
		ca.ErrInvalidFilter:    http.StatusBadRequest,
		ca.ErrStatusTransition: http.StatusConflict,
		ca.ErrTooManyTopics:    http.StatusBadRequest,
	}
	for err, want := range cases {
		rec := httptest.NewRecorder()
		writeDomainError(rec, err, "fallback")
		if rec.Code != want {
			t.Errorf("%v -> %d, want %d", err, rec.Code, want)
		}
	}
}

// withWorkspace stamps the session workspace the way the workspace
// middleware does, so GetWorkspaceID reads it from the context (which takes
// precedence over anything in the query).
func withWorkspace(r *http.Request, workspaceID string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), middleware.WorkspaceIDContextKey, workspaceID))
}

// ---- per-post settings ----

type capturePutContainer struct{ in ca.ContainerOverride }

func (c *capturePutContainer) Execute(_ context.Context, in ca.ContainerOverride) (*ca.ContainerSettings, error) {
	c.in = in
	eff := ca.NewSettings(in.WorkspaceID, in.Source, in.AccountID, ca.VerticalServices)
	return &ca.ContainerSettings{Override: &in, Effective: eff.WithOverride(&in)}, nil
}

// The path names the post, the session names the workspace, and the body
// only carries the override fields; the JSON `null` for a field means
// "inherit" and must arrive as a nil pointer, not a zero value.
func TestPutContainerSettings_BuildsOverrideFromPathSessionAndBody(t *testing.T) {
	uc := &capturePutContainer{}
	h := NewHandler(Deps{PutContainer: uc})
	router := mux.NewRouter()
	router.HandleFunc("/audience/settings/{source}/{accountId}/containers/{containerId}", h.PutContainerSettings).Methods(http.MethodPut)

	body := `{"enabled":true,"model":null,"severityThreshold":80,"instructions":"post sobre a obra","topics":[{"key":"obra","label":"Obra"}]}`
	req := withWorkspace(httptest.NewRequest(http.MethodPut, "/audience/settings/instagram/acc-1/containers/media-1?workspaceId=other", strings.NewReader(body)), "ws-1")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	in := uc.in
	if in.WorkspaceID != "ws-1" || in.Source != ca.SourceInstagram || in.AccountID != "acc-1" || in.ContainerID != "media-1" {
		t.Fatalf("ref not taken from path/session: %+v", in)
	}
	if in.Enabled == nil || !*in.Enabled || in.Model != nil || in.SeverityThreshold == nil || *in.SeverityThreshold != 80 ||
		in.Instructions == nil || *in.Instructions != "post sobre a obra" || in.Topics == nil || !in.Topics.Has("obra") {
		t.Fatalf("override fields not mapped: %+v", in)
	}
	if !strings.Contains(rec.Body.String(), `"override":{`) || !strings.Contains(rec.Body.String(), `"effective":{`) {
		t.Fatalf("response must carry both the override and the effective settings: %s", rec.Body.String())
	}
}

type captureAuthors struct{ in ca.AuthorsInput }

func (c *captureAuthors) Execute(_ context.Context, in ca.AuthorsInput) (*shared.PaginatedResult[*ca.AuthorStats], error) {
	c.in = in
	return shared.NewPaginatedResult([]*ca.AuthorStats{}, in.Options.Pagination, 0), nil
}

// The ranking is a query parameter, not a second endpoint. An absent ?sort=
// leaves the domain's own default in place; a known key is passed through with
// its direction; an unknown one is refused rather than quietly defaulted, so a
// client never reads a page ordered by something it did not ask for.
func TestListAuthors_SortParsing(t *testing.T) {
	t.Run("absent sort leaves the domain default", func(t *testing.T) {
		uc := &captureAuthors{}
		h := NewHandler(Deps{Authors: uc})
		rec := httptest.NewRecorder()
		h.ListAuthors(rec, withWorkspace(httptest.NewRequest(http.MethodGet, "/audience/authors", nil), "ws-1"))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
		}
		if uc.in.Sort != (ca.Sort{}) {
			t.Fatalf("handler must not invent a sort: %+v", uc.in.Sort)
		}
	})

	t.Run("known key and direction reach the use case", func(t *testing.T) {
		uc := &captureAuthors{}
		h := NewHandler(Deps{Authors: uc})
		rec := httptest.NewRecorder()
		h.ListAuthors(rec, withWorkspace(httptest.NewRequest(http.MethodGet, "/audience/authors?sort=severity&order=asc", nil), "ws-1"))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		if uc.in.Sort.Key != ca.SortAuthorSeverity || !uc.in.Sort.Ascending {
			t.Fatalf("sort = %+v", uc.in.Sort)
		}
	})

	t.Run("descending is the default direction", func(t *testing.T) {
		uc := &captureAuthors{}
		h := NewHandler(Deps{Authors: uc})
		rec := httptest.NewRecorder()
		h.ListAuthors(rec, withWorkspace(httptest.NewRequest(http.MethodGet, "/audience/authors?sort=comments", nil), "ws-1"))
		if uc.in.Sort.Key != ca.SortAuthorComments || uc.in.Sort.Ascending {
			t.Fatalf("sort = %+v", uc.in.Sort)
		}
	})

	t.Run("unknown key is 400, not a silent default", func(t *testing.T) {
		uc := &captureAuthors{}
		h := NewHandler(Deps{Authors: uc})
		rec := httptest.NewRecorder()
		h.ListAuthors(rec, withWorkspace(httptest.NewRequest(http.MethodGet, "/audience/authors?sort=karma", nil), "ws-1"))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
		if uc.in.WorkspaceID != "" {
			t.Fatal("a refused sort must not reach the use case")
		}
		// The 400 lists what IS accepted, straight from the domain, so the
		// message cannot drift from the parser.
		for _, k := range ca.AllAuthorSortKeys() {
			if !strings.Contains(rec.Body.String(), string(k)) {
				t.Fatalf("body %s omits accepted key %q", rec.Body.String(), k)
			}
		}
	})
}

// Resolving an @ to its author row is the navigation §2 hangs off: the feed
// knows a handle and an external id, the author endpoints take an author id.
func TestListAuthors_FiltersByExternalID(t *testing.T) {
	uc := &captureAuthors{}
	h := NewHandler(Deps{Authors: uc})
	rec := httptest.NewRecorder()
	h.ListAuthors(rec, withWorkspace(httptest.NewRequest(http.MethodGet, "/audience/authors?authorExternalId=%20ig-99%20", nil), "ws-1"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if uc.in.AuthorExternalID != "ig-99" {
		t.Fatalf("authorExternalId = %q, want it trimmed and passed through", uc.in.AuthorExternalID)
	}
}

// Forwarding a comment puts a message on the workspace's own WhatsApp, so it
// carries its own action rather than reusing the one that configures the
// engine: an operator who may moderate must not thereby be able to message
// people in the workspace's name.
func TestOutboundRoutesRequireTheSendAction(t *testing.T) {
	router := mux.NewRouter()
	ac := &recordingAC{calls: map[string]string{}}
	RegisterProtectedRoutes(router, &Handler{}, ac.fn)

	// Publishing a reply and forwarding a comment both speak in the
	// workspace's name, and the recipient picker enumerates who it talks to.
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/audience/row-1/escalate"},
		{http.MethodPost, "/audience/row-1/reply"},
		{http.MethodPost, "/audience/row-1/reply/suggest"},
		{http.MethodGet, "/audience/escalation-recipients"},
		// Arming an automated sender is granting sends, so the alert routes
		// carry the same action rather than the configuration one.
		{http.MethodGet, "/audience/alerts"},
		{http.MethodPost, "/audience/alerts"},
		{http.MethodGet, "/audience/alerts/options"},
		{http.MethodPut, "/audience/alerts/rule-1"},
		{http.MethodDelete, "/audience/alerts/rule-1"},
		{http.MethodPost, "/audience/alerts/rule-1/test"},
	} {
		req := httptest.NewRequest(c.method, c.path, nil)
		var match mux.RouteMatch
		if !router.Match(req, &match) {
			t.Fatalf("%s %s: no route", c.method, c.path)
			continue
		}
		func() {
			defer func() { _ = recover() }()
			match.Handler.ServeHTTP(httptest.NewRecorder(), req)
		}()
		if got := ac.calls[c.method+" "+c.path]; got != "audience:send" {
			t.Errorf("%s %s: rbac = %q, want audience:send", c.method, c.path, got)
		}
	}
}

// Route ordering: "/alerts/options" must reach the options handler, not be
// swallowed by the "/{id}" comment routes registered below it. Gorilla matches
// in registration order, so this is a real ordering dependency and not a
// theoretical one.
func TestAlertOptionsRouteIsNotSwallowedByCommentIDRoutes(t *testing.T) {
	router := mux.NewRouter()
	RegisterProtectedRoutes(router, &Handler{}, (&recordingAC{calls: map[string]string{}}).fn)

	var match mux.RouteMatch
	req := httptest.NewRequest(http.MethodGet, "/audience/alerts/options", nil)
	if !router.Match(req, &match) {
		t.Fatal("no route")
	}
	if id, ok := match.Vars["id"]; ok {
		t.Fatalf("matched a wildcard route with id = %q, want the options handler", id)
	}
}

// The vocabulary is served from the domain, so a picker cannot offer a metric
// the evaluator does not implement, or miss one it does.
func TestAlertOptionsMirrorsTheDomain(t *testing.T) {
	h := NewHandler(Deps{})
	rec := httptest.NewRecorder()
	h.AlertOptions(rec, withWorkspace(httptest.NewRequest(http.MethodGet, "/audience/alerts/options", nil), "ws-1"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body AlertVocabularyResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Metrics) != len(ca.AllAlertMetrics()) {
		t.Fatalf("metrics = %d, want %d", len(body.Metrics), len(ca.AllAlertMetrics()))
	}
	for _, m := range body.Metrics {
		metric := ca.AlertMetric(m.Metric)
		if !metric.Valid() {
			t.Fatalf("offered an unknown metric %q", m.Metric)
		}
		if m.Windowed != metric.IsWindowed() || m.Below != metric.TriggersWhenBelow() {
			t.Fatalf("%q described as windowed=%v below=%v", m.Metric, m.Windowed, m.Below)
		}
	}
	// The floors have to reach the client, or a form will offer a one-minute
	// cooldown that the server silently clamps.
	if body.Limits.MinCooldownMinutes != ca.MinAlertCooldownMinutes {
		t.Fatalf("min cooldown = %d", body.Limits.MinCooldownMinutes)
	}
	if body.Limits.MaxPerDay != ca.MaxAlertsPerDay {
		t.Fatalf("max per day = %d", body.Limits.MaxPerDay)
	}
	if body.Limits.TemplateParamCount != ca.AlertTemplateParamCount {
		t.Fatalf("template params = %d", body.Limits.TemplateParamCount)
	}
}

// The comment endpoints must keep showing comments and nothing else.

// The audience surface serves every subject kind unless narrowed, which is the
// whole reason it exists: "what is my audience saying" means comments and
// conversations, not one of them.
func TestAudienceInputServesEveryKindByDefault(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/audience?accountId=acc-1", nil)
	if got := listInput(req).SubjectKinds; len(got) != 0 {
		t.Fatalf("subject kinds = %v, want empty (every kind)", got)
	}
}

func TestAudienceInputNarrowsToRequestedKinds(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/audience?subjectKind=conversation", nil)
	got := listInput(req).SubjectKinds
	if len(got) != 1 || got[0] != ca.SubjectKindConversation {
		t.Fatalf("subject kinds = %v", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/audience?subjectKind=comment,conversation", nil)
	if got := listInput(req).SubjectKinds; len(got) != 2 {
		t.Fatalf("subject kinds = %v, want both", got)
	}
}

// An unknown kind is carried through to Validate rather than dropped, so the
// caller gets a message instead of a request that silently matched everything.
func TestAudienceInputKeepsUnknownKindsForValidation(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/audience?subjectKind=post", nil)
	in := listInput(req)
	in.WorkspaceID = "ws-1"
	if err := in.Validate(); err == nil {
		t.Fatal("an unknown subject kind should be refused, not ignored")
	}
}

// The conversation filters reach the domain, and a bad one is refused.
func TestAudienceInputCarriesConversationFilters(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet,
		"/audience?interest=interested&disposition=sale&qualification=hot_lead&nextAction=escalate", nil)
	in := listInput(req)
	in.WorkspaceID = "ws-1"
	if err := in.Validate(); err != nil {
		t.Fatalf("valid conversation filters rejected: %v", err)
	}
	if in.Interest != ca.InterestInterested || in.Disposition != ca.DispositionSale {
		t.Errorf("filters not carried: %+v", in)
	}
	if in.Qualification != ca.QualificationHotLead || in.NextAction != ca.NextActionEscalate {
		t.Errorf("filters not carried: %+v", in)
	}

	bad := httptest.NewRequest(http.MethodGet, "/audience?disposition=sold", nil)
	in = listInput(bad)
	in.WorkspaceID = "ws-1"
	if err := in.Validate(); err == nil {
		t.Fatal("an unknown disposition should be refused")
	}
}
