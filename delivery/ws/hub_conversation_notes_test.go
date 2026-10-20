package ws

import (
	"encoding/json"
	"testing"
	"time"

	"vozko/domain/conversationnote"
	"vozko/domain/shared"
)

func noteHub(t *testing.T, state *capturingSharedState) *ConversationHub {
	t.Helper()
	hub := NewConversationHub(&hubDepartmentTestAuthorizer{}, nil, nil, state, "test-replica", "")
	for _, c := range []struct{ id, user string }{
		{"c-viewer", "u-viewer"},
		{"c-viewer-popout", "u-viewer"},
		{"c-elsewhere", "u-elsewhere"},
	} {
		hub.connections[c.id] = &WSConnection{ID: c.id, UserID: c.user, WorkspaceID: "ws-1", Send: make(chan []byte, 4), Done: make(chan struct{})}
	}
	hub.entrySubscribers[entrySubscription{entryID: "e-1", entryType: "whatsapp"}] = map[string]bool{"c-viewer": true, "c-viewer-popout": true}
	hub.entrySubscribers[entrySubscription{entryID: "e-2", entryType: "whatsapp"}] = map[string]bool{"c-elsewhere": true}
	return hub
}

func deliverQueuedBroadcast(t *testing.T, hub *ConversationHub) {
	t.Helper()
	select {
	case msg := <-hub.broadcast:
		hub.handleBroadcast(msg)
	case <-time.After(2 * time.Second):
		t.Fatal("nothing was queued for broadcast")
	}
}

func TestANewNoteReachesOnlyTheConversationSubscribersAndOtherReplicas(t *testing.T) {
	state := &capturingSharedState{}
	hub := noteHub(t, state)
	note := conversationnote.Authored{
		Note: conversationnote.Note{
			ID: "note-1", WorkspaceID: "ws-1", EntryType: shared.EntryTypeWhatsApp, EntryID: "e-1",
			AuthorID: "u-viewer", Body: "interno", Mentions: []string{}, CreatedAt: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC),
		},
		AuthorName: "Carla",
	}

	hub.AnnounceNote(note)
	deliverQueuedBroadcast(t, hub)

	for id, want := range map[string]int{"c-viewer": 1, "c-viewer-popout": 1, "c-elsewhere": 0} {
		got := drained(hub.connections[id])
		if len(got) != want {
			t.Fatalf("%s received %d messages, want %d", id, len(got), want)
		}
		if want == 0 {
			continue
		}
		var msg struct {
			Type    WSEventType `json:"type"`
			Payload struct {
				EntryID   string         `json:"entry_id"`
				EntryType string         `json:"entry_type"`
				Note      map[string]any `json:"note"`
			} `json:"payload"`
		}
		if err := json.Unmarshal([]byte(got[0]), &msg); err != nil {
			t.Fatal(err)
		}
		if msg.Type != WSEventNote || msg.Payload.EntryID != "e-1" || msg.Payload.EntryType != "whatsapp" {
			t.Fatalf("message = %+v", msg)
		}
		if msg.Payload.Note["id"] != "note-1" || msg.Payload.Note["author_name"] != "Carla" || msg.Payload.Note["body"] != "interno" {
			t.Fatalf("note = %v", msg.Payload.Note)
		}
	}
	if len(state.published) != 1 {
		t.Fatalf("published to other replicas %d times, want 1", len(state.published))
	}
}

func TestADeletedNoteIsAnnouncedByIdToTheConversationSubscribersOnly(t *testing.T) {
	hub := noteHub(t, &capturingSharedState{})

	hub.AnnounceNoteDeleted(conversationnote.Note{ID: "note-1", EntryType: shared.EntryTypeWhatsApp, EntryID: "e-1", Body: "segredo"})
	deliverQueuedBroadcast(t, hub)

	if got := drained(hub.connections["c-elsewhere"]); len(got) != 0 {
		t.Fatalf("a non-subscriber received %v", got)
	}
	got := drained(hub.connections["c-viewer"])
	if len(got) != 1 {
		t.Fatalf("subscriber received %d messages, want 1", len(got))
	}
	var msg struct {
		Type    WSEventType       `json:"type"`
		Payload map[string]string `json:"payload"`
	}
	if err := json.Unmarshal([]byte(got[0]), &msg); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"entry_id": "e-1", "entry_type": "whatsapp", "note_id": "note-1"}
	if msg.Type != WSEventNoteDeleted || len(msg.Payload) != len(want) {
		t.Fatalf("message = %+v; a deletion carries only the ids", msg)
	}
	for k, v := range want {
		if msg.Payload[k] != v {
			t.Fatalf("%s = %q, want %q", k, msg.Payload[k], v)
		}
	}
}
