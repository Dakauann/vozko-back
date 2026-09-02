package commentanalysis

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	ca "vozko/domain/comment_analysis"
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

// Every route carries the comment_analysis resource with the right action:
// reads are read, anything that changes state or spends money is update.
func TestRegisterProtectedRoutes_AppliesRBAC(t *testing.T) {
	router := mux.NewRouter()
	ac := &recordingAC{calls: map[string]string{}}
	RegisterProtectedRoutes(router, &Handler{}, ac.fn)

	cases := []struct{ method, path, want string }{
		{http.MethodGet, "/comment-analysis", "comment_analysis:read"},
		{http.MethodGet, "/comment-analysis/stats", "comment_analysis:read"},
		{http.MethodGet, "/comment-analysis/trends", "comment_analysis:read"},
		{http.MethodGet, "/comment-analysis/spend", "comment_analysis:read"},
		{http.MethodGet, "/comment-analysis/authors", "comment_analysis:read"},
		{http.MethodGet, "/comment-analysis/authors/a-1", "comment_analysis:read"},
		{http.MethodPatch, "/comment-analysis/authors/a-1", "comment_analysis:update"},
		{http.MethodGet, "/comment-analysis/settings/instagram/acc-1", "comment_analysis:read"},
		{http.MethodPatch, "/comment-analysis/settings/instagram/acc-1", "comment_analysis:update"},
		{http.MethodGet, "/comment-analysis/settings", "comment_analysis:read"},
		{http.MethodGet, "/comment-analysis/settings/instagram/acc-1/containers/m-1", "comment_analysis:read"},
		{http.MethodPut, "/comment-analysis/settings/instagram/acc-1/containers/m-1", "comment_analysis:update"},
		{http.MethodDelete, "/comment-analysis/settings/instagram/acc-1/containers/m-1", "comment_analysis:update"},
		{http.MethodGet, "/comment-analysis/backfill/instagram/acc-1/estimate", "comment_analysis:read"},
		{http.MethodPost, "/comment-analysis/backfill/instagram/acc-1", "comment_analysis:update"},
		{http.MethodGet, "/comment-analysis/backfill/b-1", "comment_analysis:read"},
		{http.MethodPost, "/comment-analysis/backfill/b-1/cancel", "comment_analysis:update"},
		{http.MethodPost, "/comment-analysis/row-1/retry", "comment_analysis:update"},
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
	if router.Match(httptest.NewRequest(http.MethodGet, "/comment-analysis", nil), &match) {
		t.Fatal("an unwired feature must expose no routes")
	}
}

// ---- scoping ----

type captureList struct{ in ca.ListInput }

func (c *captureList) Execute(_ context.Context, in ca.ListInput) (*shared.PaginatedResult[*ca.CommentAnalysis], error) {
	c.in = in
	return shared.NewPaginatedResult([]*ca.CommentAnalysis{}, in.Options.Pagination, 0), nil
}

// The workspace comes from the session, never from the query. A caller who
// sends ?workspaceId=other still lists their own rows.
func TestList_WorkspaceCannotBeOverriddenByQuery(t *testing.T) {
	uc := &captureList{}
	h := NewHandler(Deps{List: uc})
	req := httptest.NewRequest(http.MethodGet, "/comment-analysis?workspaceId=other-ws&accountId=acc-1&stance=hostile&severityMin=60&requiresAction=true&status=analyzed,failed&page=2&pageSize=10&sort=severity:desc&from=2026-09-01", nil)
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
	req := withWorkspace(httptest.NewRequest(http.MethodGet, "/comment-analysis/stats?workspaceId=x", nil), "ws-1")
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
	router.HandleFunc("/comment-analysis/authors/{id}", h.SetModeration).Methods(http.MethodPatch)

	req := withWorkspace(httptest.NewRequest(http.MethodPatch, "/comment-analysis/authors/a-1", strings.NewReader(`{"state":"banned"}`)), "ws-1")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	req = withWorkspace(httptest.NewRequest(http.MethodPatch, "/comment-analysis/authors/a-1", strings.NewReader(`{"state":"blocked"}`)), "ws-1")
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
	router.HandleFunc("/comment-analysis/settings/{source}/{accountId}/containers/{containerId}", h.PutContainerSettings).Methods(http.MethodPut)

	body := `{"enabled":true,"model":null,"severityThreshold":80,"instructions":"post sobre a obra","topics":[{"key":"obra","label":"Obra"}]}`
	req := withWorkspace(httptest.NewRequest(http.MethodPut, "/comment-analysis/settings/instagram/acc-1/containers/media-1?workspaceId=other", strings.NewReader(body)), "ws-1")
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
