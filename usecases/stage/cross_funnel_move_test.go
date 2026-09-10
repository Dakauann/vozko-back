package stage_usecase

import (
	"errors"
	"testing"

	"vozko/domain/stage"
)

// Moving a conversation between funnels is a real operator action: a lead
// qualified on the support funnel belongs on the sales one, and until now the
// only way was to have never staged them at all.
//
// It stays BLOCKED by default, on purpose. enforcePipelineCoherence exists
// because a mis-scoped stage dropdown used to offer another funnel's stages, and
// a click on one silently stranded the lead on a board nobody looks at. The
// escape hatch is therefore opt-in per call: a caller has to say it means it.
//
// The three callers of this use case are the manual endpoint, the CRM bulk
// action and the AI's manage_entry_stage tool. Only the first may set the flag,
// and the tests below pin that the default keeps the other two out.

func crossFunnelRepo() *coherenceRepo {
	repo := newCoherenceRepo()
	repo.stages["s-a"] = &stage.Stage{ID: "s-a", WorkspaceID: "ws", PipelineID: "pipe-a", Name: "qualificação"}
	repo.stages["s-b"] = &stage.Stage{ID: "s-b", WorkspaceID: "ws", PipelineID: "pipe-b", Name: "proposta"}
	repo.entryStage = &stage.EntryStage{StageID: "s-a"}
	return repo
}

// The default is unchanged: an ordinary move across funnels is still refused,
// which is what protects an operator clicking a stage in a list that turned out
// to belong to another funnel.
func TestCrossFunnelMoveIsStillRefusedByDefault(t *testing.T) {
	repo := crossFunnelRepo()
	uc := NewAssignEntryStageUseCase(repo, nil)

	_, err := uc.Execute("ws", stage.AssignEntryStageInput{
		StageID: "s-b", EntryID: "e-1", EntryType: "whatsapp",
	})
	if !errors.Is(err, stage.ErrStagePipelineMismatch) {
		t.Fatalf("err = %v, want ErrStagePipelineMismatch", err)
	}
	if len(repo.assigned) != 0 {
		t.Fatal("a refused move must not write")
	}
}

// The deliberate move. The operator picked a funnel, then a stage inside it, and
// the client said so.
func TestCrossFunnelMoveIsAllowedWhenExplicitlyRequested(t *testing.T) {
	repo := crossFunnelRepo()
	uc := NewAssignEntryStageUseCase(repo, nil)

	got, err := uc.Execute("ws", stage.AssignEntryStageInput{
		StageID: "s-b", EntryID: "e-1", EntryType: "whatsapp",
		AllowCrossPipeline: true,
	})
	if err != nil {
		t.Fatalf("an explicit cross-funnel move must be allowed: %v", err)
	}
	if got == nil {
		t.Fatal("no entry stage returned")
	}
	if len(repo.assigned) != 1 {
		t.Fatalf("assigned = %d, want the move applied", len(repo.assigned))
	}
	if repo.assigned[0].StageID != "s-b" {
		t.Errorf("landed on %q, want s-b", repo.assigned[0].StageID)
	}
}

// The flag is an escape hatch for ONE rule and must not become a general
// bypass. Another workspace's stage is an authorization failure, and no client
// flag may reach past it.
func TestCrossFunnelMoveStillRefusesAnotherWorkspacesStage(t *testing.T) {
	repo := newCoherenceRepo()
	repo.stages["s-a"] = &stage.Stage{ID: "s-a", WorkspaceID: "ws", PipelineID: "pipe-a"}
	repo.stages["s-other"] = &stage.Stage{ID: "s-other", WorkspaceID: "outra-ws", PipelineID: "pipe-x"}
	repo.entryStage = &stage.EntryStage{StageID: "s-a"}

	uc := NewAssignEntryStageUseCase(repo, nil)
	_, err := uc.Execute("ws", stage.AssignEntryStageInput{
		StageID: "s-other", EntryID: "e-1", EntryType: "whatsapp",
		AllowCrossPipeline: true,
	})
	if !errors.Is(err, stage.ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
	if len(repo.assigned) != 0 {
		t.Fatal("a cross-tenant move must never write")
	}
}

// An invalid entry type is still rejected with the flag set: the escape hatch
// comes after validation, not instead of it.
func TestCrossFunnelMoveStillValidatesTheEntryType(t *testing.T) {
	repo := crossFunnelRepo()
	uc := NewAssignEntryStageUseCase(repo, nil)

	if _, err := uc.Execute("ws", stage.AssignEntryStageInput{
		StageID: "s-b", EntryID: "e-1", EntryType: "carrier-pigeon",
		AllowCrossPipeline: true,
	}); err == nil {
		t.Fatal("an invalid entry type must still be rejected")
	}
	if len(repo.assigned) != 0 {
		t.Fatal("an invalid move must not write")
	}
}

// A move WITHIN one funnel is unaffected by the flag, so a client that sets it
// unconditionally does not change ordinary behaviour.
func TestCrossFunnelFlagDoesNotChangeASameFunnelMove(t *testing.T) {
	repo := newCoherenceRepo()
	repo.stages["s-1"] = &stage.Stage{ID: "s-1", WorkspaceID: "ws", PipelineID: "pipe-a"}
	repo.stages["s-2"] = &stage.Stage{ID: "s-2", WorkspaceID: "ws", PipelineID: "pipe-a"}
	repo.entryStage = &stage.EntryStage{StageID: "s-1"}

	uc := NewAssignEntryStageUseCase(repo, nil)
	if _, err := uc.Execute("ws", stage.AssignEntryStageInput{
		StageID: "s-2", EntryID: "e-1", EntryType: "whatsapp",
		AllowCrossPipeline: true,
	}); err != nil {
		t.Fatalf("same-funnel move: %v", err)
	}
	if len(repo.assigned) != 1 {
		t.Fatal("expected the move to be applied")
	}
}
