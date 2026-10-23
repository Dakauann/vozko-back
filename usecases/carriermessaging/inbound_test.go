package carriermessaging_usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"vozko/domain/carriermessaging"
	"vozko/domain/rcs"
)

func reply(id, text string, at time.Time) carriermessaging.Inbound {
	return carriermessaging.Inbound{ProviderMessageID: id, Phone: "5511999990000", ContactName: "Ana", Channel: carriermessaging.ChannelRCS, To: "profile-1", Text: text, ReceivedAt: at}
}

func deliveredMessage(t *testing.T, h *harness, content rcs.Content) *carriermessaging.Message {
	t.Helper()
	msg := sentMessage(t, h, carriermessaging.FallbackSpec{})
	if content.Text != "" || len(content.Cards) > 0 {
		h.messages.rows[msg.ID].Content = content
	}
	_ = h.svc.ApplyStatus(context.Background(), carriermessaging.StatusReport{Reference: msg.Reference, Status: carriermessaging.StatusDelivered, ReceivedAt: t0})
	return msg
}

func TestInboundReplyOnANonConversationalSender(t *testing.T) {
	h := newHarness(t)
	msg := deliveredMessage(t, h, rcs.Content{})
	h.now = t0.Add(time.Minute)

	if err := h.svc.HandleInbound(context.Background(), reply("in-1", "Quero", h.now)); err != nil {
		t.Fatalf("HandleInbound = %v", err)
	}
	if !hasDebit(h.ledger, carriermessaging.ChargeReference(carriermessaging.ChargeRCSInbound, "in-1")) {
		t.Fatalf("debits = %+v", h.ledger.debits)
	}
	for _, d := range h.ledger.debits {
		if *d.ReferenceID == carriermessaging.ChargeReference(carriermessaging.ChargeRCSInbound, "in-1") && !d.AllowNegative {
			t.Fatal("a reply cannot be refused, its charge may go negative")
		}
	}
	last := h.transcript.records[len(h.transcript.records)-1]
	if last.EntryID != msg.ConversationID || last.Text != "Quero" || last.ProviderMessageID != "in-1" || last.EntryType != carriermessaging.ChannelRCS.EntryType() {
		t.Fatalf("transcript = %+v", last)
	}
	if len(h.inbox.assignments) != 1 || len(h.inbox.dispatched) != 1 || h.inbox.dispatched[0].Text != "Quero" || h.inbox.dispatched[0].LeadID == nil {
		t.Fatalf("inbox = %+v", h.inbox)
	}
	replies := 0
	for _, e := range h.events.rows {
		if e.Kind == carriermessaging.EventInbound && e.MessageID == msg.ID {
			replies++
		}
	}
	if replies != 1 {
		t.Fatalf("the reply must be attributed to the delivered message, events = %+v", h.events.rows)
	}
}

func TestDuplicateInboundIsIgnored(t *testing.T) {
	h := newHarness(t)
	_ = h.svc.HandleInbound(context.Background(), reply("in-1", "Oi", t0))
	records := len(h.transcript.records)
	_ = h.svc.HandleInbound(context.Background(), reply("in-1", "Oi", t0))
	if len(h.transcript.records) != records || len(h.ledger.debits) != 1 || len(h.inbox.dispatched) != 1 {
		t.Fatal("a provider retry must not record, charge or dispatch twice")
	}
}

func TestOptOutKeywordSuppressesWithoutAnAutomatedAnswer(t *testing.T) {
	h := newHarness(t)
	if err := h.svc.HandleInbound(context.Background(), reply("in-1", "SAIR", t0)); err != nil {
		t.Fatalf("HandleInbound = %v", err)
	}
	if ok, _ := h.suppressions.IsSuppressed(context.Background(), "ws-1", "5511999990000"); !ok {
		t.Fatal("SAIR must suppress the contact")
	}
	if len(h.inbox.dispatched) != 0 {
		t.Fatal("an opt-out gets no automated answer")
	}
	if len(h.transcript.records) != 1 {
		t.Fatal("the opt-out is still in the conversation")
	}
}

func TestFloodingContactIsFlaggedOnceAndLeftUnanswered(t *testing.T) {
	h := newHarness(t)
	for i := range carriermessaging.FloodThreshold + 3 {
		h.now = t0.Add(time.Duration(i) * time.Second)
		if err := h.svc.HandleInbound(context.Background(), reply(fmt.Sprintf("in-%d", i), "spam", h.now)); err != nil {
			t.Fatalf("HandleInbound = %v", err)
		}
	}
	if len(h.notices.flooding) != 1 {
		t.Fatalf("flood alerts = %d, want 1", len(h.notices.flooding))
	}
	if len(h.inbox.dispatched) != carriermessaging.FloodThreshold {
		t.Fatalf("dispatched = %d, automation stops past the threshold", len(h.inbox.dispatched))
	}
	if len(h.ledger.debits) != carriermessaging.FloodThreshold+3 {
		t.Fatal("every received message is still charged")
	}
}

func TestConversationalReplyOpensAConversationAndReplacesTheMessageCharge(t *testing.T) {
	h := newHarness(t)
	h.senders.rows["sender-1"].BillingCategory = rcs.BillingConversational
	msg := deliveredMessage(t, h, rcs.Content{})
	h.now = t0.Add(time.Hour)

	_ = h.svc.HandleInbound(context.Background(), reply("in-1", "Oi", h.now))
	_ = h.svc.HandleInbound(context.Background(), reply("in-2", "Tudo bem?", h.now.Add(time.Minute)))

	conversationCharges := 0
	for _, d := range h.ledger.debits {
		switch {
		case d.ServiceType == LedgerService(carriermessaging.ChannelRCS) && *d.ReferenceID == carriermessaging.ChargeReference(carriermessaging.ChargeRCSInbound, "in-1"):
			t.Fatal("conversational replies are not charged one by one")
		case strings.HasPrefix(*d.ReferenceID, "carrier:rcs_conversation:"):
			conversationCharges++
			if !d.AllowNegative {
				t.Fatal("a conversation opened by a reply may go negative")
			}
		}
	}
	if conversationCharges != 1 {
		t.Fatalf("conversation charges = %d, want 1", conversationCharges)
	}
	refunded := false
	for _, c := range h.ledger.credits {
		if *c.ReferenceID == "refund:"+msg.ChargeReference {
			refunded = true
		}
	}
	if !refunded {
		t.Fatal("the conversation replaces the charge of the message that started it")
	}
}

func TestConversationalReplyWithoutARecentMessageIsFree(t *testing.T) {
	h := newHarness(t)
	h.senders.rows["sender-1"].BillingCategory = rcs.BillingConversational
	_ = h.svc.HandleInbound(context.Background(), reply("in-1", "Oi", t0))
	if len(h.ledger.debits) != 0 {
		t.Fatalf("debits = %+v", h.ledger.debits)
	}
}

func TestInboundForAnUnknownSenderIsRefused(t *testing.T) {
	h := newHarness(t)
	in := reply("in-1", "Oi", t0)
	in.To = "someone-else"
	if err := h.svc.HandleInbound(context.Background(), in); !errors.Is(err, rcs.ErrSenderNotFound) {
		t.Fatalf("HandleInbound = %v", err)
	}
}

var buttons = rcs.Content{Text: "Escolha", Suggestions: []rcs.Suggestion{
	{Kind: rcs.SuggestReply, Label: "Quero", Postback: "want"},
	{Kind: rcs.SuggestReply, Label: "Sair da lista", Postback: rcs.OptOutPostback},
	{Kind: rcs.SuggestOpenURL, Label: "Ver", URL: "https://loja.example.com/p"},
}}

func TestReplyButtonTapIsRecordedAndChosen(t *testing.T) {
	h := newHarness(t)
	msg := deliveredMessage(t, h, buttons)

	click := carriermessaging.SuggestionClick{EventID: "ev-1", Reference: msg.Reference, Phone: "5511999990000", To: "profile-1", Postback: (rcs.ButtonKey{Card: -1, Index: 0}).Postback(), Label: "Quero", OccurredAt: t0}
	for range 2 {
		if err := h.svc.HandleSuggestionClick(context.Background(), click); err != nil {
			t.Fatalf("HandleSuggestionClick = %v", err)
		}
	}
	taps := 0
	for _, e := range h.events.rows {
		if e.Kind == carriermessaging.EventTap && e.MessageID == msg.ID && e.Button != nil && *e.Button == (rcs.ButtonKey{Card: -1, Index: 0}) {
			taps++
		}
	}
	if taps != 1 {
		t.Fatalf("taps = %d, a retried event counts once", taps)
	}
	if len(h.inbox.dispatched) != 1 {
		t.Fatalf("dispatched = %d", len(h.inbox.dispatched))
	}
	sel := h.inbox.dispatched[0].Selection
	if sel == nil || sel.ID != "want" || sel.Title != "Quero" || sel.Kind != SelectionKind {
		t.Fatalf("selection = %+v", sel)
	}
}

func TestOptOutButtonSuppresses(t *testing.T) {
	h := newHarness(t)
	msg := deliveredMessage(t, h, buttons)
	click := carriermessaging.SuggestionClick{EventID: "ev-1", Reference: msg.Reference, Phone: "5511999990000", Postback: (rcs.ButtonKey{Card: -1, Index: 1}).Postback(), OccurredAt: t0}
	if err := h.svc.HandleSuggestionClick(context.Background(), click); err != nil {
		t.Fatalf("HandleSuggestionClick = %v", err)
	}
	if ok, _ := h.suppressions.IsSuppressed(context.Background(), "ws-1", "5511999990000"); !ok {
		t.Fatal("the opt-out button must suppress")
	}
	if len(h.inbox.dispatched) != 0 {
		t.Fatal("an opt-out gets no automated answer")
	}
}

func TestInboundCarryingAPostbackIsATap(t *testing.T) {
	h := newHarness(t)
	msg := deliveredMessage(t, h, buttons)
	in := reply("in-1", "Quero", t0)
	in.InReplyTo, in.Postback = msg.Reference, (rcs.ButtonKey{Card: -1, Index: 0}).Postback()
	if err := h.svc.HandleInbound(context.Background(), in); err != nil {
		t.Fatalf("HandleInbound = %v", err)
	}
	if sel := h.inbox.dispatched[len(h.inbox.dispatched)-1].Selection; sel == nil || sel.ID != "want" {
		t.Fatalf("selection = %+v", sel)
	}
}

func TestTrackedLinkClickRedirectsAndCounts(t *testing.T) {
	h := newHarness(t)
	msg := deliveredMessage(t, h, buttons)
	token := msg.ID + "." + (rcs.ButtonKey{Card: -1, Index: 2}).Postback()

	target, err := h.svc.ResolveLinkClick(context.Background(), token, t0)
	if err != nil || target != "https://loja.example.com/p" {
		t.Fatalf("ResolveLinkClick = %q, %v", target, err)
	}
	clicks := 0
	for _, e := range h.events.rows {
		if e.Kind == carriermessaging.EventClick && e.MessageID == msg.ID {
			clicks++
		}
	}
	if clicks != 1 {
		t.Fatalf("clicks = %d", clicks)
	}
	if _, err := h.svc.ResolveLinkClick(context.Background(), msg.ID+"."+(rcs.ButtonKey{Card: -1, Index: 0}).Postback(), t0); !errors.Is(err, ErrLinkInvalid) {
		t.Fatalf("a reply button has no link: %v", err)
	}
}

func TestInboundCarriesTheSenderAutomation(t *testing.T) {
	h := newHarness(t)
	agent := "agent-1"
	h.senders.rows["sender-1"].Automation.AgentID, h.senders.rows["sender-1"].Automation.EnableAgentResponses = &agent, true
	_ = h.svc.HandleInbound(context.Background(), reply("in-1", "Oi", t0))
	if got := h.inbox.dispatched[0].Config; got.AgentID == nil || *got.AgentID != "agent-1" || !got.EnableAgentResponses {
		t.Fatalf("config = %+v", got)
	}
}
