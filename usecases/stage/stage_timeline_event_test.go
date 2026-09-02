package stage_usecase

import (
	"testing"

	"vozko/domain/actor"
	ce "vozko/domain/conversation_event"
	"vozko/domain/stage"
)

// The stage move's timeline event belongs to the USE CASE.
//
// It used to be written by the stage HTTP handler, which meant the two other
// writers that reach this use case directly — the CRM's bulk "move to stage"
// and the AI's manage_entry_stage tool — changed the board and left the
// conversation's history blank. These cases pin it where every writer passes.

type recordingLogger struct{ events []*ce.ConversationEvent }

func (l *recordingLogger) Log(e *ce.ConversationEvent) { l.events = append(l.events, e) }

func (l *recordingLogger) ofType(t ce.EventType) *ce.ConversationEvent {
	for _, e := range l.events {
		if e.EventType == t {
			return e
		}
	}
	return nil
}

// RemoveStage completes the shared fake for the removal path; the coherence
// tests only ever exercised assignment.
func (r *coherenceRepo) RemoveStage(stageID, entryID, entryType, workspaceID string) error {
	r.entryStage = nil
	return nil
}

func stageRepoWith(stages ...*stage.Stage) *coherenceRepo {
	repo := newCoherenceRepo()
	for _, s := range stages {
		repo.stages[s.ID] = s
	}
	return repo
}

func TestAssignEntryStage_RecordsTheMoveOnTheTimeline(t *testing.T) {
	repo := stageRepoWith(
		&stage.Stage{ID: "s-new", WorkspaceID: "ws", Name: "em atendimento"},
		&stage.Stage{ID: "s-old", WorkspaceID: "ws", Name: "recebido"},
	)
	repo.entryStage = &stage.EntryStage{EntryID: "e1", StageID: "s-old"}
	log := &recordingLogger{}

	uc := NewAssignEntryStageUseCase(repo, log)
	if _, err := uc.Execute("ws", stage.AssignEntryStageInput{
		StageID:   "s-new",
		EntryID:   "e1",
		EntryType: "unofficial_whatsapp",
		ActorID:   "user-1",
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	ev := log.ofType(ce.EventStageChanged)
	if ev == nil {
		t.Fatal("no stage_changed event was recorded")
	}
	if ev.ActorID != "user-1" || ev.ActorKind != actor.KindHuman {
		t.Fatalf("actor = %s/%s, want user-1/human", ev.ActorKind, ev.ActorID)
	}
	// The channel used to default to "whatsapp" for every non-voice type.
	if ev.Channel != "unofficial_whatsapp" {
		t.Fatalf("Channel = %q, want unofficial_whatsapp", ev.Channel)
	}

	// Names, not just ids: the row rendered as a bare "Stage changed" while the
	// details held nothing but uuids.
	d := ev.DetailsMap()
	if d["stage_name"] != "em atendimento" || d["from_stage_name"] != "recebido" {
		t.Fatalf("details = %v, want the move named both ways", d)
	}
	if d["to_stage_id"] != "s-new" || d["from_stage_id"] != "s-old" {
		t.Fatalf("details = %v, want both stage ids", d)
	}
}

// tag_added rides along for consumers that predate the stage rename.
func TestAssignEntryStage_AlsoRecordsTagAdded(t *testing.T) {
	repo := stageRepoWith(&stage.Stage{ID: "s1", WorkspaceID: "ws", Name: "novo"})
	log := &recordingLogger{}

	uc := NewAssignEntryStageUseCase(repo, log)
	if _, err := uc.Execute("ws", stage.AssignEntryStageInput{
		StageID: "s1", EntryID: "e1", EntryType: "whatsapp", ActorID: "user-1",
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if log.ofType(ce.EventTagAdded) == nil {
		t.Fatal("no tag_added event was recorded")
	}
}

// The AI moves leads too; the timeline has to say so rather than filing it
// under a human or the system.
func TestAssignEntryStage_AttributesAnAIMoveToTheAgent(t *testing.T) {
	repo := stageRepoWith(&stage.Stage{ID: "s1", WorkspaceID: "ws", Name: "interessado"})
	log := &recordingLogger{}

	uc := NewAssignEntryStageUseCase(repo, log)
	if _, err := uc.Execute("ws", stage.AssignEntryStageInput{
		StageID: "s1", EntryID: "e1", EntryType: "whatsapp",
		ActorID: actor.FormatAI("agent-9"),
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	ev := log.ofType(ce.EventStageChanged)
	if ev.ActorKind != actor.KindAI || ev.ActorID != "ai:agent-9" {
		t.Fatalf("actor = %s/%s, want ai/ai:agent-9", ev.ActorKind, ev.ActorID)
	}
}

// An unattributed call is the platform acting, not a human with an empty id.
func TestAssignEntryStage_FallsBackToTheSystemActor(t *testing.T) {
	repo := stageRepoWith(&stage.Stage{ID: "s1", WorkspaceID: "ws", Name: "novo"})
	log := &recordingLogger{}

	uc := NewAssignEntryStageUseCase(repo, log)
	if _, err := uc.Execute("ws", stage.AssignEntryStageInput{
		StageID: "s1", EntryID: "e1", EntryType: "whatsapp",
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	ev := log.ofType(ce.EventStageChanged)
	if ev.ActorKind != actor.KindSystem || ev.ActorID != actor.SystemID {
		t.Fatalf("actor = %s/%s, want system", ev.ActorKind, ev.ActorID)
	}
}

// A first placement has no previous stage to name, and must not invent one.
func TestAssignEntryStage_OmitsTheFromSideOnFirstPlacement(t *testing.T) {
	repo := stageRepoWith(&stage.Stage{ID: "s1", WorkspaceID: "ws", Name: "novo"})
	log := &recordingLogger{}

	uc := NewAssignEntryStageUseCase(repo, log)
	if _, err := uc.Execute("ws", stage.AssignEntryStageInput{
		StageID: "s1", EntryID: "e1", EntryType: "whatsapp", ActorID: "user-1",
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	d := log.ofType(ce.EventStageChanged).DetailsMap()
	if _, ok := d["from_stage_id"]; ok {
		t.Fatalf("details = %v, want no from side", d)
	}
}

// A nil logger is the unit-test wiring; it must not panic a real move.
func TestAssignEntryStage_SurvivesWithoutALogger(t *testing.T) {
	repo := stageRepoWith(&stage.Stage{ID: "s1", WorkspaceID: "ws", Name: "novo"})
	uc := NewAssignEntryStageUseCase(repo, nil)
	if _, err := uc.Execute("ws", stage.AssignEntryStageInput{
		StageID: "s1", EntryID: "e1", EntryType: "whatsapp",
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestRemoveEntryStage_RecordsTheRemoval(t *testing.T) {
	repo := stageRepoWith(&stage.Stage{ID: "s1", WorkspaceID: "ws", Name: "recebido"})
	log := &recordingLogger{}

	uc := NewRemoveEntryStageUseCase(repo, log)
	if err := uc.Execute("ws", stage.RemoveEntryStageInput{
		StageID: "s1", EntryID: "e1", EntryType: "whatsapp", ActorID: "user-1",
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	ev := log.ofType(ce.EventTagRemoved)
	if ev == nil {
		t.Fatal("no tag_removed event was recorded")
	}
	if ev.DetailsMap()["stage_name"] != "recebido" {
		t.Fatalf("details = %v, want the stage named", ev.DetailsMap())
	}
}

// A rejected move must leave no trace: the entry did not change stage.
func TestAssignEntryStage_RecordsNothingWhenTheMoveIsRejected(t *testing.T) {
	repo := stageRepoWith(&stage.Stage{ID: "s1", WorkspaceID: "other-ws", Name: "novo"})
	log := &recordingLogger{}

	uc := NewAssignEntryStageUseCase(repo, log)
	if _, err := uc.Execute("ws", stage.AssignEntryStageInput{
		StageID: "s1", EntryID: "e1", EntryType: "whatsapp", ActorID: "user-1",
	}); err == nil {
		t.Fatal("expected the cross-workspace move to be rejected")
	}
	if len(log.events) != 0 {
		t.Fatalf("recorded %d events for a rejected move", len(log.events))
	}
}
