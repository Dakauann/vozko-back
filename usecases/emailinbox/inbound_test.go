package emailinbox

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/conversation"
	conv_event "vozko/domain/conversation_event"
	"vozko/domain/emailaddress"
	"vozko/domain/emailconsent"
	em "vozko/domain/emailinbox"
	"vozko/domain/emailsending"
	"vozko/domain/emailtoken"
	"vozko/domain/lead"
	"vozko/domain/shared"
	"vozko/usecases/emailhygiene/hygienetest"
)

const (
	wsA         = "aaaaaaaa-0000-0000-0000-000000000001"
	wsB         = "bbbbbbbb-0000-0000-0000-000000000002"
	replyA      = "reply.loja-a.com.br"
	replyB      = "reply.loja-b.com.br"
	senderA     = "5e000000-0000-0000-0000-00000000000a"
	senderB     = "5e000000-0000-0000-0000-00000000000b"
	campaignMsg = "c0000000-0000-0000-0000-000000000001"
	otherWsMsg  = "c0000000-0000-0000-0000-000000000002"
	forwardMsg  = "c0000000-0000-0000-0000-000000000003"
	replyMsg    = "c0000000-0000-0000-0000-000000000004"
	objectKey   = "inbound/obj-1"
)

type world struct {
	t           *testing.T
	engagement  *recordingEngagement
	seen        *hygienetest.SeenEmails
	codec       *emailtoken.Codec
	raw         *fakeRaw
	parser      *fakeParser
	sent        *fakeSent
	domains     *fakeDomains
	senders     *fakeSenders
	unsub       *fakeUnsubscriber
	inboxes     *fakeInboxes
	contacts    *fakeContacts
	convs       *fakeConversations
	leads       *fakeLeads
	messages    *fakeMessages
	history     *fakeHistory
	media       *fakeMedia
	assignments *fakeAssignments
	dispatcher  *fakeDispatcher
	claims      *fakeClaims
	billing     *fakeBilling
	gateway     *fakeGateway
	events      *fakeEvents
	senderIP    *fakeSenderIP
	guard       *fakeGuard
	receipts    *fakeReceiveBilling
	originals   *fakeOriginals
	inbound     *Inbound
}

func newWorld(t *testing.T) *world {
	t.Helper()
	domainA := &emailsending.SendingDomain{ID: "d-a", WorkspaceID: wsA, Domain: "loja-a.com.br", ReplyDomain: replyA}
	domainB := &emailsending.SendingDomain{ID: "d-b", WorkspaceID: wsB, Domain: "loja-b.com.br", ReplyDomain: replyB}
	w := &world{
		t:      t,
		codec:  mustCodec(),
		raw:    &fakeRaw{objects: map[string][]byte{objectKey: []byte("raw")}},
		parser: &fakeParser{},
		sent: &fakeSent{byID: map[string]*emailsending.Message{
			campaignMsg: {ID: campaignMsg, WorkspaceID: wsA, DomainID: "d-a", RunKind: emailsending.RunCampaign, RunID: "camp-1", Address: "ana@cliente.com"},
			otherWsMsg:  {ID: otherWsMsg, WorkspaceID: wsB, DomainID: "d-b", RunKind: emailsending.RunCampaign, RunID: "camp-2", Address: "ana@cliente.com"},
		}},
		domains: &fakeDomains{
			byReply: map[string]*emailsending.SendingDomain{replyA: domainA, replyB: domainB},
			byID:    map[string]*emailsending.SendingDomain{"d-a": domainA, "d-b": domainB},
		},
		senders: &fakeSenders{all: []emailsending.Sender{
			{ID: senderA, WorkspaceID: wsA, DomainID: "d-a", LocalPart: "vendas", DisplayName: "Loja A", IsDefault: true},
			{ID: senderB, WorkspaceID: wsB, DomainID: "d-b", LocalPart: "contato", DisplayName: "Loja B", IsDefault: true},
		}},
		unsub:       &fakeUnsubscriber{},
		inboxes:     newFakeInboxes(),
		contacts:    newFakeContacts(),
		convs:       newFakeConversations(),
		leads:       &fakeLeads{byEmail: map[string]*lead.Lead{}},
		messages:    newFakeMessages(),
		media:       &fakeMedia{},
		assignments: &fakeAssignments{},
		dispatcher:  &fakeDispatcher{},
		claims:      &fakeClaims{},
		billing:     &fakeBilling{},
		gateway:     &fakeGateway{},
		events:      &fakeEvents{},
		senderIP:    &fakeSenderIP{},
		guard:       &fakeGuard{},
		receipts:    &fakeReceiveBilling{balanceAfter: 10_000},
		originals:   &fakeOriginals{byMessage: map[string]em.SentOriginal{}},
		engagement:  &recordingEngagement{},
		seen:        &hygienetest.SeenEmails{},
	}
	w.history = &fakeHistory{store: w.messages}
	transcript := NewOutboundTranscript(OutboundTranscriptDeps{
		Contacts: w.contacts, Conversations: w.convs, Inboxes: w.inboxes, Senders: w.senders, Domains: w.domains, History: w.history, Messages: w.messages, Media: w.media,
	})
	w.inbound = NewInbound(InboundDeps{
		Raw: w.raw, Parser: w.parser, Codec: w.codec, Sent: w.sent, ReplyDomains: w.domains, Senders: w.senders,
		Domains: w.domains, Unsubscriber: w.unsub, Inboxes: w.inboxes, Contacts: w.contacts, Conversations: w.convs,
		Leads: w.leads, History: w.history, Messages: w.messages, Media: w.media, Assignments: w.assignments,
		Automation: w.dispatcher, Claims: w.claims, SenderIP: w.senderIP, Guard: w.guard, Receipts: w.receipts,
		Forwarder: NewForwarder(w.senders, w.billing, w.gateway, w.events), Originals: w.originals, Transcript: transcript, Engagement: w.engagement,
		EmailsSeen: w.seen,
	})
	return w
}

func (w *world) tagged(purpose emailtoken.Purpose, id, domain string) emailaddress.Address {
	w.t.Helper()
	address, err := w.codec.TaggedAddress(purpose, id, domain)
	if err != nil {
		w.t.Fatal(err)
	}
	return address
}

func (w *world) receive(msg em.InboundMessage, recipients ...emailaddress.Address) error {
	w.parser.msg = msg
	return w.inbound.HandleInbound(context.Background(), emailsending.InboundNotice{
		Recipients: recipients, ObjectKey: objectKey, MessageID: "ses-in-1", ReceivedAt: time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC),
	})
}

func (w *world) onlyConversation() *em.Conversation {
	w.t.Helper()
	if len(w.convs.byID) != 1 {
		w.t.Fatalf("conversations = %d, want 1", len(w.convs.byID))
	}
	for _, c := range w.convs.byID {
		return c
	}
	return nil
}

func metadataOf(t *testing.T, rec conversation.MessageHistoryRecord) map[string]any {
	t.Helper()
	var meta map[string]any
	if err := json.Unmarshal(rec.Metadata, &meta); err != nil {
		t.Fatal(err)
	}
	return meta
}

func reply(from emailaddress.Address, id string) em.InboundMessage {
	return em.InboundMessage{
		From:      em.Mailbox{Name: "Ana", Address: from},
		Subject:   "Re: Oferta de outubro",
		MessageID: id,
		InReplyTo: "out-1@loja-a.com.br",
		Text:      "Quero saber o preço",
	}
}

func TestReplyByTokenOpensAConversationOwnedByTheMessageWorkspace(t *testing.T) {
	w := newWorld(t)
	if err := w.receive(reply("ana@cliente.com", "m1@cliente.com"), w.tagged(emailtoken.PurposeReply, campaignMsg, replyA)); err != nil {
		t.Fatal(err)
	}
	conv := w.onlyConversation()
	if conv.WorkspaceID != wsA || conv.SenderID != senderA {
		t.Fatalf("conversation = %+v", conv)
	}
	if conv.Subject != "Oferta de outubro" || conv.LastInboundMessageID != "m1@cliente.com" {
		t.Errorf("thread not applied: %+v", conv)
	}
	if len(w.history.records) != 1 {
		t.Fatalf("records = %d", len(w.history.records))
	}
	rec := w.history.records[0]
	if rec.EntryType != shared.EntryTypeEmail || rec.Channel != conversation.MessageChannelEmail || rec.ProviderMessageID != "m1@cliente.com" ||
		rec.Text != "Quero saber o preço" || !rec.SentBy.IsContact() || rec.SenderName != "Ana" {
		t.Errorf("record = %+v", rec)
	}
	if meta := metadataOf(t, rec); meta[em.MetadataSubject] != "Re: Oferta de outubro" || meta[em.MetadataFromDiffers] != nil {
		t.Errorf("metadata = %v", meta)
	}
	if len(w.convs.inbound) != 1 || len(w.assignments.calls) != 1 || w.assignments.calls[0] != "email|"+conv.ID+"|"+senderA {
		t.Errorf("clock %v assignments %v", w.convs.inbound, w.assignments.calls)
	}
	if len(w.dispatcher.inputs) != 1 || w.dispatcher.inputs[0].EntryType != shared.EntryTypeEmail || w.dispatcher.inputs[0].ProviderMessageID != "m1@cliente.com" {
		t.Errorf("dispatch = %+v", w.dispatcher.inputs)
	}
	if inbox, err := w.inboxes.FindByID(context.Background(), wsA, senderA); err != nil || inbox.Address != "vendas@loja-a.com.br" {
		t.Errorf("inbox = %+v, %v", inbox, err)
	}
}

func TestRoutingWithoutAValidToken(t *testing.T) {
	cases := []struct {
		name      string
		recipient func(w *world) emailaddress.Address
		wantConv  bool
		workspace string
	}{
		{"forged token on a known reply domain routes by domain and sender", func(w *world) emailaddress.Address {
			return emailaddress.Address("reply+aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa@" + replyB)
		}, true, wsB},
		{"untagged address on a known reply domain routes by domain", func(*world) emailaddress.Address {
			return emailaddress.Address("vendas@" + replyA)
		}, true, wsA},
		{"forged token on an unknown domain is dropped", func(*world) emailaddress.Address {
			return emailaddress.Address("reply+aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa@estranho.com")
		}, false, ""},
		{"token for an unknown message falls back to the domain", func(w *world) emailaddress.Address {
			return w.tagged(emailtoken.PurposeReply, "d0000000-0000-0000-0000-000000000009", replyA)
		}, true, wsA},
		{"valid token arriving on another workspace reply domain is dropped", func(w *world) emailaddress.Address {
			return w.tagged(emailtoken.PurposeReply, otherWsMsg, replyA)
		}, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			if err := w.receive(reply("ana@cliente.com", "m1@cliente.com"), tc.recipient(w)); err != nil {
				t.Fatal(err)
			}
			if !tc.wantConv {
				if len(w.convs.byID) != 0 || len(w.history.records) != 0 || len(w.dispatcher.inputs) != 0 {
					t.Fatalf("dropped message reached the inbox: convs=%d records=%d", len(w.convs.byID), len(w.history.records))
				}
				return
			}
			if conv := w.onlyConversation(); conv.WorkspaceID != tc.workspace {
				t.Fatalf("workspace = %s, want %s", conv.WorkspaceID, tc.workspace)
			}
		})
	}
}

func TestUnsubscribeTagNeverReachesTheInbox(t *testing.T) {
	w := newWorld(t)
	if err := w.receive(reply("ana@cliente.com", "m1@cliente.com"), w.tagged(emailtoken.PurposeUnsubscribe, campaignMsg, replyA)); err != nil {
		t.Fatal(err)
	}
	if len(w.unsub.calls) != 1 || w.unsub.calls[0] != campaignMsg {
		t.Fatalf("unsubscribe calls = %v", w.unsub.calls)
	}
	if len(w.convs.byID) != 0 || len(w.history.records) != 0 {
		t.Fatal("an unsubscribe request must not open a conversation")
	}
}

func TestDuplicateMessageIDIsRecordedOnce(t *testing.T) {
	w := newWorld(t)
	recipient := w.tagged(emailtoken.PurposeReply, campaignMsg, replyA)
	for i := 0; i < 2; i++ {
		if err := w.receive(reply("ana@cliente.com", "m1@cliente.com"), recipient, recipient); err != nil {
			t.Fatal(err)
		}
	}
	if len(w.history.records) != 1 || len(w.dispatcher.inputs) != 1 || len(w.convs.inbound) != 1 {
		t.Fatalf("records=%d dispatch=%d clocks=%d", len(w.history.records), len(w.dispatcher.inputs), len(w.convs.inbound))
	}
}

func TestMissingMessageIDFallsBackToTheProviderID(t *testing.T) {
	w := newWorld(t)
	if err := w.receive(reply("ana@cliente.com", ""), emailaddress.Address("vendas@"+replyA)); err != nil {
		t.Fatal(err)
	}
	if got := w.history.records[0].ProviderMessageID; got != "provider:ses-in-1" {
		t.Fatalf("external id = %q", got)
	}
}

func TestAttachmentsAreStoredOneRowEach(t *testing.T) {
	w := newWorld(t)
	msg := reply("ana@cliente.com", "m1@cliente.com")
	msg.Attachments = []em.Attachment{
		{FileName: "nota.pdf", ContentType: "application/pdf", Data: []byte("%PDF"), Size: 4},
		{FileName: "video.mp4", ContentType: "video/mp4", Size: em.MaxAttachmentTotalBytes + 1},
		{FileName: "foto.JPG", ContentType: "image/jpeg", Data: []byte("jpg"), Size: 3},
	}
	if err := w.receive(msg, emailaddress.Address("vendas@"+replyA)); err != nil {
		t.Fatal(err)
	}
	conv := w.onlyConversation()
	if len(w.history.records) != 4 {
		t.Fatalf("records = %d", len(w.history.records))
	}
	wantRefs := []string{"m1@cliente.com", "m1@cliente.com:att0", "m1@cliente.com:att1", "m1@cliente.com:att2"}
	wantTypes := []conversation.MessageType{conversation.MessageTypeUserMessage, conversation.MessageTypeMedia, conversation.MessageTypeUnsupported, conversation.MessageTypeMedia}
	for i, rec := range w.history.records {
		if rec.ProviderMessageID != wantRefs[i] || rec.MessageType != wantTypes[i] {
			t.Errorf("row %d = %s/%s, want %s/%s", i, rec.ProviderMessageID, rec.MessageType, wantRefs[i], wantTypes[i])
		}
	}
	if w.history.records[2].Text != em.UnavailableAttachmentText || metadataOf(t, w.history.records[2])[em.MetadataAttachmentUnavailable] != true {
		t.Errorf("oversized attachment row = %+v", w.history.records[2])
	}
	if len(w.media.inputs) != 2 {
		t.Fatalf("stored = %d", len(w.media.inputs))
	}
	first := w.media.inputs[0]
	if !strings.HasPrefix(first.Key, "conversations/email/"+conv.ID+"/") || !strings.HasSuffix(first.Key, ".pdf") ||
		first.EntryType != shared.EntryTypeEmail || first.Type != conversation.MediaTypeDocument || first.OriginalFilename != "nota.pdf" {
		t.Errorf("stored media = %+v", first)
	}
	if w.media.inputs[1].Type != conversation.MediaTypeImage || !strings.HasSuffix(w.media.inputs[1].Key, ".jpg") {
		t.Errorf("image = %+v", w.media.inputs[1])
	}
}

func TestAttachmentOnlyMessageUsesTheMessageIDForTheFirstFile(t *testing.T) {
	w := newWorld(t)
	msg := reply("ana@cliente.com", "m1@cliente.com")
	msg.Text = ""
	msg.Attachments = []em.Attachment{{FileName: "a.pdf", ContentType: "application/pdf", Data: []byte("x")}}
	if err := w.receive(msg, emailaddress.Address("vendas@"+replyA)); err != nil {
		t.Fatal(err)
	}
	if len(w.history.records) != 1 || w.history.records[0].ProviderMessageID != "m1@cliente.com" {
		t.Fatalf("records = %+v", w.history.records)
	}
}

func TestReplyFromAnotherAddressOpensItsOwnConversationFlagged(t *testing.T) {
	w := newWorld(t)
	if err := w.receive(reply("colega@cliente.com", "m1@cliente.com"), w.tagged(emailtoken.PurposeReply, campaignMsg, replyA)); err != nil {
		t.Fatal(err)
	}
	conv := w.onlyConversation()
	contact, _ := w.contacts.FindByID(context.Background(), conv.ContactID)
	if contact.Address != "colega@cliente.com" || conv.WorkspaceID != wsA {
		t.Fatalf("conversation contact = %s workspace %s", contact.Address, conv.WorkspaceID)
	}
	meta := metadataOf(t, w.history.records[0])
	if meta[em.MetadataFromDiffers] != true || meta[em.MetadataOriginalRecipient] != "ana@cliente.com" {
		t.Fatalf("metadata = %v", meta)
	}
}

func TestReplyToOurReplyLandsInThePinnedConversation(t *testing.T) {
	w := newWorld(t)
	contact, _ := w.contacts.FindOrCreate(context.Background(), em.FindOrCreateContactInput{WorkspaceID: wsA, Address: "ana@cliente.com"})
	pinned := w.convs.add(&em.Conversation{ID: "conv-pinned", WorkspaceID: wsA, ContactID: contact.ID, SenderID: senderA})
	w.sent.byID[replyMsg] = &emailsending.Message{ID: replyMsg, WorkspaceID: wsA, DomainID: "d-a", RunKind: emailsending.RunReply, RunID: pinned.ID, Address: "ana@cliente.com"}

	if err := w.receive(reply("ana@cliente.com", "m2@cliente.com"), w.tagged(emailtoken.PurposeReply, replyMsg, replyA)); err != nil {
		t.Fatal(err)
	}
	if w.onlyConversation().ID != pinned.ID || w.history.records[0].EntryID != pinned.ID {
		t.Fatalf("reply did not land in the pinned conversation")
	}
}

func TestReplyToAForwardedCopyIsDropped(t *testing.T) {
	w := newWorld(t)
	contact, _ := w.contacts.FindOrCreate(context.Background(), em.FindOrCreateContactInput{WorkspaceID: wsA, Address: "ana@cliente.com"})
	pinned := w.convs.add(&em.Conversation{ID: "conv-pinned", WorkspaceID: wsA, ContactID: contact.ID, SenderID: senderA})
	w.sent.byID[forwardMsg] = &emailsending.Message{ID: forwardMsg, WorkspaceID: wsA, DomainID: "d-a", RunKind: emailsending.RunReply, RunID: pinned.ID, Address: "equipe@loja-a.com.br"}

	if err := w.receive(reply("equipe@loja-a.com.br", "m3@loja-a.com.br"), w.tagged(emailtoken.PurposeReply, forwardMsg, replyA)); err != nil {
		t.Fatal(err)
	}
	if len(w.convs.byID) != 1 || len(w.history.records) != 0 {
		t.Fatalf("a reply to the forwarded copy reached the inbox: convs=%d records=%d", len(w.convs.byID), len(w.history.records))
	}
}

func TestAutomatedRepliesAreRecordedWithoutAutomationOrForward(t *testing.T) {
	w := newWorld(t)
	w.senders.all[0].ReplyForwardTo = "equipe@loja-a.com.br"
	msg := reply("ana@cliente.com", "auto@cliente.com")
	msg.Automated = true
	if err := w.receive(msg, emailaddress.Address("vendas@"+replyA)); err != nil {
		t.Fatal(err)
	}
	if len(w.history.records) != 1 || len(w.dispatcher.inputs) != 0 || len(w.gateway.requests) != 0 || len(w.billing.charges) != 0 {
		t.Fatalf("records=%d dispatch=%d forwards=%d", len(w.history.records), len(w.dispatcher.inputs), len(w.gateway.requests))
	}
}

func TestLeadIsLinkedAndPassedToAutomation(t *testing.T) {
	w := newWorld(t)
	w.leads.byEmail[wsA+"|ana@cliente.com"] = &lead.Lead{ID: "lead-1", WorkspaceID: wsA}
	if err := w.receive(reply("ana@cliente.com", "m1@cliente.com"), emailaddress.Address("vendas@"+replyA)); err != nil {
		t.Fatal(err)
	}
	if got := w.dispatcher.inputs[0].LeadID; got == nil || *got != "lead-1" {
		t.Fatalf("lead = %v", got)
	}
}

func TestQuotedHistoryAndLongBodies(t *testing.T) {
	w := newWorld(t)
	msg := reply("ana@cliente.com", "m1@cliente.com")
	msg.Text = strings.Repeat("a", em.MaxTranscriptTextBytes+10)
	msg.QuotedText = "> antigo"
	if err := w.receive(msg, emailaddress.Address("vendas@"+replyA)); err != nil {
		t.Fatal(err)
	}
	rec := w.history.records[0]
	meta := metadataOf(t, rec)
	if len(rec.Text) > em.MaxTranscriptTextBytes || meta[em.MetadataTruncated] != true || meta[em.MetadataQuotedText] != "> antigo" {
		t.Fatalf("len=%d meta=%v", len(rec.Text), meta)
	}
}

func TestForwardCopyToTheSenderMailbox(t *testing.T) {
	cases := []struct {
		name       string
		short      bool
		sendErr    error
		wantSends  int
		wantRefund int
		wantEvent  string
	}{
		{"charged then sent", false, nil, 1, 0, ""},
		{"wallet short skips with an event", true, nil, 0, 0, ForwardSkippedBalance},
		{"send failure refunds with an event", false, errors.New("provider down"), 1, 1, ForwardSkippedSend},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			w.senders.all[0].ReplyForwardTo = "equipe@loja-a.com.br"
			order := []string{}
			w.billing.short, w.billing.order = tc.short, &order
			w.gateway.err, w.gateway.order = tc.sendErr, &order
			if err := w.receive(reply("ana@cliente.com", "m1@cliente.com"), emailaddress.Address("vendas@"+replyA)); err != nil {
				t.Fatal(err)
			}
			if len(w.history.records) != 1 || len(w.dispatcher.inputs) != 1 {
				t.Fatal("the forward must never block recording or automation")
			}
			if len(w.gateway.requests) != tc.wantSends || len(w.billing.refunds) != tc.wantRefund {
				t.Fatalf("sends=%d refunds=%d", len(w.gateway.requests), len(w.billing.refunds))
			}
			if tc.wantSends > 0 {
				if order[0] != "charge" || order[1] != "send" {
					t.Errorf("charge must precede the send: %v", order)
				}
				req := w.gateway.requests[0]
				if req.To != "equipe@loja-a.com.br" || req.Kind != emailsending.KindTransactional || req.RunKind != emailsending.RunReply || req.SenderID != senderA {
					t.Errorf("forward request = %+v", req)
				}
			}
			if tc.wantSends > 0 && tc.sendErr == nil && !strings.HasPrefix(w.gateway.contents[0].Subject, "Fwd: Oferta") {
				t.Errorf("subject = %q", w.gateway.contents[0].Subject)
			}
			switch {
			case tc.wantEvent == "" && len(w.events.events) != 0:
				t.Errorf("unexpected events %+v", w.events.events)
			case tc.wantEvent != "":
				if len(w.events.events) != 1 || w.events.events[0].EventType != conv_event.EventEmailForwardSkipped ||
					w.events.events[0].DetailsMap()["reason"] != tc.wantEvent {
					t.Errorf("events = %+v", w.events.events)
				}
			}
		})
	}
}

func TestRawMessageFailures(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		wantErr bool
	}{
		{"transient fetch error is retried", errors.New("s3 timeout"), true},
		{"key outside the inbound prefix is dropped", emailsending.ErrRawMessageKey, false},
		{"oversized message is dropped", emailsending.ErrRawMessageTooLarge, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			w.raw.err = tc.err
			err := w.receive(reply("ana@cliente.com", "m1@cliente.com"), emailaddress.Address("vendas@"+replyA))
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, want error %v", err, tc.wantErr)
			}
		})
	}
}

func TestUnparseableMessageIsDropped(t *testing.T) {
	w := newWorld(t)
	w.parser.err = em.ErrUnparseableMessage
	if err := w.receive(em.InboundMessage{}, emailaddress.Address("vendas@"+replyA)); err != nil {
		t.Fatal(err)
	}
	if len(w.history.records) != 0 {
		t.Fatal("nothing must be recorded")
	}
}

func TestNoSenderInTheWorkspaceDropsTheMessage(t *testing.T) {
	w := newWorld(t)
	w.senders.all = nil
	if err := w.receive(reply("ana@cliente.com", "m1@cliente.com"), emailaddress.Address("vendas@"+replyA)); err != nil {
		t.Fatal(err)
	}
	if len(w.convs.byID) != 0 {
		t.Fatal("a conversation needs a sender to own it")
	}
}

func TestUnsubscribeFailures(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		wantErr bool
	}{
		{"unknown message is dropped", emailconsent.ErrUnsubscribeTokenInvalid, false},
		{"transient failure is retried", errors.New("db down"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			w.unsub.err = tc.err
			err := w.receive(reply("ana@cliente.com", "m1@cliente.com"), w.tagged(emailtoken.PurposeUnsubscribe, campaignMsg, replyA))
			if (err != nil) != tc.wantErr || len(w.history.records) != 0 {
				t.Fatalf("err = %v, records = %d", err, len(w.history.records))
			}
		})
	}
}

type recordingEngagement struct{ replies []string }

func (r *recordingEngagement) Replied(_ context.Context, workspaceID string, address emailaddress.Address, _ time.Time) error {
	r.replies = append(r.replies, workspaceID+"|"+string(address))
	return nil
}

func TestAReplyCountsAsEngagementButAnAutomaticOneDoesNot(t *testing.T) {
	w := newWorld(t)
	if err := w.receive(reply("ana@cliente.com", "m1@cliente.com"), w.tagged(emailtoken.PurposeReply, campaignMsg, replyA)); err != nil {
		t.Fatal(err)
	}
	if len(w.engagement.replies) != 1 || w.engagement.replies[0] != wsA+"|ana@cliente.com" {
		t.Fatalf("engagement = %v", w.engagement.replies)
	}
	auto := reply("ana@cliente.com", "m2@cliente.com")
	auto.Automated = true
	if err := w.receive(auto, w.tagged(emailtoken.PurposeReply, campaignMsg, replyA)); err != nil {
		t.Fatal(err)
	}
	if len(w.engagement.replies) != 1 {
		t.Fatalf("an out of office reply is not engagement, got %v", w.engagement.replies)
	}
}
