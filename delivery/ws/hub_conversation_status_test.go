package ws

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type mockStatusUpdater struct {
	statuses      map[string]conversation.ConversationStatus
	setCalls      []setStatusCall
	transitionErr error
	setErr        error
	counts        map[string]int64
	countsErr     error
}

type setStatusCall struct {
	EntryID   string
	EntryType string
	Status    conversation.ConversationStatus
}

func newMockStatusUpdater() *mockStatusUpdater {
	return &mockStatusUpdater{
		statuses: make(map[string]conversation.ConversationStatus),
		counts:   map[string]int64{"new": 0, "ongoing": 0, "finished": 0},
	}
}

func (m *mockStatusUpdater) GetConversationStatus(entryID, entryType string) conversation.ConversationStatus {
	return m.statuses[entryID+"|"+entryType]
}

func (m *mockStatusUpdater) SetConversationStatus(entryID, entryType string, status conversation.ConversationStatus) error {
	if m.setErr != nil {
		return m.setErr
	}
	m.statuses[entryID+"|"+entryType] = status
	m.setCalls = append(m.setCalls, setStatusCall{entryID, entryType, status})
	return nil
}

func (m *mockStatusUpdater) Finish(entryID, entryType string, opts conversation.FinishOptions) error {
	return m.SetConversationStatus(entryID, entryType, conversation.ConversationStatusFinished)
}

func (m *mockStatusUpdater) TransitionOnMessage(entryID, entryType string, msgType conversation.MessageType) error {
	return m.transitionErr
}

func (m *mockStatusUpdater) GetStatusCounts(workspaceID, campaignID, entryType string) (map[string]int64, error) {
	return m.counts, m.countsErr
}

type statusTestHistoryProvider struct {
	entries map[string]*conversation.InboxEntry
}

func (p *statusTestHistoryProvider) GetEntryInfo(string, string) (string, string, string, map[string]interface{}, []string, bool, error) {
	return "", "", "", nil, nil, false, nil
}
func (p *statusTestHistoryProvider) GetHistory(string, shared.EntryType, int) ([]*conversation.Message, bool, int64, error) {
	return nil, false, 0, nil
}
func (p *statusTestHistoryProvider) GetHistoryBefore(string, shared.EntryType, time.Time, int) ([]*conversation.Message, bool, error) {
	return nil, false, nil
}
func (p *statusTestHistoryProvider) GetHistoryAround(string, shared.EntryType, time.Time, int) ([]*conversation.Message, bool, bool, int64, error) {
	return nil, false, false, 0, nil
}
func (p *statusTestHistoryProvider) GetUnreadCount(string, shared.EntryType) (int64, error) {
	return 0, nil
}
func (p *statusTestHistoryProvider) GetWindowStatusForEntry(string, string) (bool, *time.Time) {
	return false, nil
}
func (p *statusTestHistoryProvider) GetInboxEntries(string, string, string, string, int, int) ([]conversation.InboxEntry, int64, error) {
	return nil, 0, nil
}
func (p *statusTestHistoryProvider) GetInboxEntry(entryID, entryType string) (*conversation.InboxEntry, error) {
	e := p.entries[entryID+"|"+entryType]
	return e, nil
}
func (p *statusTestHistoryProvider) SearchInboxEntries(conversation.SearchInboxInput) ([]conversation.InboxEntry, int64, error) {
	return nil, 0, nil
}

type wsEventEnvelope struct {
	Type    WSEventType     `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

func drainEvent(t *testing.T, conn *WSConnection) wsEventEnvelope {
	t.Helper()
	select {
	case raw := <-conn.Send:
		var env wsEventEnvelope
		require.NoError(t, json.Unmarshal(raw, &env))
		return env
	default:
		t.Fatal("expected a message on conn.Send but channel was empty")
		return wsEventEnvelope{}
	}
}

func drainErrorPayload(t *testing.T, conn *WSConnection) ErrorPayload {
	t.Helper()
	env := drainEvent(t, conn)
	require.Equal(t, WSEventError, env.Type)
	var ep ErrorPayload
	require.NoError(t, json.Unmarshal(env.Payload, &ep))
	return ep
}

func TestBroadcastEntryUpdateLocal_FiltersConnectionsByConversationStatus(t *testing.T) {
	authorizer := &hubDepartmentTestAuthorizer{
		entryAccess: map[string]bool{
			"user-all":      true,
			"user-new":      true,
			"user-ongoing":  true,
			"user-finished": true,
		},
		viewOthers: map[string]bool{
			"user-all":      true,
			"user-new":      true,
			"user-ongoing":  true,
			"user-finished": true,
		},
	}

	hub := NewConversationHub(authorizer, nil, nil, "test-replica", "")

	hub.historyProvider = &statusTestHistoryProvider{
		entries: map[string]*conversation.InboxEntry{
			"entry-1|whatsapp": {
				EntryID:            "entry-1",
				EntryType:          "whatsapp",
				ConversationStatus: conversation.ConversationStatusNew,
			},
		},
	}

	connAll := &WSConnection{ID: "conn-all", UserID: "user-all", WorkspaceID: "ws-1", ConversationStatus: "", Send: make(chan []byte, 2)}
	connNew := &WSConnection{ID: "conn-new", UserID: "user-new", WorkspaceID: "ws-1", ConversationStatus: "new", Send: make(chan []byte, 2)}
	connOngoing := &WSConnection{ID: "conn-ongoing", UserID: "user-ongoing", WorkspaceID: "ws-1", ConversationStatus: "ongoing", Send: make(chan []byte, 2)}
	connFinished := &WSConnection{ID: "conn-finished", UserID: "user-finished", WorkspaceID: "ws-1", ConversationStatus: "finished", Send: make(chan []byte, 2)}

	for _, c := range []*WSConnection{connAll, connNew, connOngoing, connFinished} {
		hub.connections[c.ID] = c
		hub.userConnections[c.UserID] = map[string]bool{c.ID: true}
	}

	hub.broadcastEntryUpdateLocal("entry-1", "whatsapp", nil)

	require.Len(t, connAll.Send, 1, "connection with no status filter should receive update")
	require.Len(t, connNew.Send, 1, "connection filtering for 'new' should receive update")

	require.Len(t, connOngoing.Send, 0, "connection filtering for 'ongoing' should NOT receive update for 'new' entry")
	require.Len(t, connFinished.Send, 0, "connection filtering for 'finished' should NOT receive update for 'new' entry")
}

func TestBroadcastEntryUpdateLocal_NoStatusFilterReceivesAllStatuses(t *testing.T) {
	authorizer := &hubDepartmentTestAuthorizer{
		entryAccess: map[string]bool{"user-1": true},
		viewOthers:  map[string]bool{"user-1": true},
	}

	hub := NewConversationHub(authorizer, nil, nil, "test-replica", "")
	hub.historyProvider = &statusTestHistoryProvider{
		entries: map[string]*conversation.InboxEntry{
			"e-new|whatsapp":      {EntryID: "e-new", EntryType: "whatsapp", ConversationStatus: conversation.ConversationStatusNew},
			"e-ongoing|whatsapp":  {EntryID: "e-ongoing", EntryType: "whatsapp", ConversationStatus: conversation.ConversationStatusOngoing},
			"e-finished|whatsapp": {EntryID: "e-finished", EntryType: "whatsapp", ConversationStatus: conversation.ConversationStatusFinished},
		},
	}

	conn := &WSConnection{ID: "conn-1", UserID: "user-1", WorkspaceID: "ws-1", ConversationStatus: "", Send: make(chan []byte, 10)}
	hub.connections[conn.ID] = conn
	hub.userConnections[conn.UserID] = map[string]bool{conn.ID: true}

	hub.broadcastEntryUpdateLocal("e-new", "whatsapp", nil)
	hub.broadcastEntryUpdateLocal("e-ongoing", "whatsapp", nil)
	hub.broadcastEntryUpdateLocal("e-finished", "whatsapp", nil)

	require.Len(t, conn.Send, 3, "unfiltered connection should receive entry updates for all statuses")
}

func TestHandleSetConversationStatus_CannotSetNewManually(t *testing.T) {
	authorizer := &hubDepartmentTestAuthorizer{
		entryAccess: map[string]bool{"admin-user": true},
	}
	statusMock := newMockStatusUpdater()
	statusMock.statuses["entry-1|whatsapp"] = conversation.ConversationStatusOngoing

	hub := NewConversationHub(authorizer, nil, nil, "test-replica", "")
	hub.statusUpdater = statusMock

	conn := &WSConnection{
		ID: "conn-admin", UserID: "admin-user", WorkspaceID: "ws-1",
		IsAdmin: true, Send: make(chan []byte, 5),
	}
	hub.connections[conn.ID] = conn
	hub.userConnections[conn.UserID] = map[string]bool{conn.ID: true}

	hub.broadcast = make(chan *broadcastMessage, 10)

	payload, _ := json.Marshal(SetConversationStatusPayload{
		EntryID:   "entry-1",
		EntryType: "whatsapp",
		Status:    "new",
	})

	hub.handleSetConversationStatus(conn, payload)

	ep := drainErrorPayload(t, conn)
	require.Equal(t, "forbidden", ep.Code)
	require.Len(t, statusMock.setCalls, 0)
}

func TestHandleSetConversationStatus_FinishedCannotReturnToOngoingManually(t *testing.T) {
	authorizer := &hubDepartmentTestAuthorizer{
		entryAccess: map[string]bool{"regular-user": true},
	}
	statusMock := newMockStatusUpdater()
	statusMock.statuses["entry-1|whatsapp"] = conversation.ConversationStatusFinished

	hub := NewConversationHub(authorizer, nil, nil, "test-replica", "")
	hub.statusUpdater = statusMock

	conn := &WSConnection{
		ID: "conn-regular", UserID: "regular-user", WorkspaceID: "ws-1",
		IsAdmin: false, Send: make(chan []byte, 5),
	}

	payload, _ := json.Marshal(SetConversationStatusPayload{
		EntryID:   "entry-1",
		EntryType: "whatsapp",
		Status:    "ongoing",
	})

	hub.handleSetConversationStatus(conn, payload)

	ep := drainErrorPayload(t, conn)
	require.Equal(t, "forbidden", ep.Code)
	require.Len(t, statusMock.setCalls, 0)
}

func TestHandleSetConversationStatus_NonAdminCanSetOngoing(t *testing.T) {
	authorizer := &hubDepartmentTestAuthorizer{
		entryAccess: map[string]bool{"regular-user": true},
	}
	statusMock := newMockStatusUpdater()

	hub := NewConversationHub(authorizer, nil, nil, "test-replica", "")
	hub.statusUpdater = statusMock
	hub.broadcast = make(chan *broadcastMessage, 10)

	conn := &WSConnection{
		ID: "conn-regular", UserID: "regular-user", WorkspaceID: "ws-1",
		IsAdmin: false, Send: make(chan []byte, 5),
	}

	payload, _ := json.Marshal(SetConversationStatusPayload{
		EntryID:   "entry-1",
		EntryType: "whatsapp",
		Status:    "ongoing",
	})

	hub.handleSetConversationStatus(conn, payload)

	require.Len(t, statusMock.setCalls, 1)
	require.Equal(t, conversation.ConversationStatusOngoing, statusMock.setCalls[0].Status)
	require.Len(t, conn.Send, 0, "no error for allowed transition")
}

func TestHandleSetConversationStatus_NonAdminCanSetFinished(t *testing.T) {
	authorizer := &hubDepartmentTestAuthorizer{
		entryAccess: map[string]bool{"regular-user": true},
	}
	statusMock := newMockStatusUpdater()

	hub := NewConversationHub(authorizer, nil, nil, "test-replica", "")
	hub.statusUpdater = statusMock
	hub.broadcast = make(chan *broadcastMessage, 10)

	conn := &WSConnection{
		ID: "conn-regular", UserID: "regular-user", WorkspaceID: "ws-1",
		IsAdmin: false, Send: make(chan []byte, 5),
	}

	payload, _ := json.Marshal(SetConversationStatusPayload{
		EntryID:   "entry-1",
		EntryType: "whatsapp",
		Status:    "finished",
	})

	hub.handleSetConversationStatus(conn, payload)

	require.Len(t, statusMock.setCalls, 1)
	require.Equal(t, conversation.ConversationStatusFinished, statusMock.setCalls[0].Status)
}

func TestHandleSetConversationStatus_InvalidStatusRejected(t *testing.T) {
	authorizer := &hubDepartmentTestAuthorizer{
		entryAccess: map[string]bool{"user-1": true},
	}
	hub := NewConversationHub(authorizer, nil, nil, "test-replica", "")
	hub.statusUpdater = newMockStatusUpdater()

	conn := &WSConnection{
		ID: "conn-1", UserID: "user-1", WorkspaceID: "ws-1",
		IsAdmin: true, Send: make(chan []byte, 5),
	}

	payload, _ := json.Marshal(SetConversationStatusPayload{
		EntryID:   "entry-1",
		EntryType: "whatsapp",
		Status:    "invalid_status",
	})

	hub.handleSetConversationStatus(conn, payload)

	ep := drainErrorPayload(t, conn)
	require.Equal(t, "invalid_status", ep.Code)
}

func TestHandleSetConversationStatus_MissingFieldsRejected(t *testing.T) {
	authorizer := &hubDepartmentTestAuthorizer{}
	hub := NewConversationHub(authorizer, nil, nil, "test-replica", "")
	hub.statusUpdater = newMockStatusUpdater()

	conn := &WSConnection{
		ID: "conn-1", UserID: "user-1", WorkspaceID: "ws-1",
		IsAdmin: true, Send: make(chan []byte, 5),
	}

	payload, _ := json.Marshal(SetConversationStatusPayload{
		EntryID: "entry-1",
	})

	hub.handleSetConversationStatus(conn, payload)

	ep := drainErrorPayload(t, conn)
	require.Equal(t, "missing_fields", ep.Code)
}

func TestHandleSetConversationStatus_UnauthorizedAccess(t *testing.T) {
	authorizer := &hubDepartmentTestAuthorizer{
		entryAccess: map[string]bool{"user-1": false},
	}
	statusMock := newMockStatusUpdater()

	hub := NewConversationHub(authorizer, nil, nil, "test-replica", "")
	hub.statusUpdater = statusMock

	conn := &WSConnection{
		ID: "conn-1", UserID: "user-1", WorkspaceID: "ws-1",
		IsAdmin: false, Send: make(chan []byte, 5),
	}

	payload, _ := json.Marshal(SetConversationStatusPayload{
		EntryID:   "entry-1",
		EntryType: "whatsapp",
		Status:    "ongoing",
	})

	hub.handleSetConversationStatus(conn, payload)

	ep := drainErrorPayload(t, conn)
	require.Equal(t, "unauthorized", ep.Code)
	require.Len(t, statusMock.setCalls, 0)
}

func TestHandleSetConversationStatus_BroadcastsStatusUpdate(t *testing.T) {
	authorizer := &hubDepartmentTestAuthorizer{
		entryAccess: map[string]bool{"user-1": true},
	}
	statusMock := newMockStatusUpdater()

	hub := NewConversationHub(authorizer, nil, nil, "test-replica", "")
	hub.statusUpdater = statusMock
	hub.broadcast = make(chan *broadcastMessage, 10)

	conn := &WSConnection{
		ID: "conn-1", UserID: "user-1", WorkspaceID: "ws-1",
		IsAdmin: false, Send: make(chan []byte, 5),
	}
	hub.connections[conn.ID] = conn
	hub.userConnections[conn.UserID] = map[string]bool{conn.ID: true}

	payload, _ := json.Marshal(SetConversationStatusPayload{
		EntryID:   "entry-1",
		EntryType: "whatsapp",
		Status:    "finished",
	})

	hub.handleSetConversationStatus(conn, payload)

	require.Len(t, hub.broadcast, 1)
	bm := <-hub.broadcast
	require.Equal(t, "entry-1", bm.entryID)
	require.Equal(t, "whatsapp", bm.entryType)

	require.Equal(t, WSEventConversationStatusUpdate, bm.event.Type)

	payloadBytes, _ := json.Marshal(bm.event.Payload)
	var statusPayload ConversationStatusUpdatePayload
	require.NoError(t, json.Unmarshal(payloadBytes, &statusPayload))
	require.Equal(t, "entry-1", statusPayload.EntryID)
	require.Equal(t, "whatsapp", statusPayload.EntryType)
	require.Equal(t, "finished", statusPayload.Status)
}

func TestHandleSwitchView_SetsConversationStatusOnConnection(t *testing.T) {
	authorizer := &hubDepartmentTestAuthorizer{
		entryAccess: map[string]bool{"user-1": true},
	}

	hub := NewConversationHub(authorizer, nil, nil, "test-replica", "")

	conn := &WSConnection{
		ID: "conn-1", UserID: "user-1", WorkspaceID: "ws-1",
		ViewMode: "global", Send: make(chan []byte, 5),
	}
	hub.connections[conn.ID] = conn
	hub.userConnections[conn.UserID] = map[string]bool{conn.ID: true}

	payload, _ := json.Marshal(SwitchViewPayload{
		CampaignType:       "whatsapp",
		ConversationStatus: "new",
	})

	hub.handleSwitchView(conn, payload)

	require.Equal(t, "new", conn.ConversationStatus)

	env := drainEvent(t, conn)
	require.Equal(t, WSEventViewSwitched, env.Type)
	var vp ViewSwitchedPayload
	require.NoError(t, json.Unmarshal(env.Payload, &vp))
	require.Equal(t, "new", vp.ConversationStatus)
}

func TestHandleSwitchView_ClearsConversationStatusWhenEmpty(t *testing.T) {
	authorizer := &hubDepartmentTestAuthorizer{
		entryAccess: map[string]bool{"user-1": true},
	}

	hub := NewConversationHub(authorizer, nil, nil, "test-replica", "")

	conn := &WSConnection{
		ID: "conn-1", UserID: "user-1", WorkspaceID: "ws-1",
		ViewMode: "global", ConversationStatus: "ongoing",
		Send: make(chan []byte, 5),
	}
	hub.connections[conn.ID] = conn
	hub.userConnections[conn.UserID] = map[string]bool{conn.ID: true}

	payload, _ := json.Marshal(SwitchViewPayload{
		CampaignType: "whatsapp",
	})

	hub.handleSwitchView(conn, payload)

	require.Equal(t, "", conn.ConversationStatus, "switching view with empty status should clear the filter")
}

