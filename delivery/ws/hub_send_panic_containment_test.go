package ws

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"vozko/domain/conversation"
)

// The send path runs in a detached goroutine. On 2026-08-04 a nil dereference inside
// SendMediaMessage escaped it and, with no recover in place, took the whole process
// down — every WebSocket, plus HTTP, webhooks and the campaign consumers. These pin
// the containment: a failing send must cost the sender an error, never the process.
//
// What the send DOES (resolving the operator, signing, routing text vs media) moved
// to conversation.OperatorSendUseCase and is covered by its own tests. What is left
// here is the transport's contract: authorize, delegate, report.

type panicSendTestUseCase struct {
	panicWith any
	err       error
	calls     chan conversation.OperatorSendInput
}

func (s panicSendTestUseCase) Execute(_ context.Context, in conversation.OperatorSendInput) (*conversation.Message, error) {
	if s.panicWith != nil {
		panic(s.panicWith)
	}
	if s.calls != nil {
		s.calls <- in
	}
	if s.err != nil {
		return nil, s.err
	}
	return &conversation.Message{ID: "m1"}, nil
}

// newSendHub wires a hub with one registered connection, so BroadcastMessageError has
// somewhere to deliver.
func newSendHub(t *testing.T, operatorSend conversation.OperatorSendUseCase) (*ConversationHub, *WSConnection) {
	t.Helper()

	authorizer := &hubDepartmentTestAuthorizer{entryAccess: map[string]bool{"user-1": true}}
	hub := NewConversationHub(authorizer, nil, operatorSend, nil, "test-replica", "")

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
	hub, conn := newSendHub(t, panicSendTestUseCase{panicWith: "boom: simulated nil dereference"})

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

// A send failure is reported to the sender with its own code, not confused with a
// panic and not silently dropped.
func TestHandleSend_UseCaseErrorIsReported(t *testing.T) {
	hub, conn := newSendHub(t, panicSendTestUseCase{err: errors.New("window closed")})

	payload, err := json.Marshal(SendMessagePayload{
		EntryID:   "entry-1",
		EntryType: "whatsapp",
		Text:      "oi",
		RequestID: "req-1",
	})
	require.NoError(t, err)

	hub.handleSend(conn, payload)

	msg := awaitSendPayload(t, conn)
	require.Equal(t, WSEventMessageError, msg.Type)

	body, err := json.Marshal(msg.Payload)
	require.NoError(t, err)
	var errPayload MessageErrorPayload
	require.NoError(t, json.Unmarshal(body, &errPayload))
	require.Equal(t, "send_failed", errPayload.Code)
}

// The frame's fields must reach the use case intact. The media pair in particular
// is only meaningful together, which is why the transport sets both or neither.
func TestHandleSend_TranslatesTheFrameFaithfully(t *testing.T) {
	calls := make(chan conversation.OperatorSendInput, 1)
	hub, conn := newSendHub(t, panicSendTestUseCase{calls: calls})

	mediaID, mediaType, replyTo := "med-1", "image", "msg-9"
	payload, err := json.Marshal(SendMessagePayload{
		EntryID:          "entry-1",
		EntryType:        "whatsapp",
		Text:             "oi",
		RequestID:        "req-1",
		Signed:           true,
		MediaID:          &mediaID,
		MediaType:        &mediaType,
		ReplyToMessageID: &replyTo,
	})
	require.NoError(t, err)

	hub.handleSend(conn, payload)

	select {
	case in := <-calls:
		require.Equal(t, "entry-1", in.EntryID)
		require.Equal(t, "whatsapp", in.EntryType)
		require.Equal(t, "user-1", in.SenderUserID)
		require.Equal(t, "ws-1", in.WorkspaceID)
		require.Equal(t, "oi", in.Text)
		require.True(t, in.Signed)
		require.Equal(t, "med-1", in.MediaID)
		require.Equal(t, "image", in.MediaType)
		require.Equal(t, "msg-9", in.ReplyToMessageID)
		require.Nil(t, in.Buttons, "a plain send must not be turned into an interactive prompt")
	case <-time.After(2 * time.Second):
		t.Fatal("the send never reached the use case")
	}
}

// A button frame is the same use case with the interactive payload attached, so
// the post-send side effects cannot diverge between the two send shapes.
func TestHandleSendButton_GoesThroughTheSameUseCase(t *testing.T) {
	calls := make(chan conversation.OperatorSendInput, 1)
	hub, conn := newSendHub(t, panicSendTestUseCase{calls: calls})

	payload, err := json.Marshal(SendButtonPayload{
		EntryID:   "entry-1",
		EntryType: "whatsapp",
		BodyText:  "escolha",
		RequestID: "req-1",
		Buttons:   []conversation.ButtonPayload{{ID: "b1", Title: "Sim"}},
	})
	require.NoError(t, err)

	hub.handleSendButton(conn, payload)

	select {
	case in := <-calls:
		require.NotNil(t, in.Buttons)
		require.Equal(t, "escolha", in.Buttons.BodyText)
		require.Len(t, in.Buttons.Buttons, 1)
	case <-time.After(2 * time.Second):
		t.Fatal("the button send never reached the use case")
	}
}
