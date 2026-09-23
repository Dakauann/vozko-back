package ws

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"vozko/domain/conversation"
)

func newReentrancyHub(t *testing.T, userIDs ...string) (*ConversationHub, []*WSConnection) {
	t.Helper()

	access := make(map[string]bool, len(userIDs))
	for _, id := range userIDs {
		access[id] = true
	}

	authorizer := &hubDepartmentTestAuthorizer{entryAccess: access}
	hub := NewConversationHub(authorizer, nil, nil, &eligibilityFakeSharedState{}, "test-replica", "")
	hub.SetWSMetrics(noopWSMetricsRecorder{})

	conns := make([]*WSConnection, 0, len(userIDs))
	for i, id := range userIDs {
		conn := &WSConnection{
			ID:          "conn-" + id,
			UserID:      id,
			WorkspaceID: "ws-1",
			Send:        make(chan []byte, 64),
			Done:        make(chan struct{}),
		}
		hub.connections[conn.ID] = conn
		hub.userConnections[conn.UserID] = map[string]bool{conn.ID: true}
		conns = append(conns, conn)
		_ = i
	}

	return hub, conns
}

func subscribeToEntry(hub *ConversationHub, entryID, entryType string, conns ...*WSConnection) {
	sub := entrySubscription{entryID: entryID, entryType: entryType}
	if hub.entrySubscribers[sub] == nil {
		hub.entrySubscribers[sub] = make(map[string]bool)
	}
	for _, conn := range conns {
		hub.entrySubscribers[sub][conn.ID] = true
	}
}

func saturateBroadcast(hub *ConversationHub) {
	for {
		select {
		case hub.broadcast <- &broadcastMessage{entryID: "filler", entryType: "whatsapp"}:
		default:
			return
		}
	}
}

func runsWithin(t *testing.T, d time.Duration, fn func()) bool {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

func TestHandleTyping_DoesNotBlockWhenBroadcastChannelIsFull(t *testing.T) {
	hub, conns := newReentrancyHub(t, "user-1", "user-2")
	subscribeToEntry(hub, "entry-1", "whatsapp", conns[0], conns[1])
	saturateBroadcast(hub)

	payload, err := json.Marshal(map[string]any{
		"entry_id":   "entry-1",
		"entry_type": "whatsapp",
		"is_typing":  true,
	})
	require.NoError(t, err)

	completed := runsWithin(t, 2*time.Second, func() {
		hub.handleTyping(conns[0], payload)
	})

	require.True(t, completed, "handleTyping must not block the hub loop when the broadcast channel is full")
}

func TestHandleSetConversationStatus_DoesNotBlockWhenBroadcastChannelIsFull(t *testing.T) {
	hub, conns := newReentrancyHub(t, "user-1")
	statusMock := newMockStatusUpdater()
	statusMock.statuses["entry-1|whatsapp"] = conversation.ConversationStatusNew
	hub.statusUpdater = statusMock
	subscribeToEntry(hub, "entry-1", "whatsapp", conns[0])
	saturateBroadcast(hub)

	payload, err := json.Marshal(SetConversationStatusPayload{
		EntryID:   "entry-1",
		EntryType: "whatsapp",
		Status:    string(conversation.ConversationStatusOngoing),
	})
	require.NoError(t, err)

	completed := runsWithin(t, 2*time.Second, func() {
		hub.handleSetConversationStatus(conns[0], payload)
	})

	require.True(t, completed, "handleSetConversationStatus must not block the hub loop when the broadcast channel is full")
}

func TestHandleTyping_StillReachesOtherSubscribers(t *testing.T) {
	hub, conns := newReentrancyHub(t, "user-1", "user-2")
	sender, receiver := conns[0], conns[1]
	subscribeToEntry(hub, "entry-1", "whatsapp", sender, receiver)

	payload, err := json.Marshal(map[string]any{
		"entry_id":   "entry-1",
		"entry_type": "whatsapp",
		"is_typing":  true,
	})
	require.NoError(t, err)

	hub.handleTyping(sender, payload)

	require.Len(t, sender.Send, 0, "the typing sender must not receive its own event")

	select {
	case raw := <-receiver.Send:
		var msg WSOutgoingMessage
		require.NoError(t, json.Unmarshal(raw, &msg))
		require.Equal(t, WSEventTypingRemote, msg.Type)
	case <-time.After(2 * time.Second):
		t.Fatal("subscriber did not receive the typing event")
	}
}
