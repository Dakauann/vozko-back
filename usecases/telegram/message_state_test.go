package telegram

import (
	"context"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	tgdomain "vozko/domain/telegram"
)

type storedTelegramMessages struct {
	conversation.MessageRepository
	byProviderID map[string]*conversation.Message
	updated      []string
	deleted      []string
}

func (s *storedTelegramMessages) GetByExternalMessageID(_ shared.EntryType, providerID string) (*conversation.Message, error) {
	if m, ok := s.byProviderID[providerID]; ok {
		return m, nil
	}
	return nil, conversation.ErrMessageNotFound
}

func (s *storedTelegramMessages) Update(messageID string, _ *conversation.Message) error {
	s.updated = append(s.updated, messageID)
	return nil
}

func (s *storedTelegramMessages) Delete(messageID string) error {
	s.deleted = append(s.deleted, messageID)
	return nil
}

type threadBroadcasts struct {
	conversation.EventBroadcaster
	updated []string
	entries []string
}

func (b *threadBroadcasts) BroadcastMessageUpdated(_, _ string, message *conversation.Message) {
	b.updated = append(b.updated, message.ID)
}

func (b *threadBroadcasts) BroadcastEntryUpdate(entryID, _ string, _ *conversation.Message) {
	b.entries = append(b.entries, entryID)
}

func telegramStateFixture() (*HandleWebhookUseCase, *storedTelegramMessages, *threadBroadcasts, *tgdomain.Account) {
	account := &tgdomain.Account{ID: "acc-1", BotUserID: 100}
	stored := &conversation.Message{ID: "row-1", EntryID: "conv-1", EntryType: shared.EntryTypeTelegram, Text: "oi"}
	messages := &storedTelegramMessages{byProviderID: map[string]*conversation.Message{tgdomain.ProviderMessageID(100, 42, 7): stored}}
	broadcasts := &threadBroadcasts{}
	return &HandleWebhookUseCase{messages: messages, broadcaster: broadcasts}, messages, broadcasts, account
}

func TestATelegramEditUpdatesTheTextStampsTheEditAndReachesTheOpenThread(t *testing.T) {
	uc, messages, broadcasts, account := telegramStateFixture()
	at := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

	if err := uc.handleEdited(context.Background(), account, &tgdomain.Event{ChatID: 42, MessageID: 7, Text: "olá", Timestamp: at}); err != nil {
		t.Fatal(err)
	}

	edited := messages.byProviderID[tgdomain.ProviderMessageID(100, 42, 7)]
	if edited.Text != "olá" || edited.EditedAt == nil || !edited.EditedAt.Equal(at) {
		t.Fatalf("text = %q edited_at = %v", edited.Text, edited.EditedAt)
	}
	if len(messages.updated) != 1 || len(broadcasts.updated) != 1 || len(broadcasts.entries) != 1 {
		t.Fatalf("updated=%v message_updated=%v entry_update=%v", messages.updated, broadcasts.updated, broadcasts.entries)
	}
}

func TestATelegramDeleteRetractsTheMessageInsteadOfRemovingIt(t *testing.T) {
	uc, messages, broadcasts, account := telegramStateFixture()

	if err := uc.handleDeleted(context.Background(), account, &tgdomain.Event{ChatID: 42, DeletedMessageIDs: []int64{7, 8}}); err != nil {
		t.Fatal(err)
	}

	retracted := messages.byProviderID[tgdomain.ProviderMessageID(100, 42, 7)]
	if !retracted.IsRetracted() {
		t.Fatal("the deleted message must be marked retracted")
	}
	if len(messages.deleted) != 0 || len(messages.updated) != 1 || len(broadcasts.updated) != 1 {
		t.Fatalf("deleted=%v updated=%v message_updated=%v", messages.deleted, messages.updated, broadcasts.updated)
	}
}

func (s *storedTelegramMessages) GetByEntryAndExternalMessageID(_ shared.EntryType, entryID, providerID string) (*conversation.Message, error) {
	if m, ok := s.byProviderID[providerID]; ok && m.EntryID == entryID {
		return m, nil
	}
	return nil, conversation.ErrMessageNotFound
}

type chatConversations struct {
	*fakeConversations
	byChat map[int64]*tgdomain.Conversation
}

func (c *chatConversations) FindByChat(_ context.Context, accountID string, chatID int64) (*tgdomain.Conversation, error) {
	if conv, ok := c.byChat[chatID]; ok && conv.AccountID == accountID {
		return conv, nil
	}
	return nil, tgdomain.ErrConversationNotFound
}

func telegramReactionFixture() (*HandleWebhookUseCase, *storedTelegramMessages, *threadBroadcasts, *tgdomain.Account) {
	uc, messages, broadcasts, account := telegramStateFixture()
	uc.conversations = &chatConversations{
		fakeConversations: &fakeConversations{},
		byChat: map[int64]*tgdomain.Conversation{
			42: {ID: "conv-1", AccountID: account.ID, TGChatID: 42},
			43: {ID: "conv-2", AccountID: account.ID, TGChatID: 43},
		},
	}
	return uc, messages, broadcasts, account
}

func reactionEvent(chatID, messageID int64, emoji string) *tgdomain.Event {
	return &tgdomain.Event{
		Kind:          tgdomain.EventReaction,
		ChatID:        chatID,
		MessageID:     messageID,
		From:          &tgdomain.User{ID: 42, FirstName: "Ana"},
		ReactionEmoji: emoji,
		Timestamp:     time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC),
	}
}

func TestACustomerReactionFoldsOntoTheTargetAsAnInboundReactionFromThatUser(t *testing.T) {
	uc, messages, broadcasts, account := telegramReactionFixture()

	if err := uc.handleEvent(context.Background(), account, reactionEvent(42, 7, "👍")); err != nil {
		t.Fatal(err)
	}

	reactions := messages.byProviderID[tgdomain.ProviderMessageID(100, 42, 7)].Reactions()
	if len(reactions) != 1 {
		t.Fatalf("reactions = %+v, want the customer's reaction on the target", reactions)
	}
	got := reactions[0]
	if got.Emoji != "👍" || got.Direction != conversation.ReactionInbound || got.ActorID != "42" {
		t.Fatalf("reaction = %+v, want an inbound 👍 by Telegram user 42", got)
	}
	if len(messages.updated) != 1 || len(broadcasts.updated) != 1 || len(broadcasts.entries) != 1 {
		t.Fatalf("updated=%v message_updated=%v entry_update=%v", messages.updated, broadcasts.updated, broadcasts.entries)
	}
}

func TestAnEmptyReactionRemovesTheCustomersReaction(t *testing.T) {
	uc, messages, _, account := telegramReactionFixture()
	ctx := context.Background()

	if err := uc.handleEvent(ctx, account, reactionEvent(42, 7, "👍")); err != nil {
		t.Fatal(err)
	}
	if err := uc.handleEvent(ctx, account, reactionEvent(42, 7, "")); err != nil {
		t.Fatal(err)
	}

	if reactions := messages.byProviderID[tgdomain.ProviderMessageID(100, 42, 7)].Reactions(); len(reactions) != 0 {
		t.Fatalf("reactions = %+v, want none after the customer removed theirs", reactions)
	}
}

func TestAReactionThatCannotBePlacedIsDroppedWithoutTouchingAnyMessage(t *testing.T) {
	cases := []struct {
		name string
		ev   *tgdomain.Event
	}{
		{"a message the inbox never stored", reactionEvent(42, 999, "👍")},
		{"a chat with no conversation", reactionEvent(44, 7, "👍")},
		{"a stored message that belongs to another conversation", reactionEvent(43, 7, "👍")},
		{"a custom or paid reaction", func() *tgdomain.Event {
			ev := reactionEvent(42, 7, "")
			ev.Kind = tgdomain.EventUnsupportedReaction
			return ev
		}()},
		{"a reaction with no user", func() *tgdomain.Event {
			ev := reactionEvent(42, 7, "👍")
			ev.From = nil
			return ev
		}()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uc, messages, broadcasts, account := telegramReactionFixture()
			messages.byProviderID[tgdomain.ProviderMessageID(100, 43, 7)] = messages.byProviderID[tgdomain.ProviderMessageID(100, 42, 7)]

			if err := uc.handleEvent(context.Background(), account, tc.ev); err != nil {
				t.Fatal(err)
			}

			if len(messages.updated) != 0 || len(broadcasts.updated) != 0 {
				t.Fatalf("updated=%v message_updated=%v, want nothing saved", messages.updated, broadcasts.updated)
			}
			if reactions := messages.byProviderID[tgdomain.ProviderMessageID(100, 42, 7)].Reactions(); len(reactions) != 0 {
				t.Fatalf("reactions = %+v, want the target untouched", reactions)
			}
		})
	}
}
