package stage_usecase

import (
	"errors"
	"testing"

	"vozko/domain/stage"
)

type cloneGroupRepoStub struct {
	stage.StageGroupRepository
	group *stage.StageGroup
	err   error
}

func (s *cloneGroupRepoStub) FindByID(string) (*stage.StageGroup, error) { return s.group, s.err }

type cloneStageRepoStub struct {
	stage.Repository
	created          []*stage.Stage
	createdPipeline  string
	createdGroupID   string
	existingPipeline string // when set, FindConversationPipelineByGroup returns it (reuse path)
	setCampaign      string
	setPipeline      string
	err              error
}

func (s *cloneStageRepoStub) Create(st *stage.Stage) error {
	s.created = append(s.created, st)
	return s.err
}
func (s *cloneStageRepoStub) FindConversationPipelineByGroup(_, _ string) (string, error) {
	return s.existingPipeline, nil
}
func (s *cloneStageRepoStub) CreateConversationPipeline(_, _, stageGroupID string) (string, error) {
	s.createdPipeline = "pipe1"
	s.createdGroupID = stageGroupID
	return s.createdPipeline, nil
}
func (s *cloneStageRepoStub) SetCampaignPipeline(campaignID, _, pipelineID string) error {
	s.setCampaign = campaignID
	s.setPipeline = pipelineID
	return nil
}

// A campaign's group is cloned into a NEW conversation pipeline (pipeline-scoped,
// not campaign_id-scoped); the campaign is then pointed at that pipeline. First
// item is the initial stage, names lower-cased/trimmed.
func TestCloneStagesFromGroupCreatesStagesWithInitialFirst(t *testing.T) {
	groupRepo := &cloneGroupRepoStub{group: &stage.StageGroup{
		WorkspaceID: "ws1",
		Name:        "Vendas",
		Items: []stage.StageGroupItem{
			{Name: " New ", Description: "d", Color: "#fff", Position: 0},
			{Name: "Won", Position: 1},
		},
	}}
	stageRepo := &cloneStageRepoStub{}
	uc := NewCloneStagesFromGroupUseCase(groupRepo, stageRepo)

	if err := uc.Execute("ws1", "camp1", "voice", "grp1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stageRepo.created) != 2 {
		t.Fatalf("created %d stages, want 2", len(stageRepo.created))
	}
	first := stageRepo.created[0]
	if first.Name != "new" || !first.IsInitial ||
		first.PipelineID != "pipe1" || first.CampaignID != "" || first.WorkspaceID != "ws1" {
		t.Errorf("first stage wrong: %+v", first)
	}
	if stageRepo.created[1].IsInitial {
		t.Errorf("second stage should not be initial")
	}
	if stageRepo.setCampaign != "camp1" || stageRepo.setPipeline != "pipe1" {
		t.Errorf("campaign should be pointed at the new pipeline: camp=%q pipe=%q", stageRepo.setCampaign, stageRepo.setPipeline)
	}
}

func TestCloneStagesFromGroupRejectsCrossWorkspace(t *testing.T) {
	groupRepo := &cloneGroupRepoStub{group: &stage.StageGroup{
		WorkspaceID: "other",
		Items:       []stage.StageGroupItem{{Name: "x"}},
	}}
	stageRepo := &cloneStageRepoStub{}
	uc := NewCloneStagesFromGroupUseCase(groupRepo, stageRepo)

	if err := uc.Execute("ws1", "camp1", "voice", "grp1"); err == nil {
		t.Fatal("expected error for cross-workspace group")
	}
	if len(stageRepo.created) != 0 {
		t.Errorf("must not create stages for a cross-workspace group, created %d", len(stageRepo.created))
	}
}

func TestCloneStagesFromGroupPropagatesGroupError(t *testing.T) {
	uc := NewCloneStagesFromGroupUseCase(&cloneGroupRepoStub{err: errors.New("boom")}, &cloneStageRepoStub{})
	if err := uc.Execute("ws1", "c", "voice", "g"); err == nil {
		t.Fatal("expected error when group lookup fails")
	}
}

// Same group → same board: when a pipeline already exists for the group, the
// campaign is pointed at THAT pipeline and no stages are cloned (no duplicate).
func TestCloneStagesFromGroupReusesExistingPipeline(t *testing.T) {
	groupRepo := &cloneGroupRepoStub{group: &stage.StageGroup{
		WorkspaceID: "ws1", Name: "Vendas",
		Items: []stage.StageGroupItem{{Name: "a"}, {Name: "b"}},
	}}
	stageRepo := &cloneStageRepoStub{existingPipeline: "pipeExisting"}
	uc := NewCloneStagesFromGroupUseCase(groupRepo, stageRepo)

	if err := uc.Execute("ws1", "camp2", "voice", "grp1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stageRepo.created) != 0 {
		t.Errorf("reuse must not clone stages, created %d", len(stageRepo.created))
	}
	if stageRepo.createdPipeline != "" {
		t.Errorf("reuse must not create a new pipeline, created %q", stageRepo.createdPipeline)
	}
	if stageRepo.setCampaign != "camp2" || stageRepo.setPipeline != "pipeExisting" {
		t.Errorf("campaign should be pointed at the existing pipeline: camp=%q pipe=%q", stageRepo.setCampaign, stageRepo.setPipeline)
	}
}
