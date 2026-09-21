package stage_usecase

import (
	"testing"

	"vozko/domain/stage"
)

type scopedRepo struct {
	stage.Repository

	byPipeline map[string][]*stage.Stage
	byCampaign []*stage.Stage

	listedPipeline string
	listedCampaign bool
	reordered      []string
	created        []*stage.Stage
}

func newScopedRepo() *scopedRepo {
	return &scopedRepo{byPipeline: map[string][]*stage.Stage{}}
}

func (r *scopedRepo) ListByPipeline(workspaceID, pipelineID string) ([]*stage.Stage, error) {
	r.listedPipeline = pipelineID
	return r.byPipeline[pipelineID], nil
}

func (r *scopedRepo) ListByCampaign(workspaceID, campaignID, campaignType string) ([]*stage.Stage, error) {
	r.listedCampaign = true
	return r.byCampaign, nil
}

func (r *scopedRepo) ReorderStages(workspaceID string, ids []string) error {
	r.reordered = ids
	return nil
}

func (r *scopedRepo) Create(s *stage.Stage) error {
	r.created = append(r.created, s)
	r.byPipeline[s.PipelineID] = append(r.byPipeline[s.PipelineID], s)
	return nil
}

func (r *scopedRepo) FindByID(id string) (*stage.Stage, error) {
	for _, s := range r.created {
		if s.ID == id {
			return s, nil
		}
	}
	return nil, nil
}

func TestCreateStage_LandsOnTheNamedFunnel(t *testing.T) {
	repo := newScopedRepo()
	repo.byPipeline["pipe-b"] = []*stage.Stage{
		{ID: "s1", Name: "triagem", PipelineID: "pipe-b", Position: 1},
	}
	uc := NewCreateStageUseCase(repo)

	got, err := uc.Execute("ws", stage.CreateStageInput{
		Name: "Fechado", Description: "fim", PipelineID: "pipe-b",
	})
	if err != nil {
		t.Fatal(err)
	}
	if repo.listedPipeline != "pipe-b" || repo.listedCampaign {
		t.Error("uniqueness and position must be computed within the named funnel")
	}
	if len(repo.created) != 1 || repo.created[0].PipelineID != "pipe-b" {
		t.Fatalf("stage must be attached to the named funnel, got %+v", repo.created)
	}
	if repo.created[0].Position != 2 {
		t.Errorf("position continues that funnel's sequence, got %d", repo.created[0].Position)
	}
	if got == nil {
		t.Error("expected the created stage back")
	}
}

func TestCreateStage_NameCollidesOnlyWithinItsOwnFunnel(t *testing.T) {
	repo := newScopedRepo()
	repo.byPipeline["pipe-a"] = []*stage.Stage{{ID: "s1", Name: "fechado", PipelineID: "pipe-a"}}
	repo.byPipeline["pipe-b"] = []*stage.Stage{{ID: "s2", Name: "triagem", PipelineID: "pipe-b"}}
	uc := NewCreateStageUseCase(repo)

	if _, err := uc.Execute("ws", stage.CreateStageInput{
		Name: "Fechado", Description: "d", PipelineID: "pipe-b",
	}); err != nil {
		t.Fatalf("a name used by ANOTHER funnel must not collide: %v", err)
	}

	if _, err := uc.Execute("ws", stage.CreateStageInput{
		Name: "Fechado", Description: "d", PipelineID: "pipe-a",
	}); err != stage.ErrTagNameExists {
		t.Fatalf("a duplicate within the SAME funnel must be rejected, got %v", err)
	}
}

func TestCreateStage_WithoutAFunnelKeepsTheLegacyPath(t *testing.T) {
	repo := newScopedRepo()
	repo.byCampaign = []*stage.Stage{{ID: "s1", Name: "recebido", Position: 1}}
	uc := NewCreateStageUseCase(repo)

	if _, err := uc.Execute("ws", stage.CreateStageInput{
		Name: "Nova", Description: "d", CampaignID: "camp-1",
	}); err != nil {
		t.Fatal(err)
	}
	if !repo.listedCampaign {
		t.Error("with no funnel named, the campaign resolution must still be used")
	}
	if repo.created[0].PipelineID != "" {
		t.Error("an unnamed funnel is left for the repository to default, not guessed here")
	}
}

func TestReorderStages_ReadsBackTheNamedFunnel(t *testing.T) {
	repo := newScopedRepo()
	repo.byPipeline["pipe-b"] = []*stage.Stage{{ID: "s2"}, {ID: "s1"}}
	uc := NewReorderStagesUseCase(repo)

	got, err := uc.Execute("ws", stage.ReorderStagesInput{
		StageIDs: []string{"s2", "s1"}, PipelineID: "pipe-b",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(repo.reordered) != 2 {
		t.Fatal("the reorder must reach the repository")
	}
	if repo.listedPipeline != "pipe-b" || repo.listedCampaign {
		t.Error("the reordered funnel must be the one read back")
	}
	if len(got) != 2 || got[0].ID != "s2" {
		t.Errorf("wrong list returned: %+v", got)
	}
}

func TestReorderStages_EmptyListStillReturnsTheFunnel(t *testing.T) {
	repo := newScopedRepo()
	repo.byPipeline["pipe-b"] = []*stage.Stage{{ID: "s1"}}
	uc := NewReorderStagesUseCase(repo)

	got, err := uc.Execute("ws", stage.ReorderStagesInput{PipelineID: "pipe-b"})
	if err != nil {
		t.Fatal(err)
	}
	if repo.reordered != nil {
		t.Error("nothing to reorder must not write")
	}
	if len(got) != 1 {
		t.Errorf("the funnel is still returned, got %+v", got)
	}
}
