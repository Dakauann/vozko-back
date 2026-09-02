package stage_usecase

import (
	"testing"

	"vozko/domain/stage"
)

// --- fake repository (only the methods these paths touch) --------------------

type coherenceRepo struct {
	stage.Repository

	stages     map[string]*stage.Stage
	entryStage *stage.EntryStage
	assigned   []*stage.EntryStage

	groups          map[string]*stage.StageGroup
	pipelineByGroup map[string]string
	createdPipes    []string
	createdStages   []*stage.Stage
	listedPipeline  string
	listedCampaign  bool
	ensuredDefaults bool
}

func newCoherenceRepo() *coherenceRepo {
	return &coherenceRepo{
		stages:          map[string]*stage.Stage{},
		groups:          map[string]*stage.StageGroup{},
		pipelineByGroup: map[string]string{},
	}
}

func (r *coherenceRepo) FindByID(id string) (*stage.Stage, error) { return r.stages[id], nil }

func (r *coherenceRepo) GetEntryStage(entryID, entryType, workspaceID string) (*stage.EntryStage, error) {
	return r.entryStage, nil
}

func (r *coherenceRepo) AssignStage(et *stage.EntryStage) error {
	r.assigned = append(r.assigned, et)
	r.entryStage = et
	return nil
}

func (r *coherenceRepo) ListByPipeline(workspaceID, pipelineID string) ([]*stage.Stage, error) {
	r.listedPipeline = pipelineID
	return []*stage.Stage{{ID: "s-p", PipelineID: pipelineID}}, nil
}

func (r *coherenceRepo) ListByCampaign(workspaceID, campaignID, campaignType string) ([]*stage.Stage, error) {
	r.listedCampaign = true
	return []*stage.Stage{{ID: "s-default"}}, nil
}

func (r *coherenceRepo) EnsureDefaultStagesForCampaign(workspaceID, campaignID, campaignType string) error {
	r.ensuredDefaults = true
	return nil
}

func (r *coherenceRepo) FindConversationPipelineByGroup(workspaceID, groupID string) (string, error) {
	return r.pipelineByGroup[groupID], nil
}

func (r *coherenceRepo) CreateConversationPipeline(workspaceID, name, groupID string) (string, error) {
	id := "pipe-for-" + groupID
	r.pipelineByGroup[groupID] = id
	r.createdPipes = append(r.createdPipes, id)
	return id, nil
}

func (r *coherenceRepo) Create(s *stage.Stage) error {
	r.createdStages = append(r.createdStages, s)
	return nil
}

type coherenceGroupRepo struct {
	stage.StageGroupRepository
	groups map[string]*stage.StageGroup
}

func (g *coherenceGroupRepo) FindByID(id string) (*stage.StageGroup, error) {
	return g.groups[id], nil
}

func whatsappInput(stageID string) stage.AssignEntryStageInput {
	return stage.AssignEntryStageInput{StageID: stageID, EntryID: "e1", EntryType: "whatsapp"}
}

// --- the coherence rule ------------------------------------------------------

func TestAssignEntryStage_FirstPlacementIsUnconstrained(t *testing.T) {
	// A lead with no stage is on no funnel yet, so nothing can contradict.
	repo := newCoherenceRepo()
	repo.stages["s-b"] = &stage.Stage{ID: "s-b", WorkspaceID: "ws", PipelineID: "pipe-b"}

	uc := NewAssignEntryStageUseCase(repo, nil)
	if _, err := uc.Execute("ws", whatsappInput("s-b")); err != nil {
		t.Fatalf("first placement must be allowed: %v", err)
	}
	if len(repo.assigned) != 1 {
		t.Fatalf("expected the assignment to go through, got %d", len(repo.assigned))
	}
}

func TestAssignEntryStage_SameFunnelIsAllowed(t *testing.T) {
	repo := newCoherenceRepo()
	repo.stages["s-1"] = &stage.Stage{ID: "s-1", WorkspaceID: "ws", PipelineID: "pipe-a"}
	repo.stages["s-2"] = &stage.Stage{ID: "s-2", WorkspaceID: "ws", PipelineID: "pipe-a"}
	repo.entryStage = &stage.EntryStage{StageID: "s-1"}

	uc := NewAssignEntryStageUseCase(repo, nil)
	if _, err := uc.Execute("ws", whatsappInput("s-2")); err != nil {
		t.Fatalf("moving within one funnel must be allowed: %v", err)
	}
	if len(repo.assigned) != 1 {
		t.Fatal("expected the move to be applied")
	}
}

func TestAssignEntryStage_CrossFunnelIsObservedNotBlocked(t *testing.T) {
	// Ships in observe mode: the read paths still hand out the wrong funnel's
	// stages, so rejecting here would break the UI instead of fixing it.
	repo := newCoherenceRepo()
	repo.stages["s-a"] = &stage.Stage{ID: "s-a", WorkspaceID: "ws", PipelineID: "pipe-a"}
	repo.stages["s-b"] = &stage.Stage{ID: "s-b", WorkspaceID: "ws", PipelineID: "pipe-b"}
	repo.entryStage = &stage.EntryStage{StageID: "s-a"}

	uc := NewAssignEntryStageUseCase(repo, nil)
	_, err := uc.Execute("ws", whatsappInput("s-b"))

	if enforcePipelineCoherence {
		if err == nil {
			t.Fatal("with enforcement on, a cross-funnel move must be rejected")
		}
		if len(repo.assigned) != 0 {
			t.Fatal("a rejected move must not write")
		}
		return
	}
	if err != nil {
		t.Fatalf("with enforcement off, the move must still go through: %v", err)
	}
	if len(repo.assigned) != 1 {
		t.Fatal("observe mode must not block the write")
	}
}

func TestAssignEntryStage_LegacyStagesWithoutAPipelineNeverTrip(t *testing.T) {
	// Rows predating the pipeline migration carry no pipeline; treating "" as its
	// own funnel would flag every one of them.
	repo := newCoherenceRepo()
	repo.stages["s-old"] = &stage.Stage{ID: "s-old", WorkspaceID: "ws"}
	repo.stages["s-new"] = &stage.Stage{ID: "s-new", WorkspaceID: "ws", PipelineID: "pipe-a"}
	repo.entryStage = &stage.EntryStage{StageID: "s-old"}

	uc := NewAssignEntryStageUseCase(repo, nil)
	if _, err := uc.Execute("ws", whatsappInput("s-new")); err != nil {
		t.Fatalf("a legacy stage must not trip the rule: %v", err)
	}
}

func TestAssignEntryStage_StillRejectsAnotherWorkspacesStage(t *testing.T) {
	// The coherence check is an integrity rule, not an authorization one; the
	// existing workspace gate has to keep firing first.
	repo := newCoherenceRepo()
	repo.stages["s-x"] = &stage.Stage{ID: "s-x", WorkspaceID: "other-ws", PipelineID: "pipe-x"}

	uc := NewAssignEntryStageUseCase(repo, nil)
	if _, err := uc.Execute("ws", whatsappInput("s-x")); err != stage.ErrUnauthorized {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
	if len(repo.assigned) != 0 {
		t.Fatal("a cross-workspace stage must never be written")
	}
}

// --- listing one funnel ------------------------------------------------------

func TestListStages_PipelineIDWinsOverTheCampaignFallback(t *testing.T) {
	repo := newCoherenceRepo()
	uc := NewListStagesUseCase(repo)

	got, err := uc.Execute("ws", "camp-1", "whatsapp", "pipe-chosen")
	if err != nil {
		t.Fatal(err)
	}
	if repo.listedPipeline != "pipe-chosen" {
		t.Errorf("expected the named pipeline to be read, got %q", repo.listedPipeline)
	}
	if repo.listedCampaign {
		t.Error("an explicit pipeline must not fall back to the campaign resolution")
	}
	if repo.ensuredDefaults {
		t.Error("reading one funnel must not seed the DEFAULT funnel as a side effect")
	}
	if len(got) != 1 || got[0].PipelineID != "pipe-chosen" {
		t.Errorf("wrong stages returned: %+v", got)
	}
}

func TestListStages_FallsBackToCampaignWhenNoPipelineGiven(t *testing.T) {
	repo := newCoherenceRepo()
	uc := NewListStagesUseCase(repo)

	if _, err := uc.Execute("ws", "camp-1", "whatsapp", ""); err != nil {
		t.Fatal(err)
	}
	if !repo.listedCampaign || !repo.ensuredDefaults {
		t.Error("the legacy path must be unchanged when no pipeline is named")
	}
	if repo.listedPipeline != "" {
		t.Error("no pipeline was named; none should have been read")
	}
}

// --- a group materializes its funnel ----------------------------------------

func TestEnsurePipelineForGroup_CreatesTheFunnelAndItsStages(t *testing.T) {
	repo := newCoherenceRepo()
	groups := &coherenceGroupRepo{groups: map[string]*stage.StageGroup{
		"g1": {
			ID: "g1", WorkspaceID: "ws", Name: "Pós-venda",
			Items: []stage.StageGroupItem{
				{Name: " Triagem ", Position: 1},
				{Name: "Resolvido", Position: 2},
			},
		},
	}}

	id, err := ensurePipelineForGroup(groups, repo, "ws", "g1")
	if err != nil {
		t.Fatal(err)
	}
	if id == "" || len(repo.createdPipes) != 1 {
		t.Fatalf("expected exactly one pipeline, got %v", repo.createdPipes)
	}
	if len(repo.createdStages) != 2 {
		t.Fatalf("expected both items cloned, got %d", len(repo.createdStages))
	}
	if repo.createdStages[0].Name != "triagem" {
		t.Errorf("item names are normalized lower/trimmed, got %q", repo.createdStages[0].Name)
	}
	if !repo.createdStages[0].IsInitial || repo.createdStages[1].IsInitial {
		t.Error("only the first item is the initial stage")
	}
	for _, s := range repo.createdStages {
		if s.PipelineID != id {
			t.Errorf("stages must be pipeline-scoped, got %q", s.PipelineID)
		}
	}
}

func TestEnsurePipelineForGroup_IsIdempotent(t *testing.T) {
	// Both doors call this — group creation and campaign attach — in either order.
	// "Same group, same funnel" is what keeps them from forking duplicates.
	repo := newCoherenceRepo()
	groups := &coherenceGroupRepo{groups: map[string]*stage.StageGroup{
		"g1": {ID: "g1", WorkspaceID: "ws", Name: "Pós-venda",
			Items: []stage.StageGroupItem{{Name: "Triagem", Position: 1}}},
	}}

	first, err := ensurePipelineForGroup(groups, repo, "ws", "g1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := ensurePipelineForGroup(groups, repo, "ws", "g1")
	if err != nil {
		t.Fatal(err)
	}

	if first != second {
		t.Errorf("the same group must resolve to the same funnel: %q vs %q", first, second)
	}
	if len(repo.createdPipes) != 1 {
		t.Errorf("expected no duplicate pipeline, got %v", repo.createdPipes)
	}
	if len(repo.createdStages) != 1 {
		t.Errorf("expected the stages not to be cloned twice, got %d", len(repo.createdStages))
	}
}

func TestEnsurePipelineForGroup_RejectsACrossWorkspaceGroup(t *testing.T) {
	repo := newCoherenceRepo()
	groups := &coherenceGroupRepo{groups: map[string]*stage.StageGroup{
		"g1": {ID: "g1", WorkspaceID: "other-ws", Name: "Alheio"},
	}}

	if _, err := ensurePipelineForGroup(groups, repo, "ws", "g1"); err == nil {
		t.Fatal("a group from another workspace must not be materialized")
	}
	if len(repo.createdPipes) != 0 {
		t.Fatal("nothing should have been created")
	}
}
