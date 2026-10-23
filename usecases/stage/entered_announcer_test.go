package stage_usecase

import (
	"errors"
	"testing"

	"vozko/domain/shared"
	"vozko/domain/stage"
)

type placementRepo struct {
	stage.Repository
	current   map[string]string
	failMoves bool
}

func newPlacementRepo() *placementRepo {
	return &placementRepo{current: map[string]string{}}
}

func (r *placementRepo) AssignStage(et *stage.EntryStage) (string, error) {
	if r.failMoves {
		return "", errors.New("database down")
	}
	previous := r.current[et.EntryID]
	r.current[et.EntryID] = et.StageID
	return previous, nil
}

func (r *placementRepo) AssignStageIfNone(et *stage.EntryStage) (bool, error) {
	if _, has := r.current[et.EntryID]; has {
		return false, nil
	}
	r.current[et.EntryID] = et.StageID
	return true, nil
}

type heardEntries struct{ entered []stage.Entered }

func (h *heardEntries) StageEntered(e stage.Entered) { h.entered = append(h.entered, e) }

func placement(id, stageID string) *stage.EntryStage {
	return &stage.EntryStage{ID: id, StageID: stageID, EntryID: "conv-1", EntryType: "whatsapp", WorkspaceID: "ws1"}
}

func TestEveryPersistedStageChangeIsAnnouncedOnce(t *testing.T) {
	repo := newPlacementRepo()
	heard := &heardEntries{}
	announced := AnnounceEntries(repo, heard)

	if assigned, err := announced.AssignStageIfNone(placement("row-1", "inicial")); err != nil || !assigned {
		t.Fatalf("initial placement: assigned=%v err=%v", assigned, err)
	}
	if assigned, _ := announced.AssignStageIfNone(placement("row-2", "inicial")); assigned {
		t.Fatal("a conversation with a stage keeps it")
	}
	moved := placement("row-3", "qualificado")
	moved.Cause = stage.MoveCause{WorkflowID: "wf-a", RunID: "run-1", Chain: 1}
	if _, err := announced.AssignStage(moved); err != nil {
		t.Fatal(err)
	}
	if _, err := announced.AssignStage(placement("row-4", "qualificado")); err != nil {
		t.Fatal(err)
	}

	want := []stage.Entered{
		{MoveID: "row-1", WorkspaceID: "ws1", EntryID: "conv-1", EntryType: "whatsapp", StageID: "inicial"},
		{MoveID: "row-3", WorkspaceID: "ws1", EntryID: "conv-1", EntryType: "whatsapp", StageID: "qualificado", FromStageID: "inicial", Cause: moved.Cause},
	}
	if len(heard.entered) != len(want) {
		t.Fatalf("heard %+v, want %+v", heard.entered, want)
	}
	for i := range want {
		if heard.entered[i] != want[i] {
			t.Fatalf("entry %d = %+v, want %+v", i, heard.entered[i], want[i])
		}
	}
}

func TestAFailedMoveAnnouncesNothing(t *testing.T) {
	repo := newPlacementRepo()
	repo.failMoves = true
	heard := &heardEntries{}
	if _, err := AnnounceEntries(repo, heard).AssignStage(placement("row-1", "qualificado")); err == nil {
		t.Fatal("the failure must reach the caller")
	}
	if len(heard.entered) != 0 {
		t.Fatalf("heard %+v after a failed move", heard.entered)
	}
}

func TestAnAssignmentCarriesItsCauseToThePlacement(t *testing.T) {
	repo := &coherenceRepo{stages: map[string]*stage.Stage{"s2": {ID: "s2", WorkspaceID: "ws1"}}}
	heard := &heardEntries{}
	uc := NewAssignEntryStageUseCase(AnnounceEntries(repo, heard), nil)
	cause := stage.MoveCause{WorkflowID: "wf-a", RunID: "run-1", Chain: 2}
	if _, err := uc.Execute("ws1", stage.AssignEntryStageInput{StageID: "s2", EntryID: "conv-1", EntryType: "whatsapp", Cause: cause}); err != nil {
		t.Fatal(err)
	}
	if len(heard.entered) != 1 || heard.entered[0].Cause != cause || heard.entered[0].StageID != "s2" {
		t.Fatalf("heard %+v, want the move with its cause", heard.entered)
	}
}

func TestTheInitialStageOfANewConversationIsAnnounced(t *testing.T) {
	heard := &heardEntries{}
	AutoAssignInitialStage(AnnounceEntries(&initialStageRepo{}, heard), "ws", "camp", "unofficial_whatsapp", "entry", "unofficial_whatsapp")
	if len(heard.entered) != 1 || heard.entered[0].StageID != "recebido" || heard.entered[0].FromStageID != "" || heard.entered[0].Cause != (stage.MoveCause{}) {
		t.Fatalf("heard %+v, want the initial stage as a person-free entry", heard.entered)
	}
}

func TestAnInitialStageThatLostTheRaceIsNotAnnounced(t *testing.T) {
	heard := &heardEntries{}
	AutoAssignInitialStage(AnnounceEntries(&initialStageRepo{stageAppeared: true}, heard), "ws", "camp", "unofficial_whatsapp", "entry", "unofficial_whatsapp")
	if len(heard.entered) != 0 {
		t.Fatalf("heard %+v, the conversation already had a stage", heard.entered)
	}
}

func TestAnOperatorMoveIsAnnouncedWithoutACause(t *testing.T) {
	repo := &coherenceRepo{stages: map[string]*stage.Stage{"s1": {ID: "s1", WorkspaceID: "ws1"}}}
	heard := &heardEntries{}
	move := NewMoveEntryStageUseCase(&entryAccessStub{allow: true}, NewAssignEntryStageUseCase(AnnounceEntries(repo, heard), nil))
	in := moveInput("whatsapp")
	in.Cause = stage.MoveCause{WorkflowID: "forged"}
	if _, err := move.Execute("ws1", shared.Person{UserID: "u1"}, in); err != nil {
		t.Fatal(err)
	}
	if len(heard.entered) != 1 || heard.entered[0].StageID != "s1" {
		t.Fatalf("heard %+v", heard.entered)
	}
	if heard.entered[0].Cause != (stage.MoveCause{}) {
		t.Fatalf("cause = %+v, a person's move never claims to come from a workflow", heard.entered[0].Cause)
	}
}
