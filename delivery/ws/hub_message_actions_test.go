package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type actionCall struct {
	by     shared.Person
	target conversation.MessageTarget
	arg    string
}

type fakeMessageAction struct {
	calls  chan actionCall
	answer *conversation.Message
	err    error
}

func newFakeMessageAction() *fakeMessageAction {
	return &fakeMessageAction{calls: make(chan actionCall, 4), answer: &conversation.Message{ID: "msg-new"}}
}

func (f *fakeMessageAction) run(by shared.Person, target conversation.MessageTarget, arg string) (*conversation.Message, error) {
	f.calls <- actionCall{by: by, target: target, arg: arg}
	if f.err != nil {
		return nil, f.err
	}
	return f.answer, nil
}

type fakeReact struct{ *fakeMessageAction }

func (f fakeReact) Execute(_ context.Context, by shared.Person, target conversation.MessageTarget, emoji string) (*conversation.Message, error) {
	return f.run(by, target, emoji)
}

type fakeEdit struct{ *fakeMessageAction }

func (f fakeEdit) Execute(_ context.Context, by shared.Person, target conversation.MessageTarget, text string) (*conversation.Message, error) {
	return f.run(by, target, text)
}

type fakeRetract struct{ *fakeMessageAction }

func (f fakeRetract) Execute(_ context.Context, by shared.Person, target conversation.MessageTarget) (*conversation.Message, error) {
	return f.run(by, target, "")
}

type fakeResend struct{ *fakeMessageAction }

func (f fakeResend) Execute(_ context.Context, by shared.Person, target conversation.MessageTarget, requestID string) (*conversation.Message, error) {
	return f.run(by, target, requestID)
}

type fixedCapabilities conversation.MessageCapabilities

func (c fixedCapabilities) MessageCapabilities(shared.EntryType) conversation.MessageCapabilities {
	return conversation.MessageCapabilities(c)
}

func newActionHub(t *testing.T, action *fakeMessageAction) (*ConversationHub, *WSConnection) {
	t.Helper()
	hub, conn := newSendHub(t, panicSendTestUseCase{})
	hub.SetMessageActions(conversation.MessageActionUseCases{
		React:        fakeReact{action},
		Edit:         fakeEdit{action},
		Delete:       fakeRetract{action},
		Resend:       fakeResend{action},
		Capabilities: fixedCapabilities{React: true},
	})
	return hub, conn
}

func actionFrame(t *testing.T, fields map[string]any) json.RawMessage {
	t.Helper()
	base := map[string]any{"entry_id": "entry-1", "entry_type": "telegram", "message_id": "msg-1", "request_id": "req-1"}
	for k, v := range fields {
		base[k] = v
	}
	raw, err := json.Marshal(base)
	require.NoError(t, err)
	return raw
}

func awaitCall(t *testing.T, action *fakeMessageAction) actionCall {
	t.Helper()
	select {
	case call := <-action.calls:
		return call
	case <-time.After(2 * time.Second):
		t.Fatal("the command never reached its use case")
		return actionCall{}
	}
}

func decodeErrorPayload(t *testing.T, msg WSOutgoingMessage) ErrorPayload {
	t.Helper()
	raw, err := json.Marshal(msg.Payload)
	require.NoError(t, err)
	var payload ErrorPayload
	require.NoError(t, json.Unmarshal(raw, &payload))
	return payload
}

func TestMessageActionCommandsReachTheirUseCaseAsTheConnectedOperator(t *testing.T) {
	cases := map[WSEventType]struct {
		extra map[string]any
		arg   string
	}{
		WSEventReactMessage:  {map[string]any{"emoji": "👍"}, "👍"},
		WSEventEditMessage:   {map[string]any{"text": "corrigido"}, "corrigido"},
		WSEventDeleteMessage: {nil, ""},
		WSEventResendMessage: {nil, "req-1"},
	}
	for event, tc := range cases {
		t.Run(string(event), func(t *testing.T) {
			action := newFakeMessageAction()
			hub, conn := newActionHub(t, action)

			hub.handleIncoming(&incomingMessage{conn: conn, message: &WSIncomingMessage{Type: event, Payload: actionFrame(t, tc.extra)}})

			call := awaitCall(t, action)
			require.Equal(t, shared.Person{UserID: "user-1"}, call.by)
			require.Equal(t, conversation.MessageTarget{WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: shared.EntryTypeTelegram, MessageID: "msg-1"}, call.target)
			require.Equal(t, tc.arg, call.arg)
		})
	}
}

func TestARefusedMessageActionAnswersWithTheContractCode(t *testing.T) {
	cases := map[error]string{
		conversation.ErrUnauthorized:                            "not_allowed",
		conversation.ErrMessageActionNotAllowed:                 "not_allowed",
		conversation.ErrMessageNotFound:                         "not_found",
		conversation.ErrMessageEditTextInvalid:                  "invalid",
		conversation.ErrEntryTypeInvalid:                        "invalid",
		conversation.ErrMessageActionUnsupported:                "unsupported",
		conversation.ErrCapabilityUnsupported:                   "unsupported",
		conversation.ErrMessageActionWindowExpired:              "window_expired",
		conversation.ErrOutboundWindowClosed:                    "window_expired",
		fmt.Errorf("wrapped: %w", conversation.ErrWindowClosed): "window_expired",
		errors.New("telegram is down"):                          "failed",
	}
	for cause, code := range cases {
		t.Run(code+" from "+cause.Error(), func(t *testing.T) {
			action := newFakeMessageAction()
			action.err = cause
			hub, conn := newActionHub(t, action)

			hub.handleIncoming(&incomingMessage{conn: conn, message: &WSIncomingMessage{Type: WSEventEditMessage, Payload: actionFrame(t, map[string]any{"text": "x"})}})

			msg := awaitSendPayload(t, conn)
			require.Equal(t, WSEventError, msg.Type)
			payload := decodeErrorPayload(t, msg)
			require.Equal(t, code, payload.Code)
			require.Equal(t, "req-1", payload.RequestID)
			require.Equal(t, "entry-1", payload.EntryID)
			require.Equal(t, "telegram", payload.EntryType)
		})
	}
}

func TestAMessageActionWithoutItsIdentifiersIsInvalid(t *testing.T) {
	action := newFakeMessageAction()
	hub, conn := newActionHub(t, action)

	hub.handleIncoming(&incomingMessage{conn: conn, message: &WSIncomingMessage{Type: WSEventReactMessage, Payload: actionFrame(t, map[string]any{"message_id": ""})}})

	payload := decodeErrorPayload(t, awaitSendPayload(t, conn))
	require.Equal(t, "invalid", payload.Code)
	require.Equal(t, "req-1", payload.RequestID)
	require.Len(t, action.calls, 0)
}

func TestMessageActionsAreUnsupportedWhenTheServerHasNone(t *testing.T) {
	hub, conn := newSendHub(t, panicSendTestUseCase{})

	hub.handleIncoming(&incomingMessage{conn: conn, message: &WSIncomingMessage{Type: WSEventDeleteMessage, Payload: actionFrame(t, nil)}})

	payload := decodeErrorPayload(t, awaitSendPayload(t, conn))
	require.Equal(t, "unsupported", payload.Code)
}

func awaitBroadcast(t *testing.T, hub *ConversationHub) *broadcastMessage {
	t.Helper()
	select {
	case msg := <-hub.broadcast:
		return msg
	case <-time.After(2 * time.Second):
		t.Fatal("nothing was broadcast")
		return nil
	}
}

func TestAResendAnswersWithMessageSentForItsRequest(t *testing.T) {
	action := newFakeMessageAction()
	hub, conn := newActionHub(t, action)

	hub.handleIncoming(&incomingMessage{conn: conn, message: &WSIncomingMessage{Type: WSEventResendMessage, Payload: actionFrame(t, nil)}})

	sent := awaitBroadcast(t, hub)
	require.Equal(t, WSEventMessageSent, sent.event.Type)
	payload := sent.event.Payload.(MessageSentPayload)
	require.Equal(t, "req-1", payload.RequestID)
	require.Equal(t, "msg-new", payload.Message.ID)
}

func TestAResendThatCannotBeDeliveredAnswersLikeAFailedSend(t *testing.T) {
	action := newFakeMessageAction()
	action.err = fmt.Errorf("%w: %w", conversation.ErrResendDeliveryFailed, conversation.ErrOutboundWindowClosed)
	hub, conn := newActionHub(t, action)

	hub.handleIncoming(&incomingMessage{conn: conn, message: &WSIncomingMessage{Type: WSEventResendMessage, Payload: actionFrame(t, nil)}})

	msg := awaitSendPayload(t, conn)
	require.Equal(t, WSEventMessageError, msg.Type)
	raw, err := json.Marshal(msg.Payload)
	require.NoError(t, err)
	var payload MessageErrorPayload
	require.NoError(t, json.Unmarshal(raw, &payload))
	require.Equal(t, "req-1", payload.RequestID)
	require.Equal(t, "send_failed", payload.Code)
}

func TestAResendStillInFlightIsAnsweredByTheOriginalSend(t *testing.T) {
	action := newFakeMessageAction()
	action.err = conversation.ErrSendRequestInFlight
	hub, conn := newActionHub(t, action)

	hub.handleIncoming(&incomingMessage{conn: conn, message: &WSIncomingMessage{Type: WSEventResendMessage, Payload: actionFrame(t, nil)}})
	awaitCall(t, action)

	select {
	case raw := <-conn.Send:
		t.Fatalf("a request still in flight must stay silent until its own answer, got %s", raw)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestASendStillInFlightStaysSilent(t *testing.T) {
	hub, conn := newSendHub(t, panicSendTestUseCase{err: conversation.ErrSendRequestInFlight})
	payload, err := json.Marshal(SendMessagePayload{EntryID: "entry-1", EntryType: "whatsapp", Text: "oi", RequestID: "req-1"})
	require.NoError(t, err)

	hub.handleSend(conn, payload)

	select {
	case raw := <-conn.Send:
		t.Fatalf("a replayed send still in flight must wait for the original answer, got %s", raw)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestTheSubscribedPayloadAlwaysCarriesTheMessageCapabilities(t *testing.T) {
	raw, err := json.Marshal(SubscribedPayload{EntryID: "e-1", EntryType: "whatsapp"})
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(raw, &fields))
	caps, ok := fields["message_capabilities"].(map[string]any)
	require.True(t, ok, "message_capabilities missing from %s", raw)
	for _, key := range []string{"react", "edit", "delete", "edit_window_seconds", "delete_window_seconds"} {
		_, present := caps[key]
		require.True(t, present, "%s missing from %s", key, raw)
	}
}

func TestSubscribingSendsTheChannelMessageCapabilities(t *testing.T) {
	hub, conn := newActionHub(t, newFakeMessageAction())
	payload, err := json.Marshal(SubscribePayload{EntryID: "entry-1", EntryType: "telegram"})
	require.NoError(t, err)

	hub.handleSubscribe(conn, payload)

	msg := awaitSendPayload(t, conn)
	require.Equal(t, WSEventSubscribed, msg.Type)
	raw, err := json.Marshal(msg.Payload)
	require.NoError(t, err)
	var subscribed SubscribedPayload
	require.NoError(t, json.Unmarshal(raw, &subscribed))
	require.Equal(t, conversation.MessageCapabilities{React: true}, subscribed.MessageCapabilities)
}

func TestTheSenderAlwaysGetsItsOwnMessageSent(t *testing.T) {
	hub, conn := newSendHub(t, panicSendTestUseCase{})
	sub := entrySubscription{entryID: "entry-1", entryType: "whatsapp"}
	hub.sentMessageIDs[conn.ID] = map[entrySubscription]map[string]bool{sub: {"m1": true}}

	hub.answerOperatorSend(conn, "req-1", "entry-1", "whatsapp", conversation.OperatorSendInput{EntryID: "entry-1", EntryType: "whatsapp", Text: "oi", RequestID: "req-1"})

	msg := awaitSendPayload(t, conn)
	require.Equal(t, WSEventMessageSent, msg.Type, "a replayed send whose message is already in the thread still needs its answer")
	raw, err := json.Marshal(msg.Payload)
	require.NoError(t, err)
	var sent MessageSentPayload
	require.NoError(t, json.Unmarshal(raw, &sent))
	require.Equal(t, "req-1", sent.RequestID)
	require.Equal(t, "m1", sent.Message.ID)

	others := awaitBroadcast(t, hub)
	require.Equal(t, WSEventMessageSent, others.event.Type)
	require.Equal(t, conn.ID, others.excludeConnID, "the other subscribers get the broadcast, the sender is not told twice")
}
