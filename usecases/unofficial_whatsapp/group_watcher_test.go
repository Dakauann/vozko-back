package unofficial_whatsapp

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	uw "vozko/domain/unofficial_whatsapp"
)

type recordingWatcher struct {
	mu       sync.Mutex
	messages []string
	changed  []string
	changes  []uw.GroupParticipantChange
	seats    []*uw.Group
}

func (w *recordingWatcher) GroupMessage(_ context.Context, _ *uw.Instance, groupJID string, hasText bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	mark := "|silent"
	if hasText {
		mark = "|text"
	}
	w.messages = append(w.messages, groupJID+mark)
}

func (w *recordingWatcher) GroupChanged(_ context.Context, _ *uw.Instance, groupJID string, change uw.GroupParticipantChange) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.changed = append(w.changed, groupJID)
	w.changes = append(w.changes, change)
}

func (w *recordingWatcher) SeatSynced(_ context.Context, _ *uw.Instance, seat *uw.Group) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.seats = append(w.seats, seat)
}

func TestAGroupMessageIsReportedToTheGroupWatcherWithItsSeat(t *testing.T) {
	h := newGroupHarness(t, false)
	watcher := &recordingWatcher{}
	h.uc.SetGroupWatcher(watcher)

	h.deliver(t, groupMessage("5511111111111", "Bom dia"))

	if len(watcher.messages) != 1 || watcher.messages[0] != "120363012345678901@g.us|text" {
		t.Fatalf("messages = %v, want the group message reported with text", watcher.messages)
	}
	if len(watcher.seats) != 1 || len(watcher.seats[0].Participants) != 2 {
		t.Fatalf("seats = %+v, want the synced seat with its roster", watcher.seats)
	}
}

func TestAPrivateMessageIsNotReportedToTheGroupWatcher(t *testing.T) {
	h := newGroupHarness(t, false)
	watcher := &recordingWatcher{}
	h.uc.SetGroupWatcher(watcher)

	h.deliver(t, map[string]any{
		"messageid": "private-1", "chatid": "5511111111111@s.whatsapp.net",
		"sender": "5511111111111@s.whatsapp.net", "messageType": "text", "text": "Oi",
		"messageTimestamp": time.Now().UnixMilli(),
	})

	if len(watcher.messages) != 0 || len(watcher.seats) != 0 {
		t.Fatalf("a private chat reached the group watcher: %v %v", watcher.messages, watcher.seats)
	}
}

func TestAGroupChangeIsReportedToTheGroupWatcher(t *testing.T) {
	h := newGroupHarness(t, false)
	watcher := &recordingWatcher{}
	h.uc.SetGroupWatcher(watcher)

	body, _ := json.Marshal(map[string]any{"event": "groups", "instance": "prov-1", "data": map[string]any{"groupjid": "120363012345678901@g.us"}})
	if err := h.uc.Execute(context.Background(), &QueuedEvent{InstanceID: "inst-1", Body: body}); err != nil {
		t.Fatal(err)
	}

	if len(watcher.changed) != 1 || watcher.changed[0] != "120363012345678901@g.us" {
		t.Fatalf("changed = %v", watcher.changed)
	}
}

func TestWithoutAWatcherGroupMessagesBehaveAsBefore(t *testing.T) {
	h := newGroupHarness(t, false)
	h.deliver(t, groupMessage("5511111111111", "Bom dia"))
	if len(h.history.all()) != 1 {
		t.Fatalf("records = %d, want the message stored as before", len(h.history.all()))
	}
}

func TestAGroupChangeHandsTheDeparturesAndTheActorToTheWatcher(t *testing.T) {
	h := newGroupHarness(t, false)
	watcher := &recordingWatcher{}
	h.uc.SetGroupWatcher(watcher)

	body := []byte(`{"EventType": "groups", "instance": "prov-1", "type": "Group",
		"event": {"JID": "120363012345678901@g.us", "Sender": "5511222222222@s.whatsapp.net", "Leave": ["5511111111111@s.whatsapp.net"]}}`)
	if err := h.uc.Execute(context.Background(), &QueuedEvent{InstanceID: "inst-1", Body: body}); err != nil {
		t.Fatal(err)
	}

	if len(watcher.changes) != 1 || watcher.changed[0] != "120363012345678901@g.us" {
		t.Fatalf("changed = %v", watcher.changed)
	}
	change := watcher.changes[0]
	if len(change.Left) != 1 || change.Left[0] != "5511111111111@s.whatsapp.net" || len(change.Actors) != 1 || change.Actors[0] != "5511222222222@s.whatsapp.net" {
		t.Fatalf("change = %+v, want who left and who acted", change)
	}
}
