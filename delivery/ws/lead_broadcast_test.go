package ws

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/domain/shared"
)

type leadReadAuthorizer struct {
	conversation.ConversationAuthorizer
	readers map[string]bool
}

func (a leadReadAuthorizer) HasWorkspacePermission(userID, _, resource, action string, _ bool) bool {
	return resource == "leads" && action == "read" && a.readers[userID]
}

type leadEntries map[string][]shared.EntryRef

func (e leadEntries) EntryRefs(_ context.Context, _, leadID string) ([]shared.EntryRef, error) {
	return e[leadID], nil
}

type capturingSharedState struct {
	eligibilityFakeSharedState
	published [][]byte
}

func (s *capturingSharedState) Publish(_ string, data []byte) error {
	s.published = append(s.published, data)
	return nil
}

func leadHub(t *testing.T, readers map[string]bool, state *capturingSharedState) *ConversationHub {
	t.Helper()
	hub := NewConversationHub(leadReadAuthorizer{readers: readers}, nil, nil, state, "test-replica", "")
	for _, c := range []struct{ id, user, workspace string }{
		{"c-reader", "u-reader", "ws-1"},
		{"c-subscriber", "u-subscriber", "ws-1"},
		{"c-both", "u-both", "ws-1"},
		{"c-bystander", "u-bystander", "ws-1"},
		{"c-foreign", "u-reader", "ws-2"},
	} {
		hub.connections[c.id] = &WSConnection{ID: c.id, UserID: c.user, WorkspaceID: c.workspace, Send: make(chan []byte, 4), Done: make(chan struct{})}
	}
	sub := entrySubscription{entryID: "e-1", entryType: "whatsapp"}
	hub.entrySubscribers[sub] = map[string]bool{"c-subscriber": true, "c-both": true, "c-foreign": true}
	return hub
}

func drained(conn *WSConnection) []string {
	var got []string
	for {
		select {
		case data := <-conn.Send:
			got = append(got, string(data))
		default:
			return got
		}
	}
}

func TestLeadChangeReachesReadersAndTheLeadConversationSubscribersOnce(t *testing.T) {
	state := &capturingSharedState{}
	hub := leadHub(t, map[string]bool{"u-reader": true, "u-both": true}, state)
	notifier := NewLeadChangeNotifier(hub, leadEntries{"l-1": {{EntryID: "e-1", EntryType: shared.EntryTypeWhatsApp}}})

	notifier.publish(lead.Change{WorkspaceID: "ws-1", LeadID: "l-1", Version: 5, Fields: []string{"name", "owner"}})

	for id, want := range map[string]int{"c-reader": 1, "c-subscriber": 1, "c-both": 1, "c-bystander": 0, "c-foreign": 0} {
		got := drained(hub.connections[id])
		if len(got) != want {
			t.Fatalf("%s received %d messages, want %d", id, len(got), want)
		}
		if want == 0 {
			continue
		}
		var msg struct {
			Type    string            `json:"type"`
			Payload LeadUpdatePayload `json:"payload"`
		}
		if err := json.Unmarshal([]byte(got[0]), &msg); err != nil {
			t.Fatalf("payload: %v", err)
		}
		if msg.Type != "conversation:lead_update" || msg.Payload.LeadID != "l-1" || msg.Payload.Version != 5 || strings.Join(msg.Payload.Fields, ",") != "name,owner" {
			t.Fatalf("message = %+v", msg)
		}
	}
	if len(state.published) != 1 {
		t.Fatalf("the change must be published to the other replicas once, got %d", len(state.published))
	}
}

func TestLeadChangeCarriesFieldNamesNeverValues(t *testing.T) {
	hub := leadHub(t, map[string]bool{"u-reader": true}, &capturingSharedState{})
	NewLeadChangeNotifier(hub, leadEntries{}).publish(lead.Change{WorkspaceID: "ws-1", LeadID: "l-1", Version: 2, Fields: []string{"email"}})
	got := drained(hub.connections["c-reader"])
	if len(got) != 1 {
		t.Fatalf("messages = %v", got)
	}
	var raw map[string]map[string]any
	_ = json.Unmarshal([]byte(got[0]), &raw)
	for key := range raw["payload"] {
		if key != "leadId" && key != "version" && key != "fields" {
			t.Fatalf("the payload carries %q, only ids, version and field names are allowed", key)
		}
	}
}

func TestALeadChangeFromAnotherReplicaIsDeliveredLocally(t *testing.T) {
	state := &capturingSharedState{}
	origin := leadHub(t, map[string]bool{"u-reader": true}, state)
	NewLeadChangeNotifier(origin, leadEntries{"l-1": {{EntryID: "e-1", EntryType: shared.EntryTypeWhatsApp}}}).
		publish(lead.Change{WorkspaceID: "ws-1", LeadID: "l-1", Version: 3, Fields: []string{"name"}})

	var envelope redisWorkspaceBroadcast
	if err := json.Unmarshal(state.published[0], &envelope); err != nil || envelope.Type != "lead_update" {
		t.Fatalf("envelope = %+v, %v", envelope, err)
	}
	other := leadHub(t, map[string]bool{"u-reader": true}, &capturingSharedState{})
	other.deliverWorkspaceBroadcast(envelope)
	if len(drained(other.connections["c-reader"])) != 1 || len(drained(other.connections["c-subscriber"])) != 1 {
		t.Fatal("the replica must deliver the change to its readers and subscribers")
	}
	if len(drained(other.connections["c-bystander"])) != 0 {
		t.Fatal("a member without access must not receive it")
	}
}
