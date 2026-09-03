package container

import (
	"testing"

	pipeline_domain "vozko/domain/pipeline"
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

	if err := seeder.SeedConversationPipeline("ws", "pipe-new", "", nil); err != nil {
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

	if err := seeder.SeedConversationPipeline("ws", "pipe-new", "pipe-src", nil); err != nil {
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

	if err := seeder.SeedConversationPipeline("ws", "pipe-new", "pipe-src", nil); err != nil {
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

	if err := seeder.SeedConversationPipeline("ws", "pipe-new", "pipe-src", nil); err != nil {
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

	if err := seeder.SeedConversationPipeline("", "pipe-new", "", nil); err == nil {
		t.Error("a missing workspace must be refused")
	}
	if err := seeder.SeedConversationPipeline("ws", "", "", nil); err == nil {
		t.Error("a missing pipeline must be refused")
	}
	if len(repo.created) != 0 {
		t.Error("a refused seed must write nothing")
	}
}

// The composer's own columns are the funnel. Nothing merges a template into
// them, and nothing reorders them: the operator drew the board they wanted.
func TestSeedConversationPipeline_DrawnStagesWin(t *testing.T) {
	repo := &seederRepo{byPipeline: map[string][]*stage_domain.Stage{
		"pipe-source": {
			{ID: "s1", Name: "herdada", Color: "#111111", Position: 1},
		},
	}}
	seeder := pipelineStageSeeder{stages: repo}

	drawn := []pipeline_domain.StageSeed{
		{Name: "Triagem", Description: "Primeiro contato", Color: "#3B82F6"},
		{Name: "Proposta enviada", Color: "#F59E0B"},
		{Name: "Fechado"},
	}

	// A copy source is passed too, and must lose: a named list is the more
	// specific intent.
	if err := seeder.SeedConversationPipeline("ws", "pipe-new", "pipe-source", drawn); err != nil {
		t.Fatal(err)
	}

	if len(repo.created) != 3 {
		t.Fatalf("expected the 3 drawn columns, got %d", len(repo.created))
	}
	for i, want := range []string{"triagem", "proposta enviada", "fechado"} {
		if repo.created[i].Name != want {
			t.Errorf("column %d: want %q, got %q", i, want, repo.created[i].Name)
		}
		if repo.created[i].Position != i+1 {
			t.Errorf("column %d: position %d", i, repo.created[i].Position)
		}
	}
	if !repo.created[0].IsInitial {
		t.Error("the first drawn column should receive arriving conversations")
	}
	if repo.created[1].IsInitial || repo.created[2].IsInitial {
		t.Error("only one column may be the entry stage")
	}
	if repo.created[0].Description != "Primeiro contato" || repo.created[1].Color != "#F59E0B" {
		t.Error("description and colour did not survive the seed")
	}
}

// A list editor produces an empty trailing row whenever someone adds one and
// changes their mind. Losing the funnel over it would be the worse outcome.
func TestSeedConversationPipeline_BlankDrawnRowsAreDropped(t *testing.T) {
	repo := &seederRepo{byPipeline: map[string][]*stage_domain.Stage{}}
	seeder := pipelineStageSeeder{stages: repo}

	drawn := []pipeline_domain.StageSeed{
		{Name: "  Triagem  "},
		{Name: "   "},
		{Name: ""},
	}
	if err := seeder.SeedConversationPipeline("ws", "pipe-new", "", drawn); err != nil {
		t.Fatal(err)
	}
	if len(repo.created) != 1 || repo.created[0].Name != "triagem" {
		t.Fatalf("blank rows were not dropped: %+v", repo.created)
	}
}

// All-blank is indistinguishable from "sent nothing", and a funnel with no
// columns is not a funnel — so it falls back rather than shipping an empty board.
func TestSeedConversationPipeline_AllBlankDrawnFallsBackToDefaults(t *testing.T) {
	repo := &seederRepo{byPipeline: map[string][]*stage_domain.Stage{}}
	seeder := pipelineStageSeeder{stages: repo}

	drawn := []pipeline_domain.StageSeed{{Name: "  "}, {Name: ""}}
	if err := seeder.SeedConversationPipeline("ws", "pipe-new", "", drawn); err != nil {
		t.Fatal(err)
	}
	if len(repo.created) != len(stage_domain.DefaultStages) {
		t.Fatalf("expected the defaults, got %d columns", len(repo.created))
	}
}
