package carriermessaging_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/carriermessaging"
	"vozko/domain/conversation"
	"vozko/domain/rcs"
	"vozko/domain/sms"
)

func rcsConversation(t *testing.T, h *harness) *conversation.EntryContext {
	t.Helper()
	contact, _ := h.contacts.FindOrCreate(context.Background(), "ws-1", "5511999990000", "Ana")
	conv, _, _ := h.convs.FindOrCreate(context.Background(), "ws-1", "sender-1", contact.ID)
	ec, err := h.svc.Adapter(carriermessaging.ChannelRCS).ResolveEntry(context.Background(), conv.ID)
	if err != nil {
		t.Fatalf("ResolveEntry = %v", err)
	}
	return ec
}

func TestAdapterResolvesTheConversation(t *testing.T) {
	h := newHarness(t)
	ec := rcsConversation(t, h)
	if ec.EntryType != carriermessaging.ChannelRCS.EntryType() || ec.WorkspaceID != "ws-1" || ec.AccountID != "sender-1" || ec.ContactRef != "5511999990000" || ec.ContactHandle != "Ana" {
		t.Fatalf("entry = %+v", ec)
	}
}

func TestAdapterWindowIsAlwaysOpenWhileTheSenderSends(t *testing.T) {
	h := newHarness(t)
	ec := rcsConversation(t, h)
	adapter := h.svc.Adapter(carriermessaging.ChannelRCS)
	if w, err := adapter.WindowState(context.Background(), ec); err != nil || !w.Open {
		t.Fatalf("WindowState = %+v, %v", w, err)
	}
	h.senders.rows["sender-1"].Status = rcs.SenderPaused
	if w, _ := adapter.WindowState(context.Background(), ec); w.Open || w.Reason != conversation.WindowReasonChannelUnavailable {
		t.Fatalf("paused sender window = %+v", w)
	}
	states, err := adapter.WindowStates(context.Background(), []string{ec.EntryID, "missing"})
	if err != nil || states[ec.EntryID].Open {
		t.Fatalf("states = %+v", states)
	}
	if _, ok := states["missing"]; ok {
		t.Fatal("an unknown conversation gets no state")
	}
}

var (
	_ conversation.ChannelAdapter      = (*ChannelAdapter)(nil)
	_ conversation.WindowStatesAdapter = (*ChannelAdapter)(nil)
)

func TestAdapterWindowStatesCloseConversationsOfARemovedSender(t *testing.T) {
	h := newHarness(t)
	ec := rcsConversation(t, h)
	delete(h.senders.rows, "sender-1")
	states, err := h.svc.Adapter(carriermessaging.ChannelRCS).WindowStates(context.Background(), []string{ec.EntryID})
	if err != nil || states[ec.EntryID].Open || states[ec.EntryID].Reason != conversation.WindowReasonChannelUnavailable {
		t.Fatalf("states = %+v, %v", states, err)
	}
}

func TestAdapterSendTextDeliversAsTheOperatorWithoutASecondTranscript(t *testing.T) {
	h := newHarness(t)
	ec := rcsConversation(t, h)
	out, err := h.svc.Adapter(carriermessaging.ChannelRCS).SendText(context.Background(), ec, conversation.SendTextRequest{Body: "Oi Ana", HumanInitiated: true})
	if err != nil {
		t.Fatalf("SendText = %v", err)
	}
	if len(h.provider.sends) != 1 || h.provider.sends[0].Content.Text != "Oi Ana" || out.ProviderMessageID != h.provider.sends[0].Reference || out.DeliveryStatus != conversation.DeliveryStatusSent {
		t.Fatalf("out = %+v, sends = %+v", out, h.provider.sends)
	}
	if len(h.transcript.records) != 0 {
		t.Fatal("the inbox records adapter sends itself")
	}
	msgs := h.messages.all()
	if len(msgs) != 1 || msgs[0].RunKind != carriermessaging.RunOperator || msgs[0].Marketing {
		t.Fatalf("messages = %+v", msgs)
	}
}

func TestAdapterAutomationSendsAreWorkflowRuns(t *testing.T) {
	h := newHarness(t)
	ec := rcsConversation(t, h)
	if _, err := h.svc.Adapter(carriermessaging.ChannelRCS).SendText(context.Background(), ec, conversation.SendTextRequest{Body: "Oi"}); err != nil {
		t.Fatalf("SendText = %v", err)
	}
	if h.messages.all()[0].RunKind != carriermessaging.RunWorkflow {
		t.Fatal("an automated reply is a workflow run")
	}
}

func TestAdapterReportsAProviderRefusal(t *testing.T) {
	h := newHarness(t)
	ec := rcsConversation(t, h)
	h.provider.outcome = carriermessaging.SendOutcome{ErrorCode: "42", Unreachable: true}
	if _, err := h.svc.Adapter(carriermessaging.ChannelRCS).SendText(context.Background(), ec, conversation.SendTextRequest{Body: "Oi", HumanInitiated: true}); !errors.Is(err, carriermessaging.ErrProviderRefused) {
		t.Fatalf("SendText = %v", err)
	}
}

func TestAdapterSendsRCSMediaFromItsURL(t *testing.T) {
	h := newHarness(t)
	ec := rcsConversation(t, h)
	_, err := h.svc.Adapter(carriermessaging.ChannelRCS).SendMedia(context.Background(), ec, conversation.SendMediaRequest{
		URL: "https://media.example.com/a.png", MIMEType: "image/png", Caption: "Veja", HumanInitiated: true,
	})
	if err != nil {
		t.Fatalf("SendMedia = %v", err)
	}
	sent := h.provider.sends[0].Content
	if sent.Media == nil || sent.Media.Kind != rcs.MediaImage || sent.Text != "Veja" {
		t.Fatalf("content = %+v", sent)
	}
	if _, err := h.svc.Adapter(carriermessaging.ChannelRCS).SendMedia(context.Background(), ec, conversation.SendMediaRequest{Bytes: []byte("x"), MIMEType: "image/png"}); !errors.Is(err, conversation.ErrCapabilityUnsupported) {
		t.Fatalf("media without a public URL = %v", err)
	}
}

func TestAdapterInteractiveBecomesSuggestedReplies(t *testing.T) {
	h := newHarness(t)
	ec := rcsConversation(t, h)
	adapter := h.svc.Adapter(carriermessaging.ChannelRCS)
	_, err := adapter.SendInteractive(context.Background(), ec, conversation.SendInteractiveRequest{
		Body: "Escolha", Options: []conversation.InteractiveOption{{ID: "a", Title: "Sim"}, {ID: "b", Title: "Não"}},
	})
	if err != nil {
		t.Fatalf("SendInteractive = %v", err)
	}
	s := h.provider.sends[0].Content.Suggestions
	if len(s) != 2 || s[0].Label != "Sim" || s[0].Kind != rcs.SuggestReply {
		t.Fatalf("suggestions = %+v", s)
	}
	stored := h.messages.all()[0]
	if stored.Content.Suggestions[1].Postback != "b" {
		t.Fatal("the option id stays the button's postback so the tap returns it")
	}
	if adapter.InteractiveLimits().MaxOptionsButtons != rcs.MaxMessageSuggestions {
		t.Fatal("limits come from the RCS capabilities")
	}
}

func TestSMSAdapterSendsTextOnly(t *testing.T) {
	h := newHarness(t)
	contact, _ := h.contacts.FindOrCreate(context.Background(), "ws-1", "5511999990000", "")
	conv, _, _ := h.smsConvs.FindOrCreate(context.Background(), "ws-1", "route-1", contact.ID)
	adapter := h.svc.Adapter(carriermessaging.ChannelSMS)
	ec, err := adapter.ResolveEntry(context.Background(), conv.ID)
	if err != nil {
		t.Fatalf("ResolveEntry = %v", err)
	}
	if _, err := adapter.SendText(context.Background(), ec, conversation.SendTextRequest{Body: "Oi", HumanInitiated: true}); err != nil {
		t.Fatalf("SendText = %v", err)
	}
	if len(h.provider.sms) != 1 || h.provider.sms[0].Text != "Loja: Oi" {
		t.Fatalf("sms = %+v", h.provider.sms)
	}
	if _, err := adapter.SendMedia(context.Background(), ec, conversation.SendMediaRequest{URL: "https://a.example.com/a.png", MIMEType: "image/png"}); !errors.Is(err, conversation.ErrCapabilityUnsupported) {
		t.Fatalf("SMS media = %v", err)
	}
	h.routes.rows["route-1"].Status = sms.RoutePaused
	if w, _ := adapter.WindowState(context.Background(), ec); w.Open {
		t.Fatal("a paused route closes the window")
	}
}
