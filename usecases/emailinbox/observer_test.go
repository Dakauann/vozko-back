package emailinbox

import (
	"context"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/emailaddress"
	em "vozko/domain/emailinbox"
	"vozko/domain/emailsending"
	"vozko/domain/shared"
)

type observerWorld struct {
	messages      *fakeMessages
	contacts      *fakeContacts
	conversations *fakeConversations
	announcer     *fakeAnnouncer
	observer      *DeliveryObserver
}

func newObserverWorld() *observerWorld {
	w := &observerWorld{
		messages:      newFakeMessages(),
		contacts:      newFakeContacts(),
		conversations: newFakeConversations(),
		announcer:     &fakeAnnouncer{},
	}
	w.observer = NewDeliveryObserver(DeliveryObserverDeps{
		Messages: w.messages, Contacts: w.contacts, Conversations: w.conversations, Announcer: w.announcer,
	})
	return w
}

func (w *observerWorld) transcriptRow(entryID, external string, status conversation.DeliveryStatus) {
	w.messages.put(&conversation.Message{ID: "row-" + external, EntryID: entryID, EntryType: shared.EntryTypeEmail, ExternalMessageID: &external, DeliveryStatus: status})
}

func (w *observerWorld) recipientConversation(t *testing.T, workspaceID string, address emailaddress.Address, conversationID string) {
	t.Helper()
	contact, err := w.contacts.FindOrCreate(context.Background(), em.FindOrCreateContactInput{WorkspaceID: workspaceID, Address: address})
	if err != nil {
		t.Fatal(err)
	}
	w.conversations.add(&em.Conversation{ID: conversationID, WorkspaceID: workspaceID, ContactID: contact.ID})
}

func TestDeliveryObserverUpdatesReplyTranscript(t *testing.T) {
	cases := []struct {
		name    string
		runKind emailsending.RunKind
		current conversation.DeliveryStatus
		event   emailsending.EventKind
		changed bool
		want    conversation.DeliveryStatus
		saved   bool
	}{
		{"delivery marks delivered", emailsending.RunReply, conversation.DeliveryStatusSent, emailsending.EventDelivery, true, conversation.DeliveryStatusDelivered, true},
		{"bounce marks failed", emailsending.RunReply, conversation.DeliveryStatusSent, emailsending.EventBounce, true, conversation.DeliveryStatusFailed, true},
		{"bounce after delivery still fails", emailsending.RunReply, conversation.DeliveryStatusDelivered, emailsending.EventBounce, true, conversation.DeliveryStatusFailed, true},
		{"delivery never regresses a read message", emailsending.RunReply, conversation.DeliveryStatusRead, emailsending.EventDelivery, true, conversation.DeliveryStatusRead, false},
		{"opens are ignored", emailsending.RunReply, conversation.DeliveryStatusSent, emailsending.EventOpen, true, conversation.DeliveryStatusSent, false},
		{"campaign messages are not ours", emailsending.RunCampaign, conversation.DeliveryStatusSent, emailsending.EventDelivery, true, conversation.DeliveryStatusSent, false},
		{"test sends are not ours", emailsending.RunTest, conversation.DeliveryStatusSent, emailsending.EventDelivery, true, conversation.DeliveryStatusSent, false},
		{"unchanged events are ignored", emailsending.RunReply, conversation.DeliveryStatusSent, emailsending.EventDelivery, false, conversation.DeliveryStatusSent, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newObserverWorld()
			w.transcriptRow("conv-1", "ses-1", tc.current)

			err := w.observer.OnMessageEvent(context.Background(),
				emailsending.Message{RunKind: tc.runKind, RunID: "conv-1", ProviderMessageID: "ses-1"},
				emailsending.DeliveryEvent{Kind: tc.event},
				emailsending.EventEffect{Changed: tc.changed})
			if err != nil {
				t.Fatal(err)
			}
			if tc.saved != (len(w.messages.saved) == 1) {
				t.Fatalf("saved = %d", len(w.messages.saved))
			}
			if tc.saved && (w.messages.saved[0].DeliveryStatus != tc.want || len(w.announcer.updated) != 1) {
				t.Errorf("saved status = %s, announced %v", w.messages.saved[0].DeliveryStatus, w.announcer.updated)
			}
		})
	}
}

func TestDeliveryObserverUpdatesTheTranscriptOfOneToOneSends(t *testing.T) {
	cases := []struct {
		name  string
		event emailsending.EventKind
		want  conversation.DeliveryStatus
	}{
		{"delivery marks delivered", emailsending.EventDelivery, conversation.DeliveryStatusDelivered},
		{"bounce marks failed", emailsending.EventBounce, conversation.DeliveryStatusFailed},
		{"reject marks failed", emailsending.EventReject, conversation.DeliveryStatusFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newObserverWorld()
			w.recipientConversation(t, "ws-1", "ana@cliente.com", "conv-7")
			w.recipientConversation(t, "ws-2", "ana@cliente.com", "conv-other")
			w.transcriptRow("conv-7", "ses-9", conversation.DeliveryStatusSent)
			w.transcriptRow("conv-other", "ses-9", conversation.DeliveryStatusSent)

			err := w.observer.OnMessageEvent(context.Background(),
				emailsending.Message{WorkspaceID: "ws-1", Address: "ana@cliente.com", RunKind: emailsending.RunTriggered, RunID: "run-sha", ProviderMessageID: "ses-9"},
				emailsending.DeliveryEvent{Kind: tc.event},
				emailsending.EventEffect{Changed: true})
			if err != nil {
				t.Fatal(err)
			}
			if len(w.messages.saved) != 1 {
				t.Fatalf("saved = %d, want the one row of the recipient's conversation", len(w.messages.saved))
			}
			saved := w.messages.saved[0]
			if saved.EntryID != "conv-7" || saved.DeliveryStatus != tc.want {
				t.Fatalf("saved %s with %s, want conv-7 with %s", saved.EntryID, saved.DeliveryStatus, tc.want)
			}
			if len(w.announcer.updated) != 1 || w.announcer.updated[0] != "conv-7|"+string(tc.want) {
				t.Fatalf("announced %v", w.announcer.updated)
			}
		})
	}
}

func TestDeliveryObserverSkipsOneToOneSendsWithoutATranscript(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, w *observerWorld)
	}{
		{"unknown recipient", func(*testing.T, *observerWorld) {}},
		{"recipient without a conversation", func(t *testing.T, w *observerWorld) {
			if _, err := w.contacts.FindOrCreate(context.Background(), em.FindOrCreateContactInput{WorkspaceID: "ws-1", Address: "ana@cliente.com"}); err != nil {
				t.Fatal(err)
			}
		}},
		{"conversation without the row", func(t *testing.T, w *observerWorld) {
			w.recipientConversation(t, "ws-1", "ana@cliente.com", "conv-7")
			w.transcriptRow("conv-7", "ses-other", conversation.DeliveryStatusSent)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newObserverWorld()
			tc.setup(t, w)
			err := w.observer.OnMessageEvent(context.Background(),
				emailsending.Message{WorkspaceID: "ws-1", Address: "ana@cliente.com", RunKind: emailsending.RunTriggered, RunID: "run-sha", ProviderMessageID: "ses-9"},
				emailsending.DeliveryEvent{Kind: emailsending.EventDelivery}, emailsending.EventEffect{Changed: true})
			if err != nil {
				t.Fatalf("a send with no transcript row is not an error: %v", err)
			}
			if len(w.messages.saved) != 0 {
				t.Fatalf("saved = %d", len(w.messages.saved))
			}
		})
	}
}

func TestDeliveryObserverIgnoresUnknownMessages(t *testing.T) {
	w := newObserverWorld()
	err := w.observer.OnMessageEvent(context.Background(),
		emailsending.Message{RunKind: emailsending.RunReply, RunID: "conv-1", ProviderMessageID: "ses-forward"},
		emailsending.DeliveryEvent{Kind: emailsending.EventDelivery}, emailsending.EventEffect{Changed: true})
	if err != nil {
		t.Fatalf("a forwarded copy has no transcript row: %v", err)
	}
}
