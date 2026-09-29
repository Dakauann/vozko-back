package ws

import (
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"vozko/domain/conversation"
)

type countingHistory struct {
	*statusTestHistoryProvider
	builds atomic.Int32
}

func (c *countingHistory) GetInboxEntry(entryID, entryType string) (*conversation.InboxEntry, error) {
	c.builds.Add(1)
	return c.statusTestHistoryProvider.GetInboxEntry(entryID, entryType)
}

type fixedWorkspace struct {
	conversation.CampaignWorkspaceResolver
	workspaceID string
}

func (f fixedWorkspace) GetEntryWorkspaceID(string, string) (string, error) { return f.workspaceID, nil }
func (f fixedWorkspace) GetEntryCampaignID(string, string) (string, error)  { return "", nil }

func refreshHub(t *testing.T) (*ConversationHub, *WSConnection, *countingHistory) {
	t.Helper()
	authorizer := &hubDepartmentTestAuthorizer{
		entryAccess: map[string]bool{"viewer": true},
		viewOthers:  map[string]bool{"viewer": true},
	}
	hub := NewConversationHub(authorizer, nil, nil, nil, "test-replica", "")
	hub.refreshWindow = 20 * time.Millisecond
	history := &countingHistory{statusTestHistoryProvider: &statusTestHistoryProvider{
		entries: map[string]*conversation.InboxEntry{
			"entry-1|whatsapp": {EntryID: "entry-1", EntryType: "whatsapp", ConversationStatus: conversation.ConversationStatusNew},
		},
	}}
	hub.historyProvider = history
	hub.workspaceResolver = fixedWorkspace{workspaceID: "ws-1"}
	viewer := &WSConnection{ID: "conn-viewer", UserID: "viewer", WorkspaceID: "ws-1", Send: make(chan []byte, 8)}
	hub.connections[viewer.ID] = viewer
	hub.userConnections[viewer.UserID] = map[string]bool{viewer.ID: true}
	return hub, viewer, history
}

func awaitEntryUpdate(t *testing.T, conn *WSConnection) EntryUpdatePayload {
	t.Helper()
	select {
	case raw := <-conn.Send:
		var env wsEventEnvelope
		require.NoError(t, json.Unmarshal(raw, &env))
		require.Equal(t, WSEventEntryUpdate, env.Type)
		var payload EntryUpdatePayload
		require.NoError(t, json.Unmarshal(env.Payload, &payload))
		return payload
	case <-time.After(2 * time.Second):
		t.Fatal("no entry update arrived")
		return EntryUpdatePayload{}
	}
}

func requireQuiet(t *testing.T, conn *WSConnection) {
	t.Helper()
	select {
	case raw := <-conn.Send:
		t.Fatalf("unexpected event: %s", raw)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestARefreshReachesListViewersWithoutTheConversationOpen(t *testing.T) {
	hub, viewer, _ := refreshHub(t)

	hub.RefreshEntry("entry-1", "whatsapp")

	payload := awaitEntryUpdate(t, viewer)
	require.Equal(t, "entry-1", payload.Entry.EntryID)
	require.True(t, payload.Silent, "a refresh must not ring the new-message sound")
}

func TestAnEntryUpdateForANewMessageStillRings(t *testing.T) {
	hub, viewer, _ := refreshHub(t)

	hub.broadcastEntryUpdateLocal("entry-1", "whatsapp", nil, false)

	require.False(t, awaitEntryUpdate(t, viewer).Silent)
}

func TestAStageMoveRefreshesTheStagesTheEntryCanMoveTo(t *testing.T) {
	hub, viewer, history := refreshHub(t)
	history.entries["entry-1|whatsapp"].AvailableStages = []conversation.InboxEntryStage{{StageID: "teste-jev-agendado", Name: "agendado"}}

	hub.BroadcastStageUpdate("ws-1", "entry-1", "whatsapp")

	payload := awaitEntryUpdate(t, viewer)
	require.True(t, payload.Silent)
	require.Equal(t, "teste-jev-agendado", payload.Entry.AvailableStages[0].StageID,
		"after a move to another funnel the picker must offer that funnel's stages")
}

func TestRefreshesOfOneEntryInQuickSuccessionBuildItOnce(t *testing.T) {
	hub, viewer, history := refreshHub(t)

	hub.RefreshEntry("entry-1", "whatsapp")
	hub.RefreshEntry("entry-1", "whatsapp")
	hub.RefreshEntry("entry-1", "whatsapp")

	awaitEntryUpdate(t, viewer)
	requireQuiet(t, viewer)
	require.Equal(t, int32(1), history.builds.Load(), "a live read and a stage move from one decision must cost one rebuild")
}

func TestARefreshAfterTheWindowIsNotLost(t *testing.T) {
	hub, viewer, history := refreshHub(t)

	hub.RefreshEntry("entry-1", "whatsapp")
	awaitEntryUpdate(t, viewer)
	hub.RefreshEntry("entry-1", "whatsapp")
	awaitEntryUpdate(t, viewer)

	require.Equal(t, int32(2), history.builds.Load())
}

func TestNobodyWatchingTheWorkspaceMeansNoRebuild(t *testing.T) {
	hub, viewer, history := refreshHub(t)
	hub.workspaceResolver = fixedWorkspace{workspaceID: "ws-elsewhere"}

	hub.RefreshEntry("entry-1", "whatsapp")
	hub.broadcastEntryUpdateLocal("entry-1", "whatsapp", nil, false)

	requireQuiet(t, viewer)
	require.Equal(t, int32(0), history.builds.Load(), "a replica with no viewer in that workspace must not query the entry")
}
