package label_usecase

import (
	"testing"

	"vozko/domain/actor"
	ce "vozko/domain/conversation_event"
	"vozko/domain/label"
)

type fakeLabelRepo struct {
	label.Repository

	labels   map[string]*label.Label
	assigned []*label.EntryLabel
	removed  []string
}

func (r *fakeLabelRepo) FindByID(id string) (*label.Label, error) { return r.labels[id], nil }

func (r *fakeLabelRepo) AssignLabel(el *label.EntryLabel) error {
	r.assigned = append(r.assigned, el)
	return nil
}

func (r *fakeLabelRepo) RemoveLabel(labelID, entryID, entryType, workspaceID string) error {
	r.removed = append(r.removed, labelID)
	return nil
}

func (r *fakeLabelRepo) GetEntryLabels(entryID, entryType, workspaceID string) ([]*label.EntryLabel, error) {
	return r.assigned, nil
}

func repoWith(labels ...*label.Label) *fakeLabelRepo {
	m := map[string]*label.Label{}
	for _, l := range labels {
		m[l.ID] = l
	}
	return &fakeLabelRepo{labels: m}
}

type recordingLogger struct{ events []*ce.ConversationEvent }

func (l *recordingLogger) Log(e *ce.ConversationEvent) { l.events = append(l.events, e) }

func TestAssignEntryLabel_RecordsTheLabelOnTheTimeline(t *testing.T) {
	repo := repoWith(&label.Label{ID: "l1", WorkspaceID: "ws", Name: "urgente"})
	log := &recordingLogger{}

	uc := NewAssignEntryLabelUseCase(repo, log)
	if _, err := uc.Execute("ws", label.AssignEntryLabelInput{
		LabelID: "l1", EntryID: "e1", EntryType: "telegram", ActorID: "user-1",
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(log.events) != 1 {
		t.Fatalf("recorded %d events, want 1", len(log.events))
	}
	ev := log.events[0]
	if ev.EventType != ce.EventLabelAdded {
		t.Fatalf("EventType = %s, want label_added", ev.EventType)
	}
	if ev.ActorID != "user-1" || ev.ActorKind != actor.KindHuman {
		t.Fatalf("actor = %s/%s, want user-1/human", ev.ActorKind, ev.ActorID)
	}
	if ev.Channel != "telegram" {
		t.Fatalf("Channel = %q, want telegram", ev.Channel)
	}
	if got := ev.DetailsMap()["label_name"]; got != "urgente" {
		t.Fatalf("label_name = %q, want urgente", got)
	}
}

func TestRemoveEntryLabel_RecordsTheRemoval(t *testing.T) {
	repo := repoWith(&label.Label{ID: "l1", WorkspaceID: "ws", Name: "urgente"})
	log := &recordingLogger{}

	uc := NewRemoveEntryLabelUseCase(repo, log)
	if err := uc.Execute("ws", label.RemoveEntryLabelInput{
		LabelID: "l1", EntryID: "e1", EntryType: "whatsapp", ActorID: "user-1",
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(log.events) != 1 || log.events[0].EventType != ce.EventLabelRemoved {
		t.Fatalf("events = %+v, want one label_removed", log.events)
	}
	if got := log.events[0].DetailsMap()["label_name"]; got != "urgente" {
		t.Fatalf("label_name = %q, want urgente", got)
	}
}

func TestLabelUseCases_RecordNothingWhenRejected(t *testing.T) {
	repo := repoWith(&label.Label{ID: "l1", WorkspaceID: "other-ws", Name: "urgente"})

	addLog := &recordingLogger{}
	if _, err := NewAssignEntryLabelUseCase(repo, addLog).Execute("ws", label.AssignEntryLabelInput{
		LabelID: "l1", EntryID: "e1", EntryType: "whatsapp", ActorID: "user-1",
	}); err == nil {
		t.Fatal("expected the cross-workspace assign to be rejected")
	}
	if len(addLog.events) != 0 {
		t.Fatalf("recorded %d events for a rejected assign", len(addLog.events))
	}

	remLog := &recordingLogger{}
	if err := NewRemoveEntryLabelUseCase(repo, remLog).Execute("ws", label.RemoveEntryLabelInput{
		LabelID: "l1", EntryID: "e1", EntryType: "whatsapp", ActorID: "user-1",
	}); err == nil {
		t.Fatal("expected the cross-workspace remove to be rejected")
	}
	if len(remLog.events) != 0 {
		t.Fatalf("recorded %d events for a rejected remove", len(remLog.events))
	}
}

func TestLabelUseCases_SurviveWithoutALogger(t *testing.T) {
	repo := repoWith(&label.Label{ID: "l1", WorkspaceID: "ws", Name: "urgente"})
	if _, err := NewAssignEntryLabelUseCase(repo, nil).Execute("ws", label.AssignEntryLabelInput{
		LabelID: "l1", EntryID: "e1", EntryType: "whatsapp",
	}); err != nil {
		t.Fatalf("assign: %v", err)
	}
	if err := NewRemoveEntryLabelUseCase(repo, nil).Execute("ws", label.RemoveEntryLabelInput{
		LabelID: "l1", EntryID: "e1", EntryType: "whatsapp",
	}); err != nil {
		t.Fatalf("remove: %v", err)
	}
}
