package emailinbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/emailaddress"
	em "vozko/domain/emailinbox"
	"vozko/domain/emailsending"
	"vozko/domain/emailtoken"
	"vozko/domain/shared"
)

const (
	leadAna       = "1ead0000-0000-0000-0000-00000000000a"
	senderSupport = "5e000000-0000-0000-0000-0000000000a2"
	triggeredMsg  = "c0000000-0000-0000-0000-000000000010"
	anaAddress    = emailaddress.Address("ana@cliente.com")
)

func (w *world) transcript() *OutboundTranscript {
	return NewOutboundTranscript(OutboundTranscriptDeps{
		Contacts: w.contacts, Conversations: w.convs, Inboxes: w.inboxes, Senders: w.senders,
		Domains: w.domains, History: w.history, Messages: w.messages, Media: w.media,
	})
}

func (w *world) addSupportSender() {
	w.senders.all = append(w.senders.all, emailsending.Sender{ID: senderSupport, WorkspaceID: wsA, DomainID: "d-a", LocalPart: "suporte", DisplayName: "Suporte A"})
}

func sentEmail(by conversation.SentBy) em.OutboundEmail {
	return em.OutboundEmail{
		WorkspaceID: wsA,
		LeadID:      leadAna,
		LeadName:    "Ana Souza",
		To:          anaAddress,
		Subject:     "Proposta de outubro",
		Text:        "Oi Ana, segue a proposta.",
		ExternalID:  "ses-out-1",
		SentBy:      by,
		SentAt:      time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC),
	}
}

func everyBusinessSender() []struct {
	name string
	by   conversation.SentBy
	kind conversation.MessageType
} {
	return []struct {
		name string
		by   conversation.SentBy
		kind conversation.MessageType
	}{
		{"operator", conversation.SentByPerson("8a1f3c2e-6b7d-4e5f-9a0b-1c2d3e4f5a6b"), conversation.MessageTypeOperator},
		{"workflow", conversation.SentByWorkflow("wf-1"), conversation.MessageTypeAIResponse},
		{"agent", conversation.SentByAI("agent-1"), conversation.MessageTypeAIResponse},
		{"assistant", conversation.SentByAI(""), conversation.MessageTypeAIResponse},
	}
}

func TestTranscribeRecordsOneRowThatTheLaterReplyThreadsInto(t *testing.T) {
	for _, tc := range everyBusinessSender() {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			ctx := context.Background()
			id, err := w.transcript().Transcribe(ctx, sentEmail(tc.by))
			if err != nil || id == "" {
				t.Fatalf("transcribe = %q, %v", id, err)
			}
			again, err := w.transcript().Transcribe(ctx, sentEmail(tc.by))
			if err != nil || again != id {
				t.Fatalf("a repeated transcript = %q, %v, want %q", again, err, id)
			}
			if len(w.history.records) != 1 {
				t.Fatalf("records = %d, want exactly 1", len(w.history.records))
			}
			rec := w.history.records[0]
			if rec.EntryID != id || rec.EntryType != shared.EntryTypeEmail || rec.Channel != conversation.MessageChannelEmail ||
				rec.MessageType != tc.kind || rec.SentBy != tc.by || rec.ProviderMessageID != "ses-out-1" ||
				rec.From != "vendas@loja-a.com.br" || rec.To != string(anaAddress) || rec.Text != "Oi Ana, segue a proposta." {
				t.Fatalf("record = %+v", rec)
			}
			if meta := metadataOf(t, rec); meta[em.MetadataSubject] != "Proposta de outubro" {
				t.Fatalf("metadata = %v", meta)
			}
			conv := w.onlyConversation()
			if conv.Subject != "Proposta de outubro" || conv.SenderID != senderA || len(w.convs.outbound) == 0 {
				t.Fatalf("conversation = %+v, outbound clock %v", conv, w.convs.outbound)
			}
			contact, _ := w.contacts.FindByAddress(ctx, wsA, anaAddress)
			if contact == nil || contact.LeadID == nil || *contact.LeadID != leadAna || contact.DisplayName != "Ana Souza" {
				t.Fatalf("contact = %+v", contact)
			}

			w.sent.byID[triggeredMsg] = &emailsending.Message{ID: triggeredMsg, WorkspaceID: wsA, DomainID: "d-a", RunKind: emailsending.RunTriggered, RunID: "run-hash", Address: anaAddress}
			if err := w.receive(reply(anaAddress, "r1@cliente.com"), w.tagged(emailtoken.PurposeReply, triggeredMsg, replyA)); err != nil {
				t.Fatal(err)
			}
			if w.onlyConversation().ID != id || len(w.history.records) != 2 || !w.history.records[1].SentBy.IsContact() || w.history.records[1].EntryID != id {
				t.Fatalf("the reply did not land in conversation %s: %+v", id, w.history.records)
			}
		})
	}
}

func TestTranscribeOnlyRecordsAKnownRecipientUnlessItOpensAContact(t *testing.T) {
	ctx := context.Background()
	stranger := func() em.OutboundEmail {
		e := sentEmail(conversation.SentByWorkflow("wf-1"))
		e.LeadID, e.LeadName, e.To = "", "", "ninguem@exemplo.com"
		return e
	}

	w := newWorld(t)
	id, err := w.transcript().Transcribe(ctx, stranger())
	if err != nil || id != "" || len(w.history.records) != 0 || len(w.contacts.byID) != 0 || len(w.convs.byID) != 0 {
		t.Fatalf("an unknown address must not open a conversation: %q %v", id, err)
	}

	known := newWorld(t)
	if _, err := known.contacts.FindOrCreate(ctx, em.FindOrCreateContactInput{WorkspaceID: wsA, Address: "ninguem@exemplo.com"}); err != nil {
		t.Fatal(err)
	}
	if id, err := known.transcript().Transcribe(ctx, stranger()); err != nil || id == "" || len(known.history.records) != 1 {
		t.Fatalf("a known contact is recorded: %q %v", id, err)
	}

	opening := newWorld(t)
	e := stranger()
	e.OpensContact = true
	if id, err := opening.transcript().Transcribe(ctx, e); err != nil || id == "" || len(opening.contacts.byID) != 1 || len(opening.history.records) != 1 {
		t.Fatalf("an operator opens a contact for a new address: %q %v", id, err)
	}
}

func TestTranscribeUsesTheSenderThatSent(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	w.addSupportSender()
	e := sentEmail(conversation.SentByPerson("8a1f3c2e-6b7d-4e5f-9a0b-1c2d3e4f5a6b"))
	e.SenderID = senderSupport
	id, err := w.transcript().Transcribe(ctx, e)
	if err != nil {
		t.Fatal(err)
	}
	if conv := w.onlyConversation(); conv.ID != id || conv.SenderID != senderSupport || w.history.records[0].From != "suporte@loja-a.com.br" {
		t.Fatalf("conversation %+v from %s", conv, w.history.records[0].From)
	}
	if inbox, err := w.inboxes.FindByID(ctx, wsA, senderSupport); err != nil || inbox.Address != "suporte@loja-a.com.br" {
		t.Fatalf("the sender's inbox must exist: %+v %v", inbox, err)
	}

	e.ExternalID, e.SenderID, e.Subject = "ses-out-2", "", "Outra proposta"
	again, err := w.transcript().Transcribe(ctx, e)
	if err != nil || again != id || len(w.history.records) != 2 || w.history.records[1].From != "vendas@loja-a.com.br" || w.onlyConversation().Subject != "Outra proposta" {
		t.Fatalf("a later email from the default sender joins the lead's conversation: %q %v %+v", again, err, w.history.records)
	}
}

func TestTranscribeRefusesBeforeWritingAnything(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		mutate func(*em.OutboundEmail, *world)
		want   error
	}{
		{"incomplete email", func(e *em.OutboundEmail, _ *world) { e.ExternalID = "" }, em.ErrOutboundIncomplete},
		{"a sender of another workspace", func(e *em.OutboundEmail, _ *world) { e.SenderID = senderB }, emailsending.ErrSenderNotFound},
		{"no default sender", func(e *em.OutboundEmail, w *world) { w.senders.all = nil }, emailsending.ErrNoDefaultSender},
	}
	for _, tc := range cases {
		w := newWorld(t)
		e := sentEmail(conversation.SentByWorkflow("wf-1"))
		tc.mutate(&e, w)
		id, err := w.transcript().Transcribe(ctx, e)
		if !errors.Is(err, tc.want) || id != "" || len(w.history.records) != 0 || len(w.convs.byID) != 0 {
			t.Errorf("%s: %q %v", tc.name, id, err)
		}
	}
}

func TestTranscribeLinksAnUnlinkedContactToTheLead(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	if _, err := w.contacts.FindOrCreate(ctx, em.FindOrCreateContactInput{WorkspaceID: wsA, Address: anaAddress}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.transcript().Transcribe(ctx, sentEmail(conversation.SentByAI("agent-1"))); err != nil {
		t.Fatal(err)
	}
	contact, _ := w.contacts.FindByAddress(ctx, wsA, anaAddress)
	if contact.LeadID == nil || *contact.LeadID != leadAna {
		t.Fatalf("contact = %+v", contact)
	}
}

func TestTheComposedHTMLIsStoredOnceAndLinkedToTheRow(t *testing.T) {
	w := newWorld(t)
	email := sentEmail(conversation.SentByWorkflow("wf-1"))
	email.HTML = "<p>Oi Ana, segue a proposta.</p>"
	if _, err := w.transcript().Transcribe(context.Background(), email); err != nil {
		t.Fatal(err)
	}
	if len(w.media.inputs) != 1 || w.media.inputs[0].MimeType != em.HTMLMimeType || string(w.media.inputs[0].Data) != email.HTML || w.media.inputs[0].EntryType != shared.EntryTypeEmail {
		t.Fatalf("stored = %+v, want the composed html once", w.media.inputs)
	}
	if ref := em.HTMLRefOf(w.history.records[0].Metadata); ref.MediaID != w.media.inputs[0].ID {
		t.Fatalf("row metadata = %s, want the stored html linked", w.history.records[0].Metadata)
	}
}
