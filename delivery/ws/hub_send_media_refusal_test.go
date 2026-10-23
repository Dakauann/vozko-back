package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type noteImageInEntry struct{}

func (noteImageInEntry) GetInEntry(entryID string, entryType shared.EntryType, id string) (*conversation.ConversationMedia, error) {
	return &conversation.ConversationMedia{
		ID: id, EntryID: entryID, EntryType: entryType, Type: conversation.MediaTypeImage,
		URL: "https://cdn/note.png", Purpose: conversation.MediaPurposeNote,
	}, nil
}

type mediaCheckingSend struct{}

func (mediaCheckingSend) Execute(_ context.Context, in conversation.OperatorSendInput) (*conversation.Message, error) {
	if _, err := conversation.SendableUpload(noteImageInEntry{}, in.EntryID, shared.EntryType(in.EntryType), in.MediaID); err != nil {
		return nil, err
	}
	return &conversation.Message{ID: "m1"}, nil
}

type wrappingResend struct{ err error }

func (r wrappingResend) Execute(context.Context, shared.Person, conversation.MessageTarget, string) (*conversation.Message, error) {
	return nil, fmt.Errorf("%w: %w", conversation.ErrResendDeliveryFailed, r.err)
}

func messageErrorOf(t *testing.T, msg WSOutgoingMessage) MessageErrorPayload {
	t.Helper()
	require.Equal(t, WSEventMessageError, msg.Type)
	body, err := json.Marshal(msg.Payload)
	require.NoError(t, err)
	var payload MessageErrorPayload
	require.NoError(t, json.Unmarshal(body, &payload))
	return payload
}

func TestHandleSend_ANoteImageIsRefusedWithTheMediaNotSendableCode(t *testing.T) {
	hub, conn := newSendHub(t, mediaCheckingSend{})

	mediaID, mediaType := "note-image-1", "image"
	payload, err := json.Marshal(SendMessagePayload{
		EntryID:   "entry-1",
		EntryType: "whatsapp",
		RequestID: "req-1",
		MediaID:   &mediaID,
		MediaType: &mediaType,
	})
	require.NoError(t, err)

	hub.handleSend(conn, payload)

	refusal := messageErrorOf(t, awaitSendPayload(t, conn))
	require.Equal(t, conversation.MediaNotSendableCode, refusal.Code)
	require.Equal(t, "req-1", refusal.RequestID)
}

func TestHandleResend_KeepsTheMediaNotSendableCode(t *testing.T) {
	hub, conn := newSendHub(t, panicSendTestUseCase{})
	_, refused := conversation.SendableUpload(noteImageInEntry{}, "entry-1", shared.EntryTypeWhatsApp, "note-image-1")
	hub.SetMessageActions(conversation.MessageActionUseCases{Resend: wrappingResend{err: refused}})

	payload, err := json.Marshal(MessageActionPayload{EntryID: "entry-1", EntryType: "whatsapp", MessageID: "msg-1", RequestID: "req-2"})
	require.NoError(t, err)

	hub.handleResendMessage(conn, payload)

	refusal := messageErrorOf(t, awaitSendPayload(t, conn))
	require.Equal(t, conversation.MediaNotSendableCode, refusal.Code)
	require.Equal(t, "req-2", refusal.RequestID)
}
