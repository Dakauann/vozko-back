package instagram

import (
	"context"
	"testing"

	"vozko/domain/conversation"
	igdomain "vozko/domain/instagram"
	"vozko/domain/shared"
)

// A provider message id is unique per conversation, not per platform. When both
// ends of a thread are accounts we host — one tenant messaging another — the same
// mid lives on two entries, and an edit, delete, reaction or read receipt that
// matched channel-wide could land on the OTHER tenant's copy.
//
// The lookup therefore prefers this account's own conversation, and falls back
// to the channel-wide match only when no contact resolves from the event, which
// is what happens when the BUSINESS itself raised it (unsending its own
// message). Both halves matter: scoping without the fallback would silently stop
// tombstoning business-unsent messages.

type scopedLookupMessages struct {
	conversation.MessageRepository

	entryScoped []string // entry ids the scoped lookup was called with
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
	// The channel-wide query is what could reach another tenant's row.
	if msgs.channelWide != 0 {
		t.Errorf("channel-wide lookup must not run when the entry is known, ran %d time(s)", msgs.channelWide)
	}
}

// An event the business raised names the business as sender, so no contact
// resolves. Before scoping, these worked; they must keep working.
func TestMessageLookupFallsBackWhenNoContactResolves(t *testing.T) {
	for _, tc := range []struct {
		name     string
		contacts *fakeContactRepo
		convs    *fakeConversationRepo
		event    *igdomain.Event
	}{
		{
			name:     "sender is the business, so no contact row",
			contacts: &fakeContactRepo{}, // FindByIGSID → ErrContactNotFound
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
			convs: &fakeConversationRepo{}, // FindByContact → ErrConversationNotFound
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

// findConversation must not create anything: an edit or a delete names a message
// that must already exist, and resolving it must not leave an empty conversation
// behind for a thread we never held.
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
