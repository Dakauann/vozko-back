package ws

import (
	"testing"

	"github.com/stretchr/testify/require"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

func newIdentityHub(t *testing.T, provider *statusTestHistoryProvider) *ConversationHub {
	t.Helper()
	hub := NewConversationHub(&hubDepartmentTestAuthorizer{entryAccess: map[string]bool{}}, nil, nil, nil, "test-replica", "")
	hub.historyProvider = provider
	return hub
}

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
		From:        "6979451734",
		Text:        "Oi tudo bem",
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
	require.Zero(t, provider.resolveCalls,
		"a producer-supplied name must not trigger a database lookup")
}

func TestBroadcastSurvivesWithoutAHistoryProvider(t *testing.T) {
	hub := NewConversationHub(&hubDepartmentTestAuthorizer{entryAccess: map[string]bool{}}, nil, nil, nil, "test-replica", "")

	hub.BroadcastNewMessage("entry-1", string(shared.EntryTypeTelegram), &conversation.Message{
		ID: "m1", EntryID: "entry-1", EntryType: shared.EntryTypeTelegram,
		MessageType: conversation.MessageTypeUserMessage, From: "6979451734",
	})

	require.NotNil(t, drainBroadcast(t, hub))
}
