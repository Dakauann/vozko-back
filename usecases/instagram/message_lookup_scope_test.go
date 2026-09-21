package instagram

import (
	"context"
	"testing"

	"vozko/domain/conversation"
	igdomain "vozko/domain/instagram"
	"vozko/domain/shared"
)

type scopedLookupMessages struct {
	conversation.MessageRepository

	entryScoped []string
	channelWide int

	found *conversation.Message
}

func (m *scopedLookupMessages) GetByEntryAndExternalMessageID(_ shared.EntryType, entryID, _ string) (*conversation.Message, error) {
	m.entryScoped = append(m.entryScoped, entryID)
	if m.found == nil {
		return nil, conversation.ErrMessageNotFound
	}
	return m.found, nil
}

func (m *scopedLookupMessages) GetByExternalMessageID(shared.EntryType, string) (*conversation.Message, error) {
	m.channelWide++
	if m.found == nil {
		return nil, conversation.ErrMessageNotFound
	}
	return m.found, nil
}

func lookupUC(msgs conversation.MessageRepository, contacts *fakeContactRepo, convs *fakeConversationRepo) *HandleWebhookUseCase {
	return NewHandleWebhookUseCase(HandleWebhookDeps{
		Contacts:      contacts,
		Conversations: convs,
		Messages:      msgs,
	})
}

func TestMessageLookupPrefersThisAccountsConversation(t *testing.T) {
	msgs := &scopedLookupMessages{found: &conversation.Message{ID: "m1", EntryID: "conv-mine"}}
	contacts := &fakeContactRepo{
		FindByIGSIDFn: func(_ context.Context, _, igsid string) (*igdomain.Contact, error) {
			return &igdomain.Contact{ID: "contact-1", IGSID: igsid}, nil
		},
	}
	convs := &fakeConversationRepo{
		FindByContactFn: func(context.Context, string, string) (*igdomain.Conversation, error) {
			return &igdomain.Conversation{ID: "conv-mine"}, nil
		},
	}

	uc := lookupUC(msgs, contacts, convs)
	got, err := uc.messageByProviderID(context.Background(),
		&igdomain.Account{ID: "acc-1"}, &igdomain.Event{ContactIGSID: "igsid-1"}, "mid-shared")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got == nil || got.ID != "m1" {
		t.Fatalf("expected the message, got %+v", got)
	}

	if len(msgs.entryScoped) != 1 || msgs.entryScoped[0] != "conv-mine" {
		t.Errorf("must query this account's entry, got %v", msgs.entryScoped)
	}
	if msgs.channelWide != 0 {
		t.Errorf("channel-wide lookup must not run when the entry is known, ran %d time(s)", msgs.channelWide)
	}
}

func TestMessageLookupFallsBackWhenNoContactResolves(t *testing.T) {
	for _, tc := range []struct {
		name     string
		contacts *fakeContactRepo
		convs    *fakeConversationRepo
		event    *igdomain.Event
	}{
		{
			name:     "sender is the business, so no contact row",
			contacts: &fakeContactRepo{},
			convs:    &fakeConversationRepo{},
			event:    &igdomain.Event{ContactIGSID: "business-igsid"},
		},
		{
			name: "contact known but no conversation yet",
			contacts: &fakeContactRepo{
				FindByIGSIDFn: func(_ context.Context, _, igsid string) (*igdomain.Contact, error) {
					return &igdomain.Contact{ID: "contact-1", IGSID: igsid}, nil
				},
			},
			convs: &fakeConversationRepo{},
			event: &igdomain.Event{ContactIGSID: "igsid-1"},
		},
		{
			name:     "event carries no contact at all",
			contacts: &fakeContactRepo{},
			convs:    &fakeConversationRepo{},
			event:    &igdomain.Event{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msgs := &scopedLookupMessages{found: &conversation.Message{ID: "m1"}}
			uc := lookupUC(msgs, tc.contacts, tc.convs)

			got, err := uc.messageByProviderID(context.Background(),
				&igdomain.Account{ID: "acc-1"}, tc.event, "mid-1")
			if err != nil {
				t.Fatalf("lookup: %v", err)
			}
			if got == nil {
				t.Fatal("the message must still be found, or the tombstone is silently skipped")
			}
			if msgs.channelWide != 1 {
				t.Errorf("expected the channel-wide fallback, ran %d time(s)", msgs.channelWide)
			}
			if len(msgs.entryScoped) != 0 {
				t.Errorf("no entry is known, so nothing should be queried by entry, got %v", msgs.entryScoped)
			}
		})
	}
}

func TestFindConversationNeverCreates(t *testing.T) {
	contacts := &fakeContactRepo{}
	convs := &fakeConversationRepo{
		FindOrCreateFn: func(context.Context, string, string, string) (*igdomain.Conversation, error) {
			t.Fatal("FindOrCreate must not be reached from the lookup path")
			return nil, nil
		},
	}
	contacts.FindOrCreateFn = func(context.Context, string, string, string) (*igdomain.Contact, error) {
		t.Fatal("FindOrCreate must not be reached from the lookup path")
		return nil, nil
	}

	uc := lookupUC(&scopedLookupMessages{}, contacts, convs)
	if _, err := uc.findConversation(context.Background(),
		&igdomain.Account{ID: "acc-1"}, "unknown-igsid"); err == nil {
		t.Error("an unknown contact must miss, not be created")
	}
}
