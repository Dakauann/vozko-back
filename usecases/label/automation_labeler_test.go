package label_usecase

import (
	"errors"
	"testing"

	"vozko/domain/label"
)

type assignStub struct {
	got    []label.AssignEntryLabelInput
	result *label.EntryLabel
	err    error
}

func (s *assignStub) Execute(_ string, input label.AssignEntryLabelInput) (*label.EntryLabel, error) {
	s.got = append(s.got, input)
	return s.result, s.err
}

type removeRecorder struct {
	got []label.RemoveEntryLabelInput
	err error
}

func (s *removeRecorder) Execute(_ string, input label.RemoveEntryLabelInput) error {
	s.got = append(s.got, input)
	return s.err
}

type entryLabelsStub struct{ labels []*label.EntryLabel }

func (s entryLabelsStub) Execute(string, string, string) ([]*label.EntryLabel, error) {
	return s.labels, nil
}

type broadcastRecorder struct{ entries []string }

func (b *broadcastRecorder) BroadcastLabelUpdate(workspaceID, entryID, entryType string) {
	b.entries = append(b.entries, workspaceID+"|"+entryType+"|"+entryID)
}

type labelerHarness struct {
	assign  *assignStub
	remove  *removeRecorder
	current entryLabelsStub
	screens *broadcastRecorder
}

func newLabelerHarness() *labelerHarness {
	return &labelerHarness{assign: &assignStub{}, remove: &removeRecorder{}, screens: &broadcastRecorder{}}
}

func (h *labelerHarness) labeler() label.AutomationLabeler {
	return NewAutomationLabeler(h.assign, h.remove, h.current, h.screens)
}

func changeRequest(action label.LabelAction) label.LabelChangeRequest {
	return label.LabelChangeRequest{Action: action, LabelID: "vip", EntryID: "entry-1", EntryType: "instagram", ActorID: "ai:agent-1"}
}

func vipOnConversation() entryLabelsStub {
	return entryLabelsStub{labels: []*label.EntryLabel{{LabelID: "vip", LabelName: "VIP"}}}
}

func TestAnAutomationLabelsTheConversationAndRefreshesOpenScreens(t *testing.T) {
	h := newLabelerHarness()
	h.assign.result = &label.EntryLabel{LabelID: "vip", LabelName: "VIP"}

	change, err := h.labeler().Change("ws-1", changeRequest(label.LabelActionAdd))

	if err != nil || change != (label.LabelChange{LabelID: "vip", LabelName: "VIP"}) {
		t.Fatalf("change = %+v, err = %v", change, err)
	}
	want := label.AssignEntryLabelInput{LabelID: "vip", EntryID: "entry-1", EntryType: "instagram", ActorID: "ai:agent-1"}
	if len(h.assign.got) != 1 || h.assign.got[0] != want {
		t.Fatalf("the assignment must go through the shared use case with the automation as actor: %+v", h.assign.got)
	}
	if len(h.screens.entries) != 1 || h.screens.entries[0] != "ws-1|instagram|entry-1" {
		t.Fatalf("broadcasts = %v", h.screens.entries)
	}
}

func TestAddingALabelTheConversationAlreadyHasChangesNothing(t *testing.T) {
	h := newLabelerHarness()
	h.assign.err = label.ErrEntryLabelExists
	h.current = vipOnConversation()

	change, err := h.labeler().Change("ws-1", changeRequest(label.LabelActionAdd))

	if err != nil || change != (label.LabelChange{LabelID: "vip", LabelName: "VIP", Unchanged: true}) {
		t.Fatalf("change = %+v, err = %v", change, err)
	}
	if len(h.screens.entries) != 0 {
		t.Fatal("nothing changed, so nothing is broadcast")
	}
}

func TestAnAutomationRemovesALabelAndRefreshesOpenScreens(t *testing.T) {
	h := newLabelerHarness()
	h.current = vipOnConversation()

	change, err := h.labeler().Change("ws-1", changeRequest(label.LabelActionRemove))

	if err != nil || change != (label.LabelChange{LabelID: "vip", LabelName: "VIP"}) {
		t.Fatalf("change = %+v, err = %v", change, err)
	}
	want := label.RemoveEntryLabelInput{LabelID: "vip", EntryID: "entry-1", EntryType: "instagram", ActorID: "ai:agent-1"}
	if len(h.remove.got) != 1 || h.remove.got[0] != want {
		t.Fatalf("the removal must go through the shared use case: %+v", h.remove.got)
	}
	if len(h.screens.entries) != 1 {
		t.Fatalf("broadcasts = %v", h.screens.entries)
	}
}

func TestRemovingALabelTheConversationDoesNotHaveChangesNothing(t *testing.T) {
	h := newLabelerHarness()

	change, err := h.labeler().Change("ws-1", changeRequest(label.LabelActionRemove))

	if err != nil || !change.Unchanged || len(h.remove.got) != 0 || len(h.screens.entries) != 0 {
		t.Fatalf("change = %+v, err = %v, removed = %v", change, err, h.remove.got)
	}
}

func TestALabelFromAnotherWorkspaceIsRefused(t *testing.T) {
	for _, action := range []label.LabelAction{label.LabelActionAdd, label.LabelActionRemove} {
		h := newLabelerHarness()
		h.assign.err = label.ErrUnauthorized
		h.remove.err = label.ErrUnauthorized
		h.current = vipOnConversation()

		if _, err := h.labeler().Change("ws-1", changeRequest(action)); !errors.Is(err, label.ErrUnauthorized) {
			t.Fatalf("%s: err = %v", action, err)
		}
		if len(h.screens.entries) != 0 {
			t.Fatalf("%s: a refused change must not be broadcast", action)
		}
	}
}

func TestAnUnknownActionIsRefused(t *testing.T) {
	h := newLabelerHarness()
	if _, err := h.labeler().Change("ws-1", changeRequest("toggle")); !errors.Is(err, label.ErrInvalidLabelAction) {
		t.Fatalf("err = %v", err)
	}
}
