package carriermessaging_usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"vozko/domain/carriermessaging"
	"vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/domain/rcs"
	"vozko/domain/sms"
)

type harness struct {
	svc          *Service
	provider     *fakeProvider
	ledger       *fakeLedger
	balance      *fakeBalance
	spend        *fakeSpend
	messages     *memMessages
	senders      *memSenders
	events       *memEvents
	suppressions *memSuppressions
	settings     *memSettings
	contacts     *memContacts
	convs        *memConversations
	smsConvs     *memConversations
	routes       *memRoutes
	transcript   *fakeTranscript
	tstatus      *fakeTranscriptStatus
	announcer    *fakeAnnouncer
	whatsapp     *fakeWhatsApp
	leads        *fakeLeads
	notices      *fakeNotices
	inbox        *fakeInbox
	now          time.Time
	ids          int
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{
		provider:     &fakeProvider{outcome: carriermessaging.SendOutcome{Accepted: true}, smsOut: carriermessaging.SendOutcome{Accepted: true}},
		ledger:       newFakeLedger(),
		balance:      &fakeBalance{micros: 100_000_000},
		spend:        &fakeSpend{},
		messages:     &memMessages{rows: map[string]*carriermessaging.Message{}},
		senders:      &memSenders{rows: map[string]*rcs.Sender{}},
		events:       &memEvents{keys: map[string]bool{}},
		suppressions: &memSuppressions{rows: map[string]carriermessaging.Suppression{}},
		settings:     &memSettings{rows: map[string]carriermessaging.Settings{}},
		contacts:     &memContacts{rows: map[string]*carriermessaging.Contact{}},
		convs:        &memConversations{prefix: "conv", rows: map[string]*carriermessaging.Conversation{}},
		smsConvs:     &memConversations{prefix: "smsconv", rows: map[string]*carriermessaging.Conversation{}},
		routes:       &memRoutes{rows: map[string]*sms.Route{"route-1": {ID: "route-1", WorkspaceID: "ws-1", BrandPrefix: "Loja", Kind: sms.RouteShared, Status: sms.RouteActive}}},
		transcript:   &fakeTranscript{},
		tstatus:      &fakeTranscriptStatus{},
		announcer:    &fakeAnnouncer{},
		whatsapp:     &fakeWhatsApp{},
		leads:        &fakeLeads{leads: map[string]*lead.Lead{}},
		notices:      &fakeNotices{},
		inbox:        &fakeInbox{},
		now:          t0,
	}
	h.senders.rows["sender-1"] = &rcs.Sender{
		ID: "sender-1", WorkspaceID: "ws-1", BrandName: "Loja", Status: rcs.SenderLaunched,
		BillingCategory: rcs.BillingNonConversational, Provider: "fake", ProviderProfileID: "profile-1",
	}
	svc, err := NewService(Deps{
		Repos: Repositories{
			Senders: h.senders, Messages: h.messages, Events: h.events, Suppressions: h.suppressions,
			Settings: h.settings, Contacts: h.contacts, RCSConversations: h.convs, SMSConversations: h.smsConvs, Routes: h.routes,
		},
		Provider: h.provider, SMS: h.provider, Charges: NewCharges(defaultPricer(), h.ledger),
		Balance: h.balance, Spend: h.spend, Links: fakeLinks{}, Leads: h.leads,
		Transcript: h.transcript, TranscriptStatus: h.tstatus, Announcer: h.announcer,
		Timezones: fakeTimezones{}, WhatsApp: h.whatsapp, Notices: h.notices, Inbox: h.inbox, Documents: fakeDocuments{valid: true}, Library: fakeLibrary{}, RemoteMedia: fakeRemote{}, AutomationRefs: fakeRefs{},
		MediaURLs:    carrierMediaURLs(carrierLibraryByURL{}, carrierUploadsByURL{}),
		PlatformName: "Vozko", PlatformEmailDomain: "vozkoia.com", SMSSharedNumber: "29000",
		Now:   func() time.Time { return h.now },
		NewID: func() string { h.ids++; return fmt.Sprintf("00000000-0000-0000-0000-%012d", h.ids) },
	})
	if err != nil {
		t.Fatalf("NewService = %v", err)
	}
	svc.launched = func(carriermessaging.Channel) bool { return true }
	h.svc = svc
	return h
}

func marketing(content rcs.Content) DeliverRequest {
	return DeliverRequest{
		WorkspaceID: "ws-1", Channel: carriermessaging.ChannelRCS, SenderID: "sender-1", Phone: "+55 11 99999-0000",
		RunKind: carriermessaging.RunCampaign, Marketing: true, RunID: "camp-1", Content: content,
		SentBy: conversation.SentByCampaign("camp-1"),
	}
}

var cardContent = rcs.Content{Cards: []rcs.Card{{
	Title: "Oi {{1}}", Description: "Oferta",
	Media: &rcs.Media{Kind: rcs.MediaImage, URL: "https://media.example.com/a.jpg", MIMEType: "image/jpeg"},
	Suggestions: []rcs.Suggestion{
		{Kind: rcs.SuggestReply, Label: "Quero"},
		{Kind: rcs.SuggestOpenURL, Label: "Ver", URL: "https://loja.example.com/p"},
	},
}}}

func TestDeliverSendsChargesAndRecordsTheTranscript(t *testing.T) {
	h := newHarness(t)
	req := marketing(cardContent)
	req.Vars = []string{"Ana"}

	msg, err := h.svc.Deliver(context.Background(), req)
	if err != nil {
		t.Fatalf("Deliver = %v", err)
	}
	if msg.Status != carriermessaging.StatusAccepted || msg.Class != rcs.ClassSingle || msg.Phone != "5511999990000" || len(msg.Reference) != 32 {
		t.Fatalf("message = %+v", msg)
	}
	if len(h.provider.sends) != 1 {
		t.Fatalf("sends = %d", len(h.provider.sends))
	}
	sent := h.provider.sends[0]
	if sent.Reference != msg.Reference || sent.SenderProfileID != "profile-1" || sent.Phone != "5511999990000" || sent.Validity != carriermessaging.DefaultValidity {
		t.Fatalf("send request = %+v", sent)
	}
	card := sent.Content.Cards[0]
	if card.Title != "Oi Ana" {
		t.Fatalf("title = %q, variables must be filled", card.Title)
	}
	if card.Suggestions[0].Postback != (rcs.ButtonKey{Card: 0, Index: 0}).Postback() {
		t.Fatalf("reply postback = %q", card.Suggestions[0].Postback)
	}
	if !strings.HasPrefix(card.Suggestions[1].URL, "https://api.example.com/public/rcs/r/") {
		t.Fatalf("link was not tracked: %q", card.Suggestions[1].URL)
	}
	if len(h.ledger.debits) != 1 || *h.ledger.debits[0].ReferenceID != carriermessaging.ChargeReference(carriermessaging.ChargeRCSSingle, msg.ID) {
		t.Fatalf("debits = %+v", h.ledger.debits)
	}
	if len(h.transcript.records) != 1 {
		t.Fatalf("transcript = %d", len(h.transcript.records))
	}
	rec := h.transcript.records[0]
	if rec.EntryType != carriermessaging.ChannelRCS.EntryType() || rec.ProviderMessageID != msg.Reference || rec.EntryID != msg.ConversationID || rec.Text != "Oi Ana\nOferta" {
		t.Fatalf("transcript record = %+v", rec)
	}
	stored, _ := h.messages.FindByID(context.Background(), msg.ID)
	if stored.Content.Cards[0].Suggestions[1].URL != "https://loja.example.com/p" {
		t.Fatal("the stored content keeps the real link so clicks can redirect to it")
	}
}

func TestDeliverRefusesBeforeAnyCharge(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*harness, *DeliverRequest)
		want  error
	}{
		{"sender not launched", func(h *harness, _ *DeliverRequest) { h.senders.rows["sender-1"].Status = rcs.SenderPaused }, rcs.ErrSenderCannotSend},
		{"sender of another workspace", func(_ *harness, r *DeliverRequest) { r.WorkspaceID = "ws-2" }, rcs.ErrSenderNotFound},
		{"bad phone", func(_ *harness, r *DeliverRequest) { r.Phone = "123" }, carriermessaging.ErrPhoneInvalid},
		{"missing variable leaves a broken card", func(_ *harness, r *DeliverRequest) { r.Content = rcs.Content{} }, rcs.ErrContentEmpty},
		{"suppressed contact", func(h *harness, _ *DeliverRequest) {
			h.suppressions.rows["ws-1|5511999990000"] = carriermessaging.Suppression{}
		}, carriermessaging.ErrSuppressed},
		{"opted out lead", func(h *harness, _ *DeliverRequest) {
			at := t0
			h.leads.leads["5511999990000"] = &lead.Lead{ID: "l", Number: "5511999990000", OptedOutAt: &at}
		}, carriermessaging.ErrRecipientRefused},
		{"frequency cap", func(h *harness, _ *DeliverRequest) {
			for i := range 2 {
				id := fmt.Sprintf("old-%d", i)
				h.messages.rows[id] = &carriermessaging.Message{ID: id, WorkspaceID: "ws-1", Phone: "5511999990000", RunKind: carriermessaging.RunCampaign, Marketing: true, Status: carriermessaging.StatusDelivered, CreatedAt: t0.Add(-time.Hour)}
			}
		}, carriermessaging.ErrFrequencyCap},
		{"quiet hours", func(h *harness, _ *DeliverRequest) { h.now = time.Date(2026, 10, 21, 23, 30, 0, 0, time.UTC) }, carriermessaging.ErrQuietHours},
		{"balance below the reply floor", func(h *harness, _ *DeliverRequest) { h.balance.micros = 3_190_000 }, carriermessaging.ErrInsufficientBalance},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			req := marketing(rcs.Content{Text: "Oi"})
			tc.setup(h, &req)
			before := len(h.messages.all())
			_, err := h.svc.Deliver(context.Background(), req)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Deliver = %v, want %v", err, tc.want)
			}
			if len(h.provider.sends) != 0 || len(h.ledger.debits) != 0 || len(h.messages.all()) != before {
				t.Fatal("a refused send must not reach the provider, the ledger or the message table")
			}
		})
	}
}

func TestQuietHoursTellsWhenTheWindowOpens(t *testing.T) {
	h := newHarness(t)
	h.now = time.Date(2026, 10, 21, 23, 30, 0, 0, time.UTC)
	_, err := h.svc.Deliver(context.Background(), marketing(rcs.Content{Text: "Oi"}))
	var quiet *carriermessaging.QuietHoursError
	if !errors.As(err, &quiet) {
		t.Fatalf("Deliver = %v", err)
	}
	loc, _ := time.LoadLocation("America/Sao_Paulo")
	if want := time.Date(2026, 10, 22, 8, 0, 0, 0, loc); !quiet.NextOpen.Equal(want) {
		t.Fatalf("NextOpen = %v, want %v", quiet.NextOpen, want)
	}
}

func TestOperatorRepliesIgnoreMarketingGuardrails(t *testing.T) {
	h := newHarness(t)
	h.now = time.Date(2026, 10, 21, 23, 30, 0, 0, time.UTC)
	h.suppressions.rows["ws-1|5511999990000"] = carriermessaging.Suppression{}
	req := marketing(rcs.Content{Text: "Oi"})
	req.RunKind, req.Marketing, req.SentBy = carriermessaging.RunOperator, false, conversation.SentByPerson("user-1")
	if _, err := h.svc.Deliver(context.Background(), req); err != nil {
		t.Fatalf("operator reply = %v", err)
	}
}

func TestBlockedLeadRefusesEverySend(t *testing.T) {
	h := newHarness(t)
	h.leads.leads["5511999990000"] = &lead.Lead{ID: "l", Number: "5511999990000", Blocked: true}
	req := marketing(rcs.Content{Text: "Oi"})
	req.RunKind = carriermessaging.RunOperator
	if _, err := h.svc.Deliver(context.Background(), req); !errors.Is(err, carriermessaging.ErrRecipientRefused) {
		t.Fatalf("Deliver = %v", err)
	}
}

func TestShortfallSaysHowMuchIsMissing(t *testing.T) {
	h := newHarness(t)
	h.balance.micros = 3_190_000
	_, err := h.svc.Deliver(context.Background(), marketing(rcs.Content{Text: "Oi"}))
	var short *carriermessaging.ShortfallError
	if !errors.As(err, &short) || short.MissingMicros != 31_667+3_166_700-3_190_000 {
		t.Fatalf("Deliver = %v", err)
	}
}

func TestTestSendsAreFreeUpToTheDailyLimit(t *testing.T) {
	h := newHarness(t)
	req := marketing(rcs.Content{Text: "Oi"})
	req.RunKind = carriermessaging.RunTest
	for range carriermessaging.DailyFreeTestSends {
		if _, err := h.svc.Deliver(context.Background(), req); err != nil {
			t.Fatalf("test send = %v", err)
		}
	}
	if len(h.ledger.debits) != 0 {
		t.Fatal("test sends are free")
	}
	if _, err := h.svc.Deliver(context.Background(), req); !errors.Is(err, carriermessaging.ErrTestLimit) {
		t.Fatalf("21st test send = %v", err)
	}
}

func TestProviderRejectionRefundsAndFallsBackToSMS(t *testing.T) {
	h := newHarness(t)
	h.provider.outcome = carriermessaging.SendOutcome{ErrorCode: "42", Unreachable: true}
	req := marketing(rcs.Content{Text: "Oi {{1}}"})
	req.Vars = []string{"Ana"}
	req.Fallback = carriermessaging.FallbackSpec{SMSRouteID: "route-1", SMSText: "Oi Ana, veja a oferta"}

	msg, err := h.svc.Deliver(context.Background(), req)
	if err != nil {
		t.Fatalf("Deliver = %v", err)
	}
	if msg.Status != carriermessaging.StatusRejected || msg.ErrorCode != "42" {
		t.Fatalf("message = %+v", msg)
	}
	if len(h.ledger.credits) != 1 || *h.ledger.credits[0].ReferenceID != "refund:"+carriermessaging.ChargeReference(carriermessaging.ChargeRCSBasic, msg.ID) {
		t.Fatalf("refunds = %+v", h.ledger.credits)
	}
	if len(h.provider.sms) != 1 || h.provider.sms[0].Text != "Loja: Oi Ana, veja a oferta" || h.provider.sms[0].From != "29000" || h.provider.sms[0].Phone != "5511999990000" {
		t.Fatalf("sms = %+v", h.provider.sms)
	}
	parent, _ := h.messages.FindByID(context.Background(), msg.ID)
	child, _ := h.messages.FindByID(context.Background(), parent.FallbackMessageID)
	if child == nil || child.Step != carriermessaging.StepSMS || child.ParentMessageID != msg.ID || child.Status != carriermessaging.StatusAccepted {
		t.Fatalf("sms step = %+v", child)
	}
	if !hasDebit(h.ledger, carriermessaging.ChargeReference(carriermessaging.ChargeSMSSegment, child.ID)) {
		t.Fatal("the SMS step is charged")
	}
}

func TestProviderOutageFailsTheMessageAndFallsBackToWhatsApp(t *testing.T) {
	h := newHarness(t)
	h.provider.err = carriermessaging.ErrProviderUnavailable
	req := marketing(rcs.Content{Text: "Oi"})
	req.Fallback = carriermessaging.FallbackSpec{WhatsApp: &carriermessaging.WhatsAppFallback{BusinessPhoneID: "bp-1", TemplateID: "tpl-1", BodyParams: []string{"Ana"}}, SMSRouteID: "route-1", SMSText: "Oi"}

	msg, err := h.svc.Deliver(context.Background(), req)
	if err != nil {
		t.Fatalf("Deliver = %v", err)
	}
	if msg.Status != carriermessaging.StatusFailed || msg.ErrorCode != ErrorCodeProviderUnavailable {
		t.Fatalf("message = %+v", msg)
	}
	if len(h.whatsapp.starts) != 1 || h.whatsapp.starts[0].TemplateID != "tpl-1" || h.whatsapp.starts[0].Phone != "5511999990000" {
		t.Fatalf("whatsapp = %+v", h.whatsapp.starts)
	}
	if len(h.provider.sms) != 0 {
		t.Fatal("SMS waits for WhatsApp to fail")
	}
}

func TestWhatsAppRefusalGoesStraightToSMS(t *testing.T) {
	h := newHarness(t)
	h.provider.outcome = carriermessaging.SendOutcome{ErrorCode: "1"}
	h.whatsapp.err = errors.New("template paused")
	req := marketing(rcs.Content{Text: "Oi"})
	req.Fallback = carriermessaging.FallbackSpec{WhatsApp: &carriermessaging.WhatsAppFallback{BusinessPhoneID: "bp-1", TemplateID: "tpl-1"}, SMSRouteID: "route-1", SMSText: "Oi"}

	if _, err := h.svc.Deliver(context.Background(), req); err != nil {
		t.Fatalf("Deliver = %v", err)
	}
	if len(h.provider.sms) != 1 {
		t.Fatalf("sms = %d, want the chain to reach SMS", len(h.provider.sms))
	}
	steps := map[carriermessaging.Step]carriermessaging.Status{}
	for _, m := range h.messages.all() {
		steps[m.Step] = m.Status
	}
	if steps[carriermessaging.StepWhatsApp] != carriermessaging.StatusFailed || steps[carriermessaging.StepSMS] != carriermessaging.StatusAccepted {
		t.Fatalf("steps = %+v", steps)
	}
}

func TestFallbackRunsOnlyOnce(t *testing.T) {
	h := newHarness(t)
	h.provider.outcome = carriermessaging.SendOutcome{ErrorCode: "1"}
	req := marketing(rcs.Content{Text: "Oi"})
	req.Fallback = carriermessaging.FallbackSpec{SMSRouteID: "route-1", SMSText: "Oi"}
	msg, _ := h.svc.Deliver(context.Background(), req)

	stored, _ := h.messages.FindByID(context.Background(), msg.ID)
	h.svc.runFallback(context.Background(), *stored)
	if len(h.provider.sms) != 1 {
		t.Fatalf("sms = %d, the fallback must not run twice", len(h.provider.sms))
	}
}

func TestMessagesInsideAnOpenBilledConversationAreNotChargedAgain(t *testing.T) {
	h := newHarness(t)
	h.senders.rows["sender-1"].BillingCategory = rcs.BillingConversational
	req := marketing(rcs.Content{Text: "Oi"})
	req.RunKind = carriermessaging.RunOperator
	first, _ := h.svc.Deliver(context.Background(), req)
	opened := t0
	h.convs.rows[first.ConversationID].ConversationOpenedAt = &opened

	h.now = t0.Add(time.Hour)
	second, err := h.svc.Deliver(context.Background(), req)
	if err != nil {
		t.Fatalf("Deliver = %v", err)
	}
	if second.ChargeReference != "" || len(h.ledger.debits) != 1 {
		t.Fatalf("debits = %d, the open conversation covers the second message", len(h.ledger.debits))
	}
}

func TestReplyingToACustomerOnAConversationalSenderOpensAConversation(t *testing.T) {
	h := newHarness(t)
	h.senders.rows["sender-1"].BillingCategory = rcs.BillingConversational
	req := marketing(rcs.Content{Text: "Oi"})
	req.RunKind = carriermessaging.RunOperator
	contact, _ := h.contacts.FindOrCreate(context.Background(), "ws-1", "5511999990000", "")
	conv, _, _ := h.convs.FindOrCreate(context.Background(), "ws-1", "sender-1", contact.ID)
	asked := t0.Add(-time.Hour)
	h.convs.rows[conv.ID].LastCustomerMessageAt = &asked

	if _, err := h.svc.Deliver(context.Background(), req); err != nil {
		t.Fatalf("Deliver = %v", err)
	}
	if len(h.ledger.debits) != 1 || !hasDebit(h.ledger, carriermessaging.ChargeReference(carriermessaging.ChargeRCSConversation, conv.ID+":"+t0.Format(time.RFC3339))) {
		t.Fatalf("debits = %+v, the conversation replaces the message charge", h.ledger.debits)
	}
}

func hasDebit(l *fakeLedger, reference string) bool {
	for _, d := range l.debits {
		if *d.ReferenceID == reference {
			return true
		}
	}
	return false
}

func TestLostProviderAnswerKeepsTheMessagePendingAndCharged(t *testing.T) {
	h := newHarness(t)
	h.provider.err = carriermessaging.ErrSendOutcomeUnknown
	req := marketing(rcs.Content{Text: "Oi"})
	req.Fallback = carriermessaging.FallbackSpec{SMSRouteID: "route-1", SMSText: "Oi"}

	msg, err := h.svc.Deliver(context.Background(), req)
	if err != nil {
		t.Fatalf("Deliver = %v", err)
	}
	if msg.Status != carriermessaging.StatusPending || len(h.ledger.credits) != 0 || len(h.provider.sms) != 0 {
		t.Fatalf("status = %s, refunds = %d, sms = %d; a lost answer must wait for the report", msg.Status, len(h.ledger.credits), len(h.provider.sms))
	}

	_ = h.svc.ApplyStatus(context.Background(), carriermessaging.StatusReport{Reference: msg.Reference, Status: carriermessaging.StatusDelivered, ReceivedAt: t0})
	stored, _ := h.messages.FindByID(context.Background(), msg.ID)
	if stored.Status != carriermessaging.StatusDelivered || len(h.ledger.credits) != 0 || len(h.provider.sms) != 0 {
		t.Fatal("the late report settles it as delivered, charged, with no fallback")
	}
}

func TestLostProviderAnswerWithoutAReportExpiresIntoTheFallback(t *testing.T) {
	h := newHarness(t)
	h.provider.err = carriermessaging.ErrSendOutcomeUnknown
	req := marketing(rcs.Content{Text: "Oi"})
	req.Fallback = carriermessaging.FallbackSpec{SMSRouteID: "route-1", SMSText: "Oi"}
	msg, _ := h.svc.Deliver(context.Background(), req)

	h.now = t0.Add(carriermessaging.FallbackValidity + time.Minute)
	if n, err := h.svc.ExpireOverdue(context.Background(), 10); err != nil || n != 1 {
		t.Fatalf("ExpireOverdue = %d, %v", n, err)
	}
	stored, _ := h.messages.FindByID(context.Background(), msg.ID)
	if stored.Status != carriermessaging.StatusExpired || len(h.ledger.credits) != 1 || len(h.provider.sms) != 1 {
		t.Fatalf("status = %s, refunds = %d, sms = %d", stored.Status, len(h.ledger.credits), len(h.provider.sms))
	}
}

func TestLostSMSAnswerKeepsTheSMSPending(t *testing.T) {
	h := newHarness(t)
	h.provider.outcome = carriermessaging.SendOutcome{ErrorCode: "1"}
	h.provider.smsErr = carriermessaging.ErrSendOutcomeUnknown
	req := marketing(rcs.Content{Text: "Oi"})
	req.Fallback = carriermessaging.FallbackSpec{SMSRouteID: "route-1", SMSText: "Oi"}
	msg, _ := h.svc.Deliver(context.Background(), req)

	parent, _ := h.messages.FindByID(context.Background(), msg.ID)
	child, _ := h.messages.FindByID(context.Background(), parent.FallbackMessageID)
	if child.Status != carriermessaging.StatusPending || hasCredit(h.ledger, "refund:"+child.ChargeReference) {
		t.Fatalf("sms step = %+v", child)
	}
}

func hasCredit(l *fakeLedger, reference string) bool {
	for _, c := range l.credits {
		if *c.ReferenceID == reference {
			return true
		}
	}
	return false
}
