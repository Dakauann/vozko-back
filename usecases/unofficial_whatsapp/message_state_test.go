package unofficial_whatsapp

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
)

type statefulMessages struct {
	conversation.MessageRepository
	mu      sync.Mutex
	byKey   map[string]*conversation.Message
	updated []string
}

func (s *statefulMessages) GetByEntryAndExternalMessageID(_ shared.EntryType, entryID, providerID string) (*conversation.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.byKey[entryID+":"+providerID]; ok {
		return m, nil
	}
	return nil, conversation.ErrMessageNotFound
}

func (s *statefulMessages) Update(messageID string, _ *conversation.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updated = append(s.updated, messageID)
	return nil
}

func (s *statefulMessages) UpdateDeliveryStatus(string, conversation.DeliveryStatus) error {
	return nil
}

func stateHarness(t *testing.T) (*importHarness, *statefulMessages, *conversation.Message) {
	t.Helper()
	h := newImportHarness(t)
	target := &conversation.Message{ID: "row-a1", EntryID: "conv-" + anaChat, EntryType: shared.EntryTypeUnofficialWhatsApp, Text: "oi"}
	messages := &statefulMessages{byKey: map[string]*conversation.Message{"conv-" + anaChat + ":a1": target}}
	h.uc.messages = messages
	return h, messages, target
}

func (h *importHarness) deliverLive(t *testing.T, event string, item map[string]any) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"EventType": event, "data": item})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.uc.Execute(context.Background(), &QueuedEvent{InstanceID: h.instance.ID, Body: body}); err != nil {
		t.Fatal(err)
	}
}

func reactionItem(id, emoji string, fromMe bool) map[string]any {
	item := map[string]any{
		"messageid": id, "chatid": anaChat, "fromMe": fromMe, "text": emoji, "reaction": "a1",
		"messageType": "reaction", "messageTimestamp": time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC).UnixMilli(),
	}
	if !fromMe {
		item["sender"] = anaChat
	}
	return item
}

func TestAReactionIsFoldedOntoTheMessageItReactsTo(t *testing.T) {
	h, messages, target := stateHarness(t)

	h.deliverLive(t, "messages", reactionItem("r1", "👍", false))
	h.deliverLive(t, "messages", reactionItem("r2", "🙏", true))

	reactions := target.Reactions()
	if len(reactions) != 2 || reactions[0].Emoji != "👍" || reactions[0].Direction != conversation.ReactionInbound || reactions[0].ActorID == "" ||
		reactions[1].Emoji != "🙏" || reactions[1].Direction != conversation.ReactionOutbound {
		t.Fatalf("reactions = %+v", reactions)
	}
	if !reactions[0].At.Equal(time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("at = %v, want the provider time", reactions[0].At)
	}
	if len(h.history.all()) != 0 {
		t.Fatalf("a reaction must not become a message of its own: %+v", h.history.all())
	}
	if len(messages.updated) != 2 || len(h.broadcasts.updatedMessages()) != 2 {
		t.Fatalf("updated=%v message_updated=%v", messages.updated, h.broadcasts.updatedMessages())
	}

	h.deliverLive(t, "messages", reactionItem("r3", "", false))
	if got := target.Reactions(); len(got) != 1 || got[0].Direction != conversation.ReactionOutbound {
		t.Fatalf("an empty emoji removes the customer's reaction: %+v", got)
	}
}

func TestAReactionToAMessageWeDoNotHaveIsDropped(t *testing.T) {
	h, messages, _ := stateHarness(t)
	item := reactionItem("r1", "👍", false)
	item["reaction"] = "unknown"

	h.deliverLive(t, "messages", item)

	if len(h.history.all()) != 0 || len(messages.updated) != 0 || len(h.broadcasts.updatedMessages()) != 0 {
		t.Fatalf("history=%v updated=%v", h.history.all(), messages.updated)
	}
}

func TestADeletedMessageIsRetractedAndAnEditReplacesTheText(t *testing.T) {
	h, messages, target := stateHarness(t)
	if _, err := h.convs.FindOrCreate(context.Background(), uw.FindOrCreateConversationInput{WorkspaceID: "ws-1", InstanceID: h.instance.ID, ChatID: anaChat}); err != nil {
		t.Fatal(err)
	}

	h.deliverLive(t, "messages_update", map[string]any{"messageid": "a1", "chatid": anaChat, "status": "Sent", "edited": "2026-10-18T12:05:00Z", "text": "olá"})
	if target.Text != "olá" || target.EditedAt == nil {
		t.Fatalf("text = %q edited_at = %v", target.Text, target.EditedAt)
	}

	h.deliverLive(t, "messages_update", map[string]any{"messageid": "a1", "chatid": anaChat, "status": "Deleted"})
	if !target.IsRetracted() {
		t.Fatal("a revoked message must be marked retracted")
	}
	if len(messages.updated) != 2 || len(h.broadcasts.updatedMessages()) != 2 {
		t.Fatalf("updated=%v message_updated=%v", messages.updated, h.broadcasts.updatedMessages())
	}
}

func (b *entryUpdates) BroadcastMessageUpdated(_, _ string, message *conversation.Message) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.updated = append(b.updated, message.ID)
}

func (b *entryUpdates) updatedMessages() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.updated...)
}

func (b *entryUpdates) BroadcastMessageStatus(string, string, string, conversation.DeliveryStatus) {}
