package ws

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"vozko/domain/conversation"
	"vozko/domain/user"
)

// The send path runs in a detached goroutine. On 2026-08-04 a nil dereference inside
// SendMediaMessage escaped it and, with no recover in place, took the whole process
// down — every WebSocket, plus HTTP, webhooks and the campaign consumers. These pin
// the containment: a failing send must cost the sender an error, never the process.

type panicSendTestUserRepo struct {
	user.UserRepository
	u   *user.User
	err error
}

func (r panicSendTestUserRepo) FindByID(string) (*user.User, error) { return r.u, r.err }

type panicSendTestSender struct {
	conversation.MessageSender
	panicWith any
	textCalls chan string
}

func (s panicSendTestSender) SendTextMessage(_, _, text, _, _ string) (*conversation.Message, error) {
	if s.panicWith != nil {
		panic(s.panicWith)
	}
	if s.textCalls != nil {
		s.textCalls <- text
	}
	return &conversation.Message{ID: "m1"}, nil
}

func (s panicSendTestSender) SendMediaMessage(_, _, _, _, _, _ string, _ string) (*conversation.Message, error) {
	if s.panicWith != nil {
		panic(s.panicWith)
	}
	return &conversation.Message{ID: "m1"}, nil
}

// newSendHub wires a hub with one registered connection, so BroadcastMessageError has
// somewhere to deliver.
func newSendHub(t *testing.T, sender conversation.MessageSender, userRepo user.UserRepository) (*ConversationHub, *WSConnection) {
	t.Helper()

	authorizer := &hubDepartmentTestAuthorizer{entryAccess: map[string]bool{"user-1": true}}
	hub := NewConversationHub(authorizer, userRepo, nil, "test-replica", "")
	hub.messageSender = sender

	conn := &WSConnection{ID: "conn-1", UserID: "user-1", WorkspaceID: "ws-1", Send: make(chan []byte, 4)}
	hub.connections[conn.ID] = conn
	hub.userConnections[conn.UserID] = map[string]bool{conn.ID: true}

	return hub, conn
}

func awaitSendPayload(t *testing.T, conn *WSConnection) WSOutgoingMessage {
	t.Helper()
	select {
	case raw := <-conn.Send:
		var msg WSOutgoingMessage
		require.NoError(t, json.Unmarshal(raw, &msg))
		return msg
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the hub to deliver a payload")
		return WSOutgoingMessage{}
	}
}

// TestHandleSend_PanicInSenderIsContained is the amplifier. Without the recover the
// test binary itself dies here, which is exactly what production did.
func TestHandleSend_PanicInSenderIsContained(t *testing.T) {
	sender := panicSendTestSender{panicWith: "boom: simulated nil dereference"}
	hub, conn := newSendHub(t, sender, panicSendTestUserRepo{u: &user.User{ID: "user-1", Username: "Dakauann"}})

	payload, err := json.Marshal(SendMessagePayload{
		EntryID:   "entry-1",
		EntryType: "whatsapp",
		Text:      "oi",
		RequestID: "req-1",
	})
	require.NoError(t, err)

	hub.handleSend(conn, payload)

	msg := awaitSendPayload(t, conn)
	require.Equal(t, WSEventMessageError, msg.Type,
		"a panicking send must be reported back to the sender, not swallowed")

	body, err := json.Marshal(msg.Payload)
	require.NoError(t, err)
	var errPayload MessageErrorPayload
	require.NoError(t, json.Unmarshal(body, &errPayload))
	require.Equal(t, "internal_error", errPayload.Code)
	require.Equal(t, "req-1", errPayload.RequestID)
}

// TestHandleSend_UserLookupFailureDoesNotPanic covers the second nil dereference on the
// same path: FindByID's error was logged and then user.Username read anyway.
func TestHandleSend_UserLookupFailureDoesNotPanic(t *testing.T) {
	textCalls := make(chan string, 1)
	sender := panicSendTestSender{textCalls: textCalls}
	hub, conn := newSendHub(t, sender, panicSendTestUserRepo{u: nil, err: errors.New("user lookup failed")})

	payload, err := json.Marshal(SendMessagePayload{
		EntryID:   "entry-1",
		EntryType: "whatsapp",
		Text:      "oi",
		RequestID: "req-1",
	})
	require.NoError(t, err)

	hub.handleSend(conn, payload)

	select {
	case text := <-textCalls:
		require.Equal(t, "oi", text,
			"an unresolved user must not stop the message from being sent")
	case <-time.After(2 * time.Second):
		t.Fatal("the send never happened: the user lookup failure broke the path")
	}
}

// TestHandleSend_SignedMessageSurvivesUserLookupFailure pins the signature branch,
// which is the one that actually reads the username.
func TestHandleSend_SignedMessageSurvivesUserLookupFailure(t *testing.T) {
	textCalls := make(chan string, 1)
	sender := panicSendTestSender{textCalls: textCalls}
	hub, conn := newSendHub(t, sender, panicSendTestUserRepo{u: nil, err: errors.New("user lookup failed")})

	payload, err := json.Marshal(SendMessagePayload{
		EntryID:   "entry-1",
		EntryType: "whatsapp",
		Text:      "oi",
		RequestID: "req-1",
		Signed:    true,
	})
	require.NoError(t, err)

	hub.handleSend(conn, payload)

	select {
	case text := <-textCalls:
		require.Contains(t, text, "oi",
			"the message body must survive even when the signature name is unavailable")
	case <-time.After(2 * time.Second):
		t.Fatal("the signed send never happened")
	}
}
