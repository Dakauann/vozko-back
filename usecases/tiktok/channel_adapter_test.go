package tiktok

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/channel"
	"vozko/domain/conversation"
	"vozko/domain/shared"
	ttdomain "vozko/domain/tiktok"
)

type adapterHarness struct {
	a             *ChannelAdapter
	account       *ttdomain.Account
	accounts      *fakeAccounts
	contacts      *fakeContacts
	conversations *fakeConversations
	svc           *fakeServices
	side          *SideMessages
	state         *fakeSharedState
	ec            *conversation.EntryContext
	conv          *ttdomain.Conversation
	metrics       *fakeMetrics
}

func newAdapterHarness(t *testing.T) adapterHarness {
	t.Helper()
	accounts := newFakeAccounts()
	account := accounts.put(connectedAccount())
	contacts := newFakeContacts()
	conversations := newFakeConversations()
	svc := newFakeServices()
	metrics := &fakeMetrics{}
	tokens := NewTokenSource(accounts, svc, fakeClassifier{}, &fakeAlerts{}, metrics)
	tokens.now = fixedClock
	state := newFakeSharedState()
	side := NewSideMessages(state)
	a := NewChannelAdapter(ChannelAdapterDeps{
		Accounts: accounts, Contacts: contacts, Conversations: conversations, Tokens: tokens, Messaging: svc,
		Errors: fakeClassifier{}, Fetcher: &fakeFetcher{data: map[string][]byte{"https://media.example.com/a.png": []byte("\x89PNG")}}, Side: side, Metrics: metrics,
	})
	a.now = fixedClock
	ctx := context.Background()
	contact, _ := contacts.FindOrCreate(ctx, ttdomain.FindOrCreateContactInput{WorkspaceID: account.WorkspaceID, AccountID: account.ID, UniqueIdentifier: "+person==", Username: "ana"})
	conv, _, _ := conversations.FindOrCreate(ctx, ttdomain.FindOrCreateConversationInput{WorkspaceID: account.WorkspaceID, AccountID: account.ID, ContactID: contact.ID, ConversationID: "S:1:abc"})
	_ = conversations.RecordInbound(ctx, conv.ID, testNow.Add(-time.Hour))
	ec, err := a.ResolveEntry(ctx, conv.ID)
	if err != nil {
		t.Fatal(err)
	}
	return adapterHarness{a: a, account: account, accounts: accounts, contacts: contacts, conversations: conversations, svc: svc, side: side, state: state, ec: ec, conv: conv, metrics: metrics}
}

func TestResolveEntryNamesTheTikTokPerson(t *testing.T) {
	h := newAdapterHarness(t)
	if h.ec.EntryType != shared.EntryTypeTikTok || h.ec.ContactRef != "+person==" || h.ec.ContactHandle != "@ana" || h.ec.AccountID != h.account.ID {
		t.Fatalf("entry context = %+v", h.ec)
	}
}

func TestSendTextClaimsTheBudgetAndReturnsTheProviderID(t *testing.T) {
	h := newAdapterHarness(t)
	out, err := h.a.SendText(context.Background(), h.ec, conversation.SendTextRequest{Body: "Olá!", ReplyToProviderMessageID: "in-1"})
	if err != nil {
		t.Fatal(err)
	}
	if out.ProviderMessageID != "out-1" || out.DeliveryStatus != conversation.DeliveryStatusSent {
		t.Errorf("outcome = %+v", out)
	}
	if len(h.svc.sentTexts) != 1 || h.svc.sentTexts[0].conversationID != "S:1:abc" || h.svc.sentTexts[0].replyTo != "in-1" {
		t.Errorf("sent = %+v", h.svc.sentTexts)
	}
	conv, _ := h.conversations.FindByID(context.Background(), h.conv.ID)
	if conv.OutboundSinceInbound != 1 || len(h.conversations.outbound[conv.ID]) != 1 {
		t.Errorf("budget %d outbound clock %v", conv.OutboundSinceInbound, h.conversations.outbound[conv.ID])
	}
}

func TestSendTextRefusesEmptyAndTooLongBodiesBeforeAnyClaim(t *testing.T) {
	h := newAdapterHarness(t)
	if _, err := h.a.SendText(context.Background(), h.ec, conversation.SendTextRequest{Body: "  "}); !errors.Is(err, ttdomain.ErrTextEmpty) {
		t.Errorf("empty: %v", err)
	}
	if _, err := h.a.SendText(context.Background(), h.ec, conversation.SendTextRequest{Body: strings.Repeat("a", ttdomain.MaxTextRunes+1)}); !errors.Is(err, ttdomain.ErrTextTooLong) {
		t.Errorf("long: %v", err)
	}
	if conv, _ := h.conversations.FindByID(context.Background(), h.conv.ID); conv.OutboundSinceInbound != 0 {
		t.Error("a refused body must not spend the budget")
	}
}

func TestTheEleventhMessageIsRefusedWithoutCallingTikTok(t *testing.T) {
	h := newAdapterHarness(t)
	for i := 0; i < ttdomain.OutboundBudget; i++ {
		if _, err := h.a.SendText(context.Background(), h.ec, conversation.SendTextRequest{Body: "oi"}); err != nil {
			t.Fatalf("send %d: %v", i+1, err)
		}
	}
	_, err := h.a.SendText(context.Background(), h.ec, conversation.SendTextRequest{Body: "oi"})
	if !errors.Is(err, conversation.ErrOutboundWindowClosed) {
		t.Fatalf("err = %v", err)
	}
	if len(h.svc.sentTexts) != ttdomain.OutboundBudget {
		t.Fatalf("tiktok called %d times", len(h.svc.sentTexts))
	}
	state, _ := h.a.WindowState(context.Background(), h.ec)
	if state.Open || state.Reason != conversation.WindowReasonBudgetExhausted {
		t.Errorf("window = %+v", state)
	}
}

func TestAClaimErrorSendsNothing(t *testing.T) {
	h := newAdapterHarness(t)
	h.conversations.claimErr = errors.New("database down")
	if _, err := h.a.SendText(context.Background(), h.ec, conversation.SendTextRequest{Body: "oi"}); err == nil {
		t.Fatal("a claim error must refuse the send")
	}
	if len(h.svc.sentTexts) != 0 {
		t.Fatal("tiktok was called without a claim")
	}
}

func TestADefiniteRefusalReleasesTheClaimAndAnAmbiguousFailureKeepsIt(t *testing.T) {
	h := newAdapterHarness(t)
	h.svc.sendErr = codedError{invalid: true, refusal: true}
	if _, err := h.a.SendText(context.Background(), h.ec, conversation.SendTextRequest{Body: "oi"}); err == nil {
		t.Fatal("expected the refusal")
	}
	if conv, _ := h.conversations.FindByID(context.Background(), h.conv.ID); conv.OutboundSinceInbound != 0 || h.conversations.released != 1 {
		t.Fatalf("refusal: budget %d released %d", conv.OutboundSinceInbound, h.conversations.released)
	}
	h.svc.sendErr = codedError{ambiguous: true}
	if _, err := h.a.SendText(context.Background(), h.ec, conversation.SendTextRequest{Body: "oi"}); err == nil {
		t.Fatal("expected the ambiguous failure")
	}
	if conv, _ := h.conversations.FindByID(context.Background(), h.conv.ID); conv.OutboundSinceInbound != 1 || h.conversations.released != 1 {
		t.Fatalf("ambiguous: budget %d released %d", conv.OutboundSinceInbound, h.conversations.released)
	}
}

func TestTikTokBlockingTheMessageClosesTheConversation(t *testing.T) {
	h := newAdapterHarness(t)
	h.svc.sendErr = codedError{budget: true, refusal: true}
	_, err := h.a.SendText(context.Background(), h.ec, conversation.SendTextRequest{Body: "oi"})
	if !errors.Is(err, conversation.ErrOutboundWindowClosed) {
		t.Fatalf("err = %v", err)
	}
	if _, closed := h.conversations.closedAt[h.conv.ID]; !closed {
		t.Fatal("a 40064 must close the conversation")
	}
	state, _ := h.a.WindowState(context.Background(), h.ec)
	if state.Open || state.Reason != conversation.WindowReasonBudgetExhausted {
		t.Errorf("window = %+v", state)
	}
}

func TestSendsRefuseAnAccountThatCannotSend(t *testing.T) {
	h := newAdapterHarness(t)
	h.account.Capabilities.CanSendMessages = false
	h.accounts.put(h.account)
	if _, err := h.a.SendText(context.Background(), h.ec, conversation.SendTextRequest{Body: "oi"}); !errors.Is(err, conversation.ErrOutboundWindowClosed) {
		t.Fatalf("err = %v", err)
	}
	state, _ := h.a.WindowState(context.Background(), h.ec)
	if state.Reason != conversation.WindowReasonChannelUnavailable {
		t.Errorf("window = %+v", state)
	}
}

func TestSendImageUploadsAndChecksTheMarketCapabilityOnce(t *testing.T) {
	h := newAdapterHarness(t)
	h.svc.imageOK = true
	req := conversation.SendMediaRequest{Kind: string(channel.MediaImage), URL: "https://media.example.com/a.png", MIMEType: "image/png", FileName: "a.png"}
	if _, err := h.a.SendMedia(context.Background(), h.ec, req); err != nil {
		t.Fatal(err)
	}
	if h.svc.uploads != 1 || len(h.svc.sentImages) != 1 {
		t.Fatalf("uploads %d images %v", h.svc.uploads, h.svc.sentImages)
	}
	h.svc.imageErr = errors.New("must not be asked again")
	if _, err := h.a.SendMedia(context.Background(), h.ec, req); err != nil {
		t.Fatalf("the cached capability must be reused: %v", err)
	}
}

func TestImagesAreRefusedWhenTikTokRefusesOrCannotConfirmThem(t *testing.T) {
	h := newAdapterHarness(t)
	req := conversation.SendMediaRequest{Kind: string(channel.MediaImage), Bytes: []byte("\x89PNG"), MIMEType: "image/png"}
	h.svc.imageOK = false
	if _, err := h.a.SendMedia(context.Background(), h.ec, req); !errors.Is(err, ttdomain.ErrImageNotAllowedInMarket) {
		t.Fatalf("refused market: %v", err)
	}
	other := newAdapterHarness(t)
	other.svc.imageErr = errors.New("timeout")
	if _, err := other.a.SendMedia(context.Background(), other.ec, req); !errors.Is(err, conversation.ErrCapabilityUnsupported) {
		t.Fatalf("unknown capability: %v", err)
	}
	if len(h.svc.sentImages)+len(other.svc.sentImages) != 0 {
		t.Fatal("no image may be sent without a confirmed capability")
	}
}

func TestSendMediaRefusesWhatTikTokCannotReceive(t *testing.T) {
	h := newAdapterHarness(t)
	cases := []conversation.SendMediaRequest{
		{Kind: string(channel.MediaVideo), Bytes: []byte("x"), MIMEType: "video/mp4"},
		{Kind: string(channel.MediaImage), Bytes: []byte("x"), MIMEType: "image/gif"},
		{Kind: string(channel.MediaImage), Bytes: make([]byte, ttdomain.MaxImageBytes+1), MIMEType: "image/jpeg"},
	}
	for _, req := range cases {
		if _, err := h.a.SendMedia(context.Background(), h.ec, req); err == nil {
			t.Errorf("%s %s accepted", req.Kind, req.MIMEType)
		}
	}
	if conv, _ := h.conversations.FindByID(context.Background(), h.conv.ID); conv.OutboundSinceInbound != 0 {
		t.Error("refused media must not spend the budget")
	}
}

func TestACaptionGoesFirstAndIsMarkedAsASideMessage(t *testing.T) {
	h := newAdapterHarness(t)
	h.svc.imageOK = true
	req := conversation.SendMediaRequest{Kind: string(channel.MediaImage), Bytes: []byte("\x89PNG"), MIMEType: "image/png", Caption: "Nosso cardápio"}
	if _, err := h.a.SendMedia(context.Background(), h.ec, req); err != nil {
		t.Fatal(err)
	}
	if len(h.svc.sentTexts) != 1 || h.svc.sentTexts[0].text != "Nosso cardápio" || len(h.svc.sentImages) != 1 {
		t.Fatalf("texts %v images %v", h.svc.sentTexts, h.svc.sentImages)
	}
	if !h.side.Seen("out-1") {
		t.Fatal("the caption must be marked so its echo is not recorded twice")
	}
	if conv, _ := h.conversations.FindByID(context.Background(), h.conv.ID); conv.OutboundSinceInbound != 2 {
		t.Errorf("a caption and an image are two messages for the budget, got %d", conv.OutboundSinceInbound)
	}
}

func TestSendInteractiveBuildsACardWithinTikTokLimits(t *testing.T) {
	h := newAdapterHarness(t)
	req := conversation.SendInteractiveRequest{Body: "Qual tamanho?", Options: []conversation.InteractiveOption{
		{ID: "p", Title: "Pequeno"}, {ID: "m", Title: "Médio com uma descrição longa demais"}, {ID: "g", Title: "Grande"}, {ID: "gg", Title: "Extra"},
	}}
	if _, err := h.a.SendInteractive(context.Background(), h.ec, req); err != nil {
		t.Fatal(err)
	}
	card := h.svc.sentCards[0]
	if card.Title != "Qual tamanho?" || len(card.Buttons) != ttdomain.MaxCardButtons {
		t.Fatalf("card = %+v", card)
	}
	if n := len([]rune(card.Buttons[1].Text)); n > ttdomain.MaxCardButtonRunes {
		t.Errorf("label has %d runes", n)
	}
	if len(h.svc.sentTexts) != 0 {
		t.Error("a short body fits the card title")
	}
}

func TestALongPromptGoesAsTextBeforeTheCard(t *testing.T) {
	h := newAdapterHarness(t)
	body := "Olá! Para continuar o seu atendimento, escolha uma das opções abaixo, por favor."
	req := conversation.SendInteractiveRequest{Body: body, Options: []conversation.InteractiveOption{{ID: "a", Title: "Sim"}, {ID: "b", Title: "Não"}}}
	if _, err := h.a.SendInteractive(context.Background(), h.ec, req); err != nil {
		t.Fatal(err)
	}
	if len(h.svc.sentTexts) != 1 || h.svc.sentTexts[0].text != body || h.svc.sentCards[0].Title != cardFallbackTitle {
		t.Fatalf("texts %v cards %v", h.svc.sentTexts, h.svc.sentCards)
	}
	if !h.side.Seen("out-1") {
		t.Error("the prompt text is a side message")
	}
}

func TestWindowStatesCoverAPageOfConversations(t *testing.T) {
	h := newAdapterHarness(t)
	states, err := h.a.WindowStates(context.Background(), []string{h.conv.ID, "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || !states[h.conv.ID].Open {
		t.Fatalf("states = %+v", states)
	}
}

func TestPresenceUsesSenderActions(t *testing.T) {
	h := newAdapterHarness(t)
	if err := h.a.SendTyping(context.Background(), h.ec, true); err != nil {
		t.Fatal(err)
	}
	if err := h.a.SendTyping(context.Background(), h.ec, false); err != nil {
		t.Fatal(err)
	}
	if err := h.a.MarkSeen(context.Background(), h.ec, ""); err != nil {
		t.Fatal(err)
	}
	if len(h.svc.actions) != 2 || h.svc.actions[0] != ttdomain.SenderTyping || h.svc.actions[1] != ttdomain.SenderMarkRead {
		t.Fatalf("actions = %v", h.svc.actions)
	}
}

func TestTheCardOutcomeNamesOnlyTheButtonsThatFit(t *testing.T) {
	h := newAdapterHarness(t)
	req := conversation.SendInteractiveRequest{Body: "Qual tamanho?", Options: []conversation.InteractiveOption{
		{ID: "p", Title: "Pequeno"}, {ID: "m", Title: "Médio"}, {ID: "g", Title: "Grande"}, {ID: "gg", Title: "Extra"},
	}}
	out, err := h.a.SendInteractive(context.Background(), h.ec, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.ShownOptionIDs) != ttdomain.MaxCardButtons || out.ShownOptionIDs[0] != "p" {
		t.Fatalf("shown = %v, want the first %d", out.ShownOptionIDs, ttdomain.MaxCardButtons)
	}
}
