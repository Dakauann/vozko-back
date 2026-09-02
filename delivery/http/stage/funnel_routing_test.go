package stage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vozko/domain/auth"
	stagedomain "vozko/domain/stage"
	"vozko/infra/http/middleware"
	stage_usecase "vozko/usecases/stage"
)

// These wire the REAL use cases behind the handler, over a fake repository, so
// the whole delivery → use case → domain path is exercised: the query parameter
// is parsed, the funnel actually decides which stages are read, and the domain's
// coherence rule reaches the client as a status code. Stubbing the use cases here
// would have tested only that the handler calls something.

// --- fake repository ---------------------------------------------------------

type funnelRepo struct {
	stagedomain.Repository

	stagesByID map[string]*stagedomain.Stage
	byPipeline map[string][]*stagedomain.Stage
	byCampaign []*stagedomain.Stage
	entryStage *stagedomain.EntryStage

	created   []*stagedomain.Stage
	assigned  []*stagedomain.EntryStage
	lastPipe  string
	campaignH bool
}

func newFunnelRepo() *funnelRepo {
	return &funnelRepo{
		stagesByID: map[string]*stagedomain.Stage{},
		byPipeline: map[string][]*stagedomain.Stage{},
	}
}

func (r *funnelRepo) FindByID(id string) (*stagedomain.Stage, error) {
	return r.stagesByID[id], nil
}

func (r *funnelRepo) ListByPipeline(workspaceID, pipelineID string) ([]*stagedomain.Stage, error) {
	r.lastPipe = pipelineID
	return r.byPipeline[pipelineID], nil
}

func (r *funnelRepo) ListByCampaign(workspaceID, campaignID, campaignType string) ([]*stagedomain.Stage, error) {
	r.campaignH = true
	return r.byCampaign, nil
}

func (r *funnelRepo) EnsureDefaultStagesForCampaign(workspaceID, campaignID, campaignType string) error {
	return nil
}

func (r *funnelRepo) Create(s *stagedomain.Stage) error {
	r.created = append(r.created, s)
	r.stagesByID[s.ID] = s
	r.byPipeline[s.PipelineID] = append(r.byPipeline[s.PipelineID], s)
	return nil
}

func (r *funnelRepo) GetEntryStage(entryID, entryType, workspaceID string) (*stagedomain.EntryStage, error) {
	return r.entryStage, nil
}

func (r *funnelRepo) AssignStage(et *stagedomain.EntryStage) error {
	r.assigned = append(r.assigned, et)
	r.entryStage = et
	return nil
}

// --- harness -----------------------------------------------------------------

func newFunnelHandler(repo *funnelRepo) *StageHandler {
	return NewStageHandler(
		stage_usecase.NewCreateStageUseCase(repo),
		nil, // update
		nil, // delete
		stage_usecase.NewListStagesUseCase(repo),
		nil, // set initial
		stage_usecase.NewAssignEntryStageUseCase(repo, nil),
		nil, // remove
		nil, // get entry stage
		nil, // batch
		stage_usecase.NewReorderStagesUseCase(repo),
		nil, // broadcaster
	)
}

func authed(req *http.Request) *http.Request {
	ctx := context.WithValue(req.Context(), middleware.ClaimsContextKey,
		&auth.Claims{UserID: "user-1", Role: "member"})
	ctx = context.WithValue(ctx, middleware.WorkspaceIDContextKey, "ws-1")
	return req.WithContext(ctx)
}

// --- GET /stages -------------------------------------------------------------

func TestListStages_PipelineIDReachesTheRepository(t *testing.T) {
	repo := newFunnelRepo()
	repo.byPipeline["pipe-b"] = []*stagedomain.Stage{
		{ID: "s1", Name: "triagem", PipelineID: "pipe-b"},
		{ID: "s2", Name: "fechado", PipelineID: "pipe-b"},
	}
	repo.byCampaign = []*stagedomain.Stage{{ID: "d1", Name: "recebido"}}

	rec := httptest.NewRecorder()
	newFunnelHandler(repo).List(rec, authed(
		httptest.NewRequest(http.MethodGet, "/stages?pipelineId=pipe-b", nil)))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if repo.lastPipe != "pipe-b" {
		t.Errorf("the query parameter must select the funnel, got %q", repo.lastPipe)
	}
	if repo.campaignH {
		t.Error("a named funnel must not fall back to the campaign resolution")
	}

	var got []stagedomain.Stage
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "triagem" {
		t.Errorf("wrong funnel returned: %+v", got)
	}
}

func TestListStages_WithoutPipelineKeepsTheLegacyResolution(t *testing.T) {
	repo := newFunnelRepo()
	repo.byCampaign = []*stagedomain.Stage{{ID: "d1", Name: "recebido"}}

	rec := httptest.NewRecorder()
	newFunnelHandler(repo).List(rec, authed(
		httptest.NewRequest(http.MethodGet, "/stages", nil)))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !repo.campaignH || repo.lastPipe != "" {
		t.Error("with no funnel named, the campaign path must still run")
	}
}

// --- POST /stages ------------------------------------------------------------

func TestCreateStage_AttachesToTheRequestedFunnel(t *testing.T) {
	repo := newFunnelRepo()
	repo.byPipeline["pipe-b"] = []*stagedomain.Stage{
		{ID: "s1", Name: "triagem", PipelineID: "pipe-b", Position: 1},
	}

	body := `{"name":"Fechado","description":"fim","color":"#10b981","pipelineId":"pipe-b"}`
	rec := httptest.NewRecorder()
	newFunnelHandler(repo).Create(rec, authed(
		httptest.NewRequest(http.MethodPost, "/stages", strings.NewReader(body))))

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(repo.created) != 1 {
		t.Fatalf("expected one stage created, got %d", len(repo.created))
	}
	if repo.created[0].PipelineID != "pipe-b" {
		t.Errorf("stage must join the requested funnel, got %q", repo.created[0].PipelineID)
	}
	if repo.created[0].Position != 2 {
		t.Errorf("position continues that funnel's sequence, got %d", repo.created[0].Position)
	}
}

func TestCreateStage_DuplicateNameWithinTheFunnelIs409(t *testing.T) {
	repo := newFunnelRepo()
	repo.byPipeline["pipe-b"] = []*stagedomain.Stage{
		{ID: "s1", Name: "fechado", PipelineID: "pipe-b"},
	}

	body := `{"name":"Fechado","description":"d","pipelineId":"pipe-b"}`
	rec := httptest.NewRecorder()
	newFunnelHandler(repo).Create(rec, authed(
		httptest.NewRequest(http.MethodPost, "/stages", strings.NewReader(body))))

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateStage_SameNameOnAnotherFunnelIsAllowed(t *testing.T) {
	// Two funnels may each have a "fechado"; uniqueness is per funnel.
	repo := newFunnelRepo()
	repo.byPipeline["pipe-a"] = []*stagedomain.Stage{
		{ID: "s1", Name: "fechado", PipelineID: "pipe-a"},
	}

	body := `{"name":"Fechado","description":"d","pipelineId":"pipe-b"}`
	rec := httptest.NewRecorder()
	newFunnelHandler(repo).Create(rec, authed(
		httptest.NewRequest(http.MethodPost, "/stages", strings.NewReader(body))))

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- the coherence rule, end to end ------------------------------------------

func TestAssignEntryStage_CrossFunnelIs409WithAReadableMessage(t *testing.T) {
	repo := newFunnelRepo()
	repo.stagesByID["s-a"] = &stagedomain.Stage{ID: "s-a", WorkspaceID: "ws-1", PipelineID: "pipe-a", Name: "triagem"}
	repo.stagesByID["s-b"] = &stagedomain.Stage{ID: "s-b", WorkspaceID: "ws-1", PipelineID: "pipe-b", Name: "fechado"}
	repo.entryStage = &stagedomain.EntryStage{StageID: "s-a"}

	body := `{"StageID":"s-b","entryId":"e1","entryType":"whatsapp"}`
	rec := httptest.NewRecorder()
	newFunnelHandler(repo).AssignEntryStage(rec, authed(
		httptest.NewRequest(http.MethodPost, "/stages/entry", strings.NewReader(body))))

	if rec.Code != http.StatusConflict {
		t.Fatalf("a cross-funnel move must be 409, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(repo.assigned) != 0 {
		t.Error("a rejected move must write nothing")
	}
	// The operator sees this string; a bare error identifier would not help them.
	if !strings.Contains(rec.Body.String(), "outro funil") {
		t.Errorf("expected an actionable message, got %s", rec.Body.String())
	}
}

func TestAssignEntryStage_SameFunnelSucceeds(t *testing.T) {
	repo := newFunnelRepo()
	repo.stagesByID["s-1"] = &stagedomain.Stage{ID: "s-1", WorkspaceID: "ws-1", PipelineID: "pipe-a"}
	repo.stagesByID["s-2"] = &stagedomain.Stage{ID: "s-2", WorkspaceID: "ws-1", PipelineID: "pipe-a"}
	repo.entryStage = &stagedomain.EntryStage{StageID: "s-1"}

	body := `{"StageID":"s-2","entryId":"e1","entryType":"whatsapp"}`
	rec := httptest.NewRecorder()
	newFunnelHandler(repo).AssignEntryStage(rec, authed(
		httptest.NewRequest(http.MethodPost, "/stages/entry", strings.NewReader(body))))

	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Fatalf("a move within one funnel must succeed, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(repo.assigned) != 1 {
		t.Errorf("expected the move to be written, got %d", len(repo.assigned))
	}
}

func TestAssignEntryStage_FirstPlacementSucceeds(t *testing.T) {
	repo := newFunnelRepo()
	repo.stagesByID["s-b"] = &stagedomain.Stage{ID: "s-b", WorkspaceID: "ws-1", PipelineID: "pipe-b"}

	body := `{"StageID":"s-b","entryId":"e1","entryType":"whatsapp"}`
	rec := httptest.NewRecorder()
	newFunnelHandler(repo).AssignEntryStage(rec, authed(
		httptest.NewRequest(http.MethodPost, "/stages/entry", strings.NewReader(body))))

	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Fatalf("a lead with no stage must be placeable, got %d: %s", rec.Code, rec.Body.String())
	}
}
