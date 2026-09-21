package ws

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vozko/domain/conversation"
)

type multiOpenAuthorizer struct{}

func (multiOpenAuthorizer) CanAccessEntry(string, string, string, string, bool) bool    { return true }
func (multiOpenAuthorizer) CanAccessCampaign(string, string, string, string, bool) bool { return true }
func (multiOpenAuthorizer) GetAccessibleEntryIDs(string, string, bool) []string         { return nil }
func (multiOpenAuthorizer) IsWorkspaceMember(string, string) bool                       { return true }
func (multiOpenAuthorizer) IsWorkspaceOwnerOrAdmin(string, string) bool                 { return false }
func (multiOpenAuthorizer) HasWorkspacePermission(string, string, string, string, bool) bool {
	return true
}
func (multiOpenAuthorizer) GetDepartmentScope(string, string, bool) (conversation.DepartmentAccessScope, bool) {
	return conversation.DepartmentAccessScope{}, false
}

func newMultiOpenHub(t *testing.T) *ConversationHub {
	t.Helper()
	hub := NewConversationHub(multiOpenAuthorizer{}, nil, nil, nil, "test-replica", "")
	hub.SetWSMetrics(noopWSMetricsRecorder{})
	return hub
}

func subscribeConn(t *testing.T, hub *ConversationHub, conn *WSConnection, entryID, entryType string) {
	t.Helper()
	payload, err := json.Marshal(SubscribePayload{EntryID: entryID, EntryType: entryType})
	require.NoError(t, err)
	hub.handleSubscribe(conn, payload)
	drain(conn)
}

func unsubscribeConn(t *testing.T, hub *ConversationHub, conn *WSConnection, entryID, entryType string) {
	t.Helper()
	payload, err := json.Marshal(SubscribePayload{EntryID: entryID, EntryType: entryType})
	require.NoError(t, err)
	hub.handleUnsubscribe(conn, payload)
	drain(conn)
}

func registerConn(hub *ConversationHub, conn *WSConnection) {
	hub.connMu.Lock()
	hub.connections[conn.ID] = conn
	if hub.userConnections[conn.UserID] == nil {
		hub.userConnections[conn.UserID] = make(map[string]bool)
	}
	hub.userConnections[conn.UserID][conn.ID] = true
	hub.connMu.Unlock()
}

func drain(conn *WSConnection) {
	for {
		select {
		case <-conn.Send:
		default:
			return
		}
	}
}

func frameTypes(conn *WSConnection) []WSEventType {
	var types []WSEventType
	for {
		select {
		case data := <-conn.Send:
			var msg struct {
				Type WSEventType `json:"type"`
			}
			if err := json.Unmarshal(data, &msg); err == nil {
				types = append(types, msg.Type)
			}
		default:
			return types
		}
	}
}

func openConn(id, userID string) *WSConnection {
	return &WSConnection{
		ID:          id,
		UserID:      userID,
		WorkspaceID: "ws-1",
		Send:        make(chan []byte, 16),
		Done:        make(chan struct{}),
	}
}

func TestOneConnectionReceivesEveryConversationItHasOpen(t *testing.T) {
	hub := newMultiOpenHub(t)
	conn := openConn("conn-1", "user-1")
	registerConn(hub, conn)

	subscribeConn(t, hub, conn, "entry-a", "whatsapp")
	subscribeConn(t, hub, conn, "entry-b", "whatsapp")
	subscribeConn(t, hub, conn, "entry-c", "telegram")

	for _, entry := range []struct{ id, kind string }{
		{"entry-a", "whatsapp"},
		{"entry-b", "whatsapp"},
		{"entry-c", "telegram"},
	} {
		hub.handleBroadcast(&broadcastMessage{
			entryID:   entry.id,
			entryType: entry.kind,
			event:     &WSOutgoingMessage{Type: WSEventTypingRemote},
			fromRedis: true,
		})
	}

	assert.Len(t, frameTypes(conn), 3,
		"a conversation opened in its own window must receive its own frames")
}

func TestFramesReachOnlyTheConnectionThatOpenedTheConversation(t *testing.T) {
	hub := newMultiOpenHub(t)
	first := openConn("conn-1", "user-1")
	second := openConn("conn-2", "user-1")
	registerConn(hub, first)
	registerConn(hub, second)

	subscribeConn(t, hub, first, "entry-a", "whatsapp")
	subscribeConn(t, hub, second, "entry-b", "whatsapp")

	hub.handleBroadcast(&broadcastMessage{
		entryID:   "entry-a",
		entryType: "whatsapp",
		event:     &WSOutgoingMessage{Type: WSEventTypingRemote},
		fromRedis: true,
	})

	assert.Len(t, frameTypes(first), 1, "the connection that opened entry-a must receive it")
	assert.Empty(t, frameTypes(second),
		"a connection that never opened entry-a must not be fed its traffic")
}

func TestClosingOneWindowKeepsAnotherConnectionSubscribed(t *testing.T) {
	hub := newMultiOpenHub(t)
	keeper := openConn("conn-keeper", "user-1")
	closer := openConn("conn-closer", "user-1")
	registerConn(hub, keeper)
	registerConn(hub, closer)

	subscribeConn(t, hub, keeper, "entry-a", "whatsapp")
	subscribeConn(t, hub, closer, "entry-a", "whatsapp")

	unsubscribeConn(t, hub, closer, "entry-a", "whatsapp")

	hub.handleBroadcast(&broadcastMessage{
		entryID:   "entry-a",
		entryType: "whatsapp",
		event:     &WSOutgoingMessage{Type: WSEventTypingRemote},
		fromRedis: true,
	})

	assert.Len(t, frameTypes(keeper), 1,
		"closing one window must not unsubscribe the conversation elsewhere")
	assert.Empty(t, frameTypes(closer), "the closed window must stop receiving")
}

func TestClosingOneWindowKeepsTheOtherOpenConversations(t *testing.T) {
	hub := newMultiOpenHub(t)
	conn := openConn("conn-1", "user-1")
	registerConn(hub, conn)

	subscribeConn(t, hub, conn, "entry-a", "whatsapp")
	subscribeConn(t, hub, conn, "entry-b", "whatsapp")

	unsubscribeConn(t, hub, conn, "entry-a", "whatsapp")

	hub.handleBroadcast(&broadcastMessage{
		entryID:   "entry-b",
		entryType: "whatsapp",
		event:     &WSOutgoingMessage{Type: WSEventTypingRemote},
		fromRedis: true,
	})
	hub.handleBroadcast(&broadcastMessage{
		entryID:   "entry-a",
		entryType: "whatsapp",
		event:     &WSOutgoingMessage{Type: WSEventTypingRemote},
		fromRedis: true,
	})

	assert.Len(t, frameTypes(conn), 1,
		"only the window that was closed should stop receiving")
}

func TestDisconnectDropsOnlyThatConnectionsSubscriptions(t *testing.T) {
	hub := newMultiOpenHub(t)
	staying := openConn("conn-staying", "user-1")
	leaving := openConn("conn-leaving", "user-1")
	registerConn(hub, staying)
	registerConn(hub, leaving)

	subscribeConn(t, hub, staying, "entry-a", "whatsapp")
	subscribeConn(t, hub, leaving, "entry-a", "whatsapp")

	hub.handleUnregister(leaving)

	hub.handleBroadcast(&broadcastMessage{
		entryID:   "entry-a",
		entryType: "whatsapp",
		event:     &WSOutgoingMessage{Type: WSEventTypingRemote},
		fromRedis: true,
	})

	assert.Len(t, frameTypes(staying), 1,
		"one tab closing must not unsubscribe the tab that stayed")
}

func TestTypingIsNotEchoedToTheSender(t *testing.T) {
	hub := newMultiOpenHub(t)
	sender := openConn("conn-sender", "user-1")
	other := openConn("conn-other", "user-2")
	registerConn(hub, sender)
	registerConn(hub, other)

	subscribeConn(t, hub, sender, "entry-a", "whatsapp")
	subscribeConn(t, hub, other, "entry-a", "whatsapp")

	hub.handleBroadcast(&broadcastMessage{
		entryID:       "entry-a",
		entryType:     "whatsapp",
		excludeUserID: "user-1",
		event:         &WSOutgoingMessage{Type: WSEventTypingRemote},
		fromRedis:     true,
	})

	assert.Empty(t, frameTypes(sender), "a typing echo must not come back to its sender")
	assert.Len(t, frameTypes(other), 1)
}
