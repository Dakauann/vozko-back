package container

import (
	"testing"

	stage_domain "vozko/domain/stage"
)

type seederRepo struct {
	stage_domain.Repository

	byPipeline map[string][]*stage_domain.Stage
	created    []*stage_domain.Stage
	listErr    error
}

func (r *seederRepo) ListByPipeline(workspaceID, pipelineID string) ([]*stage_domain.Stage, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.byPipeline[pipelineID], nil
}

func (r *seederRepo) Create(s *stage_domain.Stage) error {
	r.created = append(r.created, s)
	return nil
}

func TestSeedConversationPipeline_DefaultsWhenNoSourceGiven(t *testing.T) {
	repo := &seederRepo{byPipeline: map[string][]*stage_domain.Stage{}}
	seeder := pipelineStageSeeder{stages: repo}

	if err := seeder.SeedConversationPipeline("ws", "pipe-new", ""); err != nil {
		t.Fatal(err)
	}

	if len(repo.created) != len(stage_domain.DefaultStages) {
		t.Fatalf("expected the product defaults, got %d stages", len(repo.created))
	}
	// Read from the domain, not restated here: a change there must reach new funnels.
	if repo.created[0].Name != stage_domain.DefaultStages[0].Name {
		t.Errorf("first column should be %q, got %q",
			stage_domain.DefaultStages[0].Name, repo.created[0].Name)
	}
	for i, s := range repo.created {
		if s.PipelineID != "pipe-new" {
			t.Errorf("stage %d not attached to the new funnel: %q", i, s.PipelineID)
		}
		if s.Position != i+1 {
			t.Errorf("positions must be a 1-based sequence, got %d at %d", s.Position, i)
		}
	}
	if !repo.created[0].IsInitial {
		t.Error("the first column is where new conversations land")
	}
	for _, s := range repo.created[1:] {
		if s.IsInitial {
			t.Errorf("only one initial column, %q also claims it", s.Name)
		}
	}
}

func TestSeedConversationPipeline_CopiesAnExistingFunnel(t *testing.T) {
	repo := &seederRepo{byPipeline: map[string][]*stage_domain.Stage{
		"pipe-src": {
			{Name: "triagem", Color: "#111", Description: "a", Position: 1},
			{Name: "proposta", Color: "#222", Description: "b", Position: 2},
			{Name: "ganho", Color: "#333", Description: "c", Position: 3},
		},
	}}
	seeder := pipelineStageSeeder{stages: repo}

	if err := seeder.SeedConversationPipeline("ws", "pipe-new", "pipe-src"); err != nil {
		t.Fatal(err)
	}

	if len(repo.created) != 3 {
		t.Fatalf("expected the source's three columns, got %d", len(repo.created))
	}
	if repo.created[1].Name != "proposta" || repo.created[1].Color != "#222" {
		t.Errorf("column not copied faithfully: %+v", repo.created[1])
	}
	if !repo.created[0].IsInitial {
		t.Error("position decides the initial column, so the first copied one carries it")
	}
}

func TestSeedConversationPipeline_FallsBackWhenTheSourceIsEmpty(t *testing.T) {
	// Duplicating an empty funnel would produce another empty one, and the caller
	// asked for a working funnel.
	repo := &seederRepo{byPipeline: map[string][]*stage_domain.Stage{"pipe-src": {}}}
	seeder := pipelineStageSeeder{stages: repo}

	if err := seeder.SeedConversationPipeline("ws", "pipe-new", "pipe-src"); err != nil {
		t.Fatal(err)
	}
	if len(repo.created) != len(stage_domain.DefaultStages) {
		t.Fatalf("expected a fallback to the defaults, got %d", len(repo.created))
	}
}

func TestSeedConversationPipeline_NormalizesCopiedNames(t *testing.T) {
	repo := &seederRepo{byPipeline: map[string][]*stage_domain.Stage{
		"pipe-src": {{Name: "  Em Atendimento  ", Position: 1}},
	}}
	seeder := pipelineStageSeeder{stages: repo}

	if err := seeder.SeedConversationPipeline("ws", "pipe-new", "pipe-src"); err != nil {
		t.Fatal(err)
	}
	if repo.created[0].Name != "em atendimento" {
		t.Errorf("names are lower-cased and trimmed like every other stage write, got %q",
			repo.created[0].Name)
	}
}

func TestSeedConversationPipeline_RequiresWorkspaceAndPipeline(t *testing.T) {
	repo := &seederRepo{byPipeline: map[string][]*stage_domain.Stage{}}
	seeder := pipelineStageSeeder{stages: repo}

	if err := seeder.SeedConversationPipeline("", "pipe-new", ""); err == nil {
		t.Error("a missing workspace must be refused")
	}
	if err := seeder.SeedConversationPipeline("ws", "", ""); err == nil {
		t.Error("a missing pipeline must be refused")
	}
	if len(repo.created) != 0 {
		t.Error("a refused seed must write nothing")
	}
}
