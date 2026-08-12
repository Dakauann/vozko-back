package ws

import (
	"testing"

	"github.com/stretchr/testify/require"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

// A message that arrives while the conversation is open is broadcast straight
// from the producer, while a reload resolves the sender through the history
// provider. The two paths disagreed: the live one shipped whatever `From`
// holds. On WhatsApp that is a phone number and merely looked unpolished; on
// Telegram it is a bare numeric user id, so the CRM showed "6979451734" until
// the operator reloaded.
//
// These pin both halves of the fix: the producer's label is used as-is, and a
// producer that supplies nothing still gets a resolved name.

func newIdentityHub(t *testing.T, provider *statusTestHistoryProvider) *ConversationHub {
	t.Helper()
	hub := NewConversationHub(&hubDepartmentTestAuthorizer{entryAccess: map[string]bool{}}, nil, nil, nil, "test-replica", "")
	hub.historyProvider = provider
	return hub
}

// drainBroadcast takes the queued event without starting the pump, so the test
// observes exactly the payload the hub built.
func drainBroadcast(t *testing.T, hub *ConversationHub) *conversation.Message {
	t.Helper()
	select {
	case bm := <-hub.broadcast:
		payload, ok := bm.event.Payload.(MessagePayload)
		require.True(t, ok, "new-message broadcast must carry a MessagePayload")
		return payload.Message
	default:
		t.Fatal("expected a broadcast to be queued")
		return nil
	}
}

func TestBroadcastResolvesSenderIdentityWhenTheProducerSuppliedNone(t *testing.T) {
	provider := &statusTestHistoryProvider{resolvedName: "Dakauann"}
	hub := newIdentityHub(t, provider)

	hub.BroadcastNewMessage("entry-1", string(shared.EntryTypeTelegram), &conversation.Message{
		ID:          "m1",
		EntryID:     "entry-1",
		EntryType:   shared.EntryTypeTelegram,
		MessageType: conversation.MessageTypeUserMessage,
		// What a Telegram inbound row actually carries: the raw user id.
		From: "6979451734",
		Text: "Oi tudo bem",
	})

	msg := drainBroadcast(t, hub)
	require.Equal(t, "Dakauann", msg.SenderName,
		"a live message must not reach the CRM labelled with the raw provider id")
	require.Equal(t, 1, provider.resolveCalls)
}

func TestBroadcastKeepsTheProducersLabelAndSkipsTheLookup(t *testing.T) {
	provider := &statusTestHistoryProvider{resolvedName: "resolved-by-fallback"}
	hub := newIdentityHub(t, provider)

	hub.BroadcastNewMessage("entry-1", string(shared.EntryTypeTelegram), &conversation.Message{
		ID:          "m1",
		EntryID:     "entry-1",
		EntryType:   shared.EntryTypeTelegram,
		MessageType: conversation.MessageTypeUserMessage,
		From:        "6979451734",
		SenderName:  "Dakauann",
	})

	msg := drainBroadcast(t, hub)
	require.Equal(t, "Dakauann", msg.SenderName)
	// The point of threading the name through the producer is that the inbound
	// path already loaded the contact. If the hub queried anyway the work would
	// be duplicated on every single inbound message.
	require.Zero(t, provider.resolveCalls,
		"a producer-supplied name must not trigger a database lookup")
}

// Without a provider the hub must still deliver the message. Broadcasting is
// not worth failing over an identity we could not resolve.
func TestBroadcastSurvivesWithoutAHistoryProvider(t *testing.T) {
	hub := NewConversationHub(&hubDepartmentTestAuthorizer{entryAccess: map[string]bool{}}, nil, nil, nil, "test-replica", "")

	hub.BroadcastNewMessage("entry-1", string(shared.EntryTypeTelegram), &conversation.Message{
		ID: "m1", EntryID: "entry-1", EntryType: shared.EntryTypeTelegram,
		MessageType: conversation.MessageTypeUserMessage, From: "6979451734",
	})

	require.NotNil(t, drainBroadcast(t, hub))
}
