package pipeline_usecase

import (
	"errors"
	"testing"

	"vozko/domain/pipeline"
)

// guardRepo is the pipeline store the delete guard reads its facts from.
type guardRepo struct {
	pipeline.Repository

	byID    map[string]*pipeline.Pipeline
	deleted []string
}

func newGuardRepo(ps ...*pipeline.Pipeline) *guardRepo {
	r := &guardRepo{byID: map[string]*pipeline.Pipeline{}}
	for _, p := range ps {
		r.byID[p.ID] = p
	}
	return r
}

func (r *guardRepo) GetByID(workspaceID, id string) (*pipeline.Pipeline, error) {
	p, ok := r.byID[id]
	if !ok {
		return nil, pipeline.ErrNotFound
	}
	return p, nil
}

func (r *guardRepo) Delete(workspaceID, id string) error {
	r.deleted = append(r.deleted, id)
	return nil
}

// fakeOccupancy stands in for the cross-aggregate adapter.
type fakeOccupancy struct {
	usage    pipeline.Usage
	usageErr error

	vacatedFrom string
	vacatedInto string
	vacateCalls int
	vacateErr   error
}

func (o *fakeOccupancy) Usage(workspaceID, pipelineID string) (pipeline.Usage, error) {
	return o.usage, o.usageErr
}

func (o *fakeOccupancy) Vacate(workspaceID, pipelineID, intoPipelineID string) (int64, error) {
	o.vacateCalls++
	o.vacatedFrom = pipelineID
	o.vacatedInto = intoPipelineID
	return o.usage.Entries, o.vacateErr
}

func conv(id string) *pipeline.Pipeline {
	return &pipeline.Pipeline{ID: id, WorkspaceID: "ws", Name: id, ObjectType: pipeline.ObjectConversation}
}

// An empty, non-default funnel is the ordinary case: no destination needed, and
// the columns still get removed so nothing is orphaned behind the funnel.
func TestDeleteEmptyFunnelRemovesItsColumns(t *testing.T) {
	repo := newGuardRepo(conv("a"))
	occ := &fakeOccupancy{}
	uc := NewDeletePipelineUseCase(repo, occ)

	if err := uc.Execute("ws", "a", pipeline.DeletePipelineInput{}); err != nil {
		t.Fatalf("delete empty funnel: %v", err)
	}
	if len(repo.deleted) != 1 || repo.deleted[0] != "a" {
		t.Fatalf("funnel not deleted: %v", repo.deleted)
	}
	if occ.vacateCalls != 1 || occ.vacatedInto != "" {
		t.Fatalf("columns not vacated cleanly: calls=%d into=%q", occ.vacateCalls, occ.vacatedInto)
	}
}

// Something must receive a conversation that names no funnel.
func TestDeleteRefusesTheDefaultFunnel(t *testing.T) {
	def := conv("a")
	def.IsDefault = true
	repo := newGuardRepo(def)
	uc := NewDeletePipelineUseCase(repo, &fakeOccupancy{})

	err := uc.Execute("ws", "a", pipeline.DeletePipelineInput{})
	if !errors.Is(err, pipeline.ErrDeleteDefault) {
		t.Fatalf("want ErrDeleteDefault, got %v", err)
	}
	if len(repo.deleted) != 0 {
		t.Fatal("default funnel was deleted anyway")
	}
}

// A campaign, channel or deal still routing here blocks the delete outright:
// repointing a running campaign is the operator's call, never an inferred cascade.
func TestDeleteRefusesABoundFunnel(t *testing.T) {
	for name, usage := range map[string]pipeline.Usage{
		"campaign":    {Campaigns: 1},
		"channel":     {Channels: 2},
		"opportunity": {Opportunities: 5},
		"mixed":       {Campaigns: 1, Channels: 1, Entries: 30},
	} {
		t.Run(name, func(t *testing.T) {
			repo := newGuardRepo(conv("a"), conv("b"))
			occ := &fakeOccupancy{usage: usage}
			uc := NewDeletePipelineUseCase(repo, occ)

			err := uc.Execute("ws", "a", pipeline.DeletePipelineInput{MoveEntriesTo: "b"})
			if !errors.Is(err, pipeline.ErrDeleteBound) {
				t.Fatalf("want ErrDeleteBound, got %v", err)
			}
			if len(repo.deleted) != 0 || occ.vacateCalls != 0 {
				t.Fatal("a bound funnel was touched")
			}
		})
	}
}

// Conversations are never dropped with the funnel.
func TestDeleteRequiresADestinationWhenItHoldsConversations(t *testing.T) {
	repo := newGuardRepo(conv("a"))
	occ := &fakeOccupancy{usage: pipeline.Usage{Entries: 12}}
	uc := NewDeletePipelineUseCase(repo, occ)

	err := uc.Execute("ws", "a", pipeline.DeletePipelineInput{})
	if !errors.Is(err, pipeline.ErrDeleteNeedsDestination) {
		t.Fatalf("want ErrDeleteNeedsDestination, got %v", err)
	}
	if len(repo.deleted) != 0 {
		t.Fatal("funnel holding conversations was deleted")
	}
}

func TestDeleteRejectsAnImpossibleDestination(t *testing.T) {
	sales := &pipeline.Pipeline{ID: "s", WorkspaceID: "ws", Name: "Vendas", ObjectType: pipeline.ObjectOpportunity}
	repo := newGuardRepo(conv("a"), sales)
	occ := &fakeOccupancy{usage: pipeline.Usage{Entries: 3}}
	uc := NewDeletePipelineUseCase(repo, occ)

	// Itself.
	if err := uc.Execute("ws", "a", pipeline.DeletePipelineInput{MoveEntriesTo: "a"}); !errors.Is(err, pipeline.ErrDeleteDestinationInvalid) {
		t.Fatalf("self as destination: want ErrDeleteDestinationInvalid, got %v", err)
	}
	// A funnel organizing another object kind.
	if err := uc.Execute("ws", "a", pipeline.DeletePipelineInput{MoveEntriesTo: "s"}); !errors.Is(err, pipeline.ErrDeleteDestinationInvalid) {
		t.Fatalf("cross-kind destination: want ErrDeleteDestinationInvalid, got %v", err)
	}
	// A funnel that is not there.
	if err := uc.Execute("ws", "a", pipeline.DeletePipelineInput{MoveEntriesTo: "ghost"}); !errors.Is(err, pipeline.ErrNotFound) {
		t.Fatalf("missing destination: want ErrNotFound, got %v", err)
	}
	if len(repo.deleted) != 0 || occ.vacateCalls != 0 {
		t.Fatal("a funnel was touched despite an invalid destination")
	}
}

// The happy path with occupants: conversations move first, and only then does
// the funnel go.
func TestDeleteMovesConversationsBeforeRemovingTheFunnel(t *testing.T) {
	repo := newGuardRepo(conv("a"), conv("b"))
	occ := &fakeOccupancy{usage: pipeline.Usage{Entries: 40}}
	uc := NewDeletePipelineUseCase(repo, occ)

	if err := uc.Execute("ws", "a", pipeline.DeletePipelineInput{MoveEntriesTo: " b "}); err != nil {
		t.Fatalf("delete with move: %v", err)
	}
	if occ.vacatedFrom != "a" || occ.vacatedInto != "b" {
		t.Fatalf("moved wrong way: %s -> %s", occ.vacatedFrom, occ.vacatedInto)
	}
	if len(repo.deleted) != 1 || repo.deleted[0] != "a" {
		t.Fatalf("funnel not deleted after the move: %v", repo.deleted)
	}
}

// A failed move must not be followed by the delete: the conversations would be
// left on stages that are about to disappear.
func TestDeleteStopsWhenTheMoveFails(t *testing.T) {
	repo := newGuardRepo(conv("a"), conv("b"))
	occ := &fakeOccupancy{usage: pipeline.Usage{Entries: 4}, vacateErr: errors.New("boom")}
	uc := NewDeletePipelineUseCase(repo, occ)

	if err := uc.Execute("ws", "a", pipeline.DeletePipelineInput{MoveEntriesTo: "b"}); err == nil {
		t.Fatal("want the move error, got nil")
	}
	if len(repo.deleted) != 0 {
		t.Fatal("funnel deleted after a failed move")
	}
}

// A funnel that is not there is a 404, not a silent success.
func TestDeleteMissingFunnelIsNotFound(t *testing.T) {
	repo := newGuardRepo()
	uc := NewDeletePipelineUseCase(repo, &fakeOccupancy{})

	if err := uc.Execute("ws", "ghost", pipeline.DeletePipelineInput{}); !errors.Is(err, pipeline.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

// Usage is read straight through, so the dialog and the guard cannot disagree
// about what the funnel holds.
func TestGetUsageReadsTheSameSourceTheGuardDoes(t *testing.T) {
	repo := newGuardRepo(conv("a"))
	occ := &fakeOccupancy{usage: pipeline.Usage{Entries: 7, Campaigns: 2}}
	uc := NewGetPipelineUsageUseCase(repo, occ)

	got, err := uc.Execute("ws", "a")
	if err != nil {
		t.Fatalf("usage: %v", err)
	}
	if got.Entries != 7 || got.Campaigns != 2 {
		t.Fatalf("usage not passed through: %+v", got)
	}
	if got.Deletable() {
		t.Fatal("a funnel with two campaigns reported as deletable")
	}
}
