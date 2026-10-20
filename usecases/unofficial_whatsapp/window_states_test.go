package unofficial_whatsapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/conversation"
	uw "vozko/domain/unofficial_whatsapp"
)

type windowFixture struct {
	adapter       conversation.ChannelAdapter
	instances     *fakeInstanceRepo
	contacts      *fakeContactRepo
	conversations *fakeConversationRepo
}

func newWindowFixture(mutate func(*uw.Instance, *uw.Contact)) windowFixture {
	instance := &uw.Instance{ID: "inst-1", WorkspaceID: "ws-1", ServerID: "srv-a", Status: uw.StatusConnected, InstanceToken: "tok"}
	contact := &uw.Contact{ID: "contact-1", WorkspaceID: "ws-1", InstanceID: "inst-1", JID: "5511999999999@s.whatsapp.net"}
	other := &uw.Contact{ID: "contact-2", WorkspaceID: "ws-1", InstanceID: "inst-1", JID: "5511888888888@s.whatsapp.net"}
	if mutate != nil {
		mutate(instance, contact)
	}
	instances := newFakeInstanceRepo(instance)
	contacts := newFakeContactRepo(contact, other)
	conversations := newFakeConversationRepo(
		&uw.Conversation{ID: "conv-1", WorkspaceID: "ws-1", InstanceID: "inst-1", ContactID: "contact-1", ChatID: contact.JID},
		&uw.Conversation{ID: "conv-2", WorkspaceID: "ws-1", InstanceID: "inst-1", ContactID: "contact-2", ChatID: other.JID},
	)
	return windowFixture{
		adapter:       NewChannelAdapter(instances, newFakeServerRepo(healthyServer("srv-a", 10, 1)), contacts, conversations, &fakeMessaging{}),
		instances:     instances,
		contacts:      contacts,
		conversations: conversations,
	}
}

func (f windowFixture) batch(t *testing.T) conversation.WindowStatesAdapter {
	t.Helper()
	batch, ok := f.adapter.(conversation.WindowStatesAdapter)
	if !ok {
		t.Fatal("the unofficial whatsapp adapter must report window states for a page of entries")
	}
	return batch
}

func TestWindowStatesMatchTheSingleEntryRule(t *testing.T) {
	until := time.Now().UTC().Add(time.Hour)
	denied := false
	cases := []struct {
		name       string
		mutate     func(*uw.Instance, *uw.Contact)
		wantOpen   bool
		wantReason conversation.WindowClosedReason
		wantUntil  bool
	}{
		{name: "open", wantOpen: true},
		{name: "session down", mutate: func(i *uw.Instance, _ *uw.Contact) { i.Status = uw.StatusDisconnected }, wantReason: conversation.WindowReasonSessionDown},
		{name: "restricted until a time", mutate: func(i *uw.Instance, _ *uw.Contact) { i.Restriction.Until = &until }, wantReason: conversation.WindowReasonAccountRestricted, wantUntil: true},
		{name: "new chats denied", mutate: func(i *uw.Instance, _ *uw.Contact) { i.Restriction.CanSendNewChats = &denied }, wantReason: conversation.WindowReasonAccountRestricted},
		{name: "contact blocked", mutate: func(_ *uw.Instance, c *uw.Contact) { c.Blocked = true }, wantReason: conversation.WindowReasonContactBlocked},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newWindowFixture(tc.mutate)
			ctx := context.Background()
			ec, err := f.adapter.ResolveEntry(ctx, "conv-1")
			if err != nil {
				t.Fatal(err)
			}
			single, err := f.adapter.WindowState(ctx, ec)
			if err != nil {
				t.Fatal(err)
			}
			states, err := f.batch(t).WindowStates(ctx, []string{"conv-1"})
			if err != nil {
				t.Fatal(err)
			}
			got, ok := states["conv-1"]
			if !ok {
				t.Fatal("no state for conv-1")
			}
			if got.Open != tc.wantOpen || got.Reason != tc.wantReason || (got.ExpiresAt != nil) != tc.wantUntil {
				t.Fatalf("batch = %+v, want open=%v reason=%q until=%v", got, tc.wantOpen, tc.wantReason, tc.wantUntil)
			}
			if !got.Equal(single) {
				t.Fatalf("batch %+v disagrees with the single entry %+v", got, single)
			}
		})
	}
}

func TestWindowStatesLoadAPageInOneBatchPerTable(t *testing.T) {
	f := newWindowFixture(nil)

	states, err := f.batch(t).WindowStates(context.Background(), []string{"conv-1", "conv-2", "conv-1", "conv-gone"})
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 2 || !states["conv-1"].Open || !states["conv-2"].Open {
		t.Fatalf("states = %+v", states)
	}
	if f.conversations.findByIDsCalls != 1 || f.instances.findByIDsCalls != 1 || f.contacts.findByIDsCalls != 1 {
		t.Fatalf("batches: conversations %d, instances %d, contacts %d, want 1 each",
			f.conversations.findByIDsCalls, f.instances.findByIDsCalls, f.contacts.findByIDsCalls)
	}
	if f.conversations.findByIDCalls != 0 || f.instances.findByIDCalls != 0 || f.contacts.findByIDCalls != 0 {
		t.Fatalf("per row lookups: conversations %d, instances %d, contacts %d",
			f.conversations.findByIDCalls, f.instances.findByIDCalls, f.contacts.findByIDCalls)
	}
}

func TestWindowStatesSkipContactsWhenTheSessionIsDown(t *testing.T) {
	f := newWindowFixture(func(i *uw.Instance, _ *uw.Contact) { i.Status = uw.StatusDisconnected })

	if _, err := f.batch(t).WindowStates(context.Background(), []string{"conv-1"}); err != nil {
		t.Fatal(err)
	}
	if f.contacts.findByIDsCalls != 0 {
		t.Fatalf("read %d contact batches though no row reaches the contact rule", f.contacts.findByIDsCalls)
	}
}

func TestWindowStatesCloseEntriesWithAMissingInstanceOrContact(t *testing.T) {
	for _, missing := range []string{"instance", "contact"} {
		t.Run(missing, func(t *testing.T) {
			f := newWindowFixture(nil)
			if missing == "instance" {
				delete(f.instances.instances, "inst-1")
			} else {
				delete(f.contacts.contacts, "contact-1")
			}
			states, err := f.batch(t).WindowStates(context.Background(), []string{"conv-1"})
			if err != nil {
				t.Fatal(err)
			}
			if got := states["conv-1"]; got.Open || got.Reason != conversation.WindowReasonChannelUnavailable {
				t.Fatalf("state = %+v, want closed channel_unavailable", got)
			}
		})
	}
}

func TestWindowStatesReturnLookupErrors(t *testing.T) {
	boom := errors.New("db down")
	for _, broken := range []string{"conversations", "instances", "contacts"} {
		t.Run(broken, func(t *testing.T) {
			f := newWindowFixture(nil)
			switch broken {
			case "conversations":
				f.conversations.findByIDsErr = boom
			case "instances":
				f.instances.findByIDsErr = boom
			default:
				f.contacts.findByIDsErr = boom
			}
			if _, err := f.batch(t).WindowStates(context.Background(), []string{"conv-1"}); !errors.Is(err, boom) {
				t.Fatalf("err = %v, want the lookup error", err)
			}
		})
	}
}
