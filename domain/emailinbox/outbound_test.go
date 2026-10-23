package emailinbox

import (
	"reflect"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/emailsending"
)

func TestOutboundExternalID(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		message  string
		want     string
	}{
		{"provider id wins", " ses-1 ", "msg-1", "ses-1"},
		{"falls back to our id", "", "msg-1", "msg-1"},
		{"nothing to deduplicate by", " ", " ", ""},
	}
	for _, tc := range cases {
		if got := OutboundExternalID(tc.provider, tc.message); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestOutboundMessageType(t *testing.T) {
	cases := []struct {
		name string
		by   conversation.SentBy
		want conversation.MessageType
	}{
		{"operator", conversation.SentByPerson("8a1f3c2e-6b7d-4e5f-9a0b-1c2d3e4f5a6b"), conversation.MessageTypeOperator},
		{"agent", conversation.SentByAI("agent-1"), conversation.MessageTypeAIResponse},
		{"platform assistant", conversation.SentByAI(""), conversation.MessageTypeAIResponse},
		{"workflow", conversation.SentByWorkflow("wf-1"), conversation.MessageTypeAIResponse},
		{"campaign, like a WhatsApp campaign send", conversation.SentByCampaign("camp-1"), conversation.MessageTypeOperator},
	}
	for _, tc := range cases {
		if got := OutboundMessageType(tc.by); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestThreadAfterOutbound(t *testing.T) {
	conv := Conversation{Subject: "Pedido 12", ThreadKey: "out1@vozko", LastInboundMessageID: "m1@x", References: []string{"out1@vozko", "m1@x"}}
	cases := []struct {
		name    string
		subject string
		want    Thread
	}{
		{"a new subject names the conversation", "Re: Proposta nova", Thread{Subject: "Proposta nova", ThreadKey: "out1@vozko", LastInboundMessageID: "m1@x", References: []string{"out1@vozko", "m1@x"}}},
		{"a blank subject keeps the old one", "  ", Thread{Subject: "Pedido 12", ThreadKey: "out1@vozko", LastInboundMessageID: "m1@x", References: []string{"out1@vozko", "m1@x"}}},
	}
	for _, tc := range cases {
		if got := conv.ThreadAfterOutbound(tc.subject); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestOutboundEmailValidate(t *testing.T) {
	valid := OutboundEmail{
		WorkspaceID: "ws-1", To: "ana@acme.com.br", SenderID: "sender-1", ExternalID: "ses-1",
		SentBy: conversation.SentByWorkflow("wf-1"),
	}
	cases := []struct {
		name   string
		mutate func(*OutboundEmail)
		ok     bool
	}{
		{"complete", func(*OutboundEmail) {}, true},
		{"no workspace", func(e *OutboundEmail) { e.WorkspaceID = " " }, false},
		{"no address", func(e *OutboundEmail) { e.To = "" }, false},
		{"no external id", func(e *OutboundEmail) { e.ExternalID = "" }, false},
		{"a contact cannot send our email", func(e *OutboundEmail) { e.SentBy = conversation.SentByContact("ana@acme.com.br") }, false},
		{"an operator without an id", func(e *OutboundEmail) { e.SentBy = conversation.SentByPerson("") }, false},
	}
	for _, tc := range cases {
		e := valid
		tc.mutate(&e)
		if err := e.Validate(); (err == nil) != tc.ok {
			t.Errorf("%s: err = %v", tc.name, err)
		}
	}
}

func TestTranscriptStatusFollowsTheSentMessage(t *testing.T) {
	cases := map[emailsending.MessageStatus]conversation.DeliveryStatus{
		emailsending.MessageQueued:     conversation.DeliveryStatusNone,
		emailsending.MessageSent:       conversation.DeliveryStatusSent,
		emailsending.MessageDelivered:  conversation.DeliveryStatusDelivered,
		emailsending.MessageBounced:    conversation.DeliveryStatusFailed,
		emailsending.MessageSuppressed: conversation.DeliveryStatusFailed,
		emailsending.MessageFailed:     conversation.DeliveryStatusFailed,
	}
	for status, want := range cases {
		if got := TranscriptStatus(status); got != want {
			t.Errorf("%s: got %q, want %q", status, got, want)
		}
	}
}

func TestSentOriginalBecomesTheOutboundOfItsMessage(t *testing.T) {
	sentAt := time.Date(2026, 10, 20, 9, 30, 0, 0, time.UTC)
	sent := emailsending.Message{ID: "msg-1", WorkspaceID: "ws-1", ProviderMessageID: "ses-1", Address: "ana@acme.com.br", Status: emailsending.MessageDelivered, SentAt: sentAt}
	original := SentOriginal{Subject: "Oferta de outubro", Text: "Olá Ana", SentBy: conversation.SentByCampaign("camp-1"), CampaignID: "camp-1", CampaignName: "Outubro"}
	want := OutboundEmail{
		WorkspaceID: "ws-1", To: "ana@acme.com.br", Subject: "Oferta de outubro", Text: "Olá Ana", ExternalID: "ses-1", SentMessageID: "msg-1",
		SentBy: conversation.SentByCampaign("camp-1"), SentAt: sentAt, CampaignID: "camp-1", CampaignName: "Outubro",
		DeliveryStatus: conversation.DeliveryStatusDelivered,
	}
	if got := original.OutboundOf(sent); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	unsent := sent
	unsent.ProviderMessageID = ""
	if got := original.OutboundOf(unsent); got.ExternalID != "msg-1" {
		t.Fatalf("external id = %q, want our message id without a provider id", got.ExternalID)
	}
}
