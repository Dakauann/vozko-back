package conversation_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/whatsappform"
)

type formReplies struct {
	got      []InboundFormReply
	received ReceivedFormReply
	err      error
}

func (f *formReplies) ReceiveFormReply(_ context.Context, in InboundFormReply) (ReceivedFormReply, error) {
	f.got = append(f.got, in)
	return f.received, f.err
}

func formReplyMessage() *conversation.WhatsAppMessage {
	return &conversation.WhatsAppMessage{
		ID: "wamid.in", From: "551187654321", Timestamp: "1760875200", Type: "interactive",
		Context:     &conversation.WhatsAppContext{ID: "wamid.out"},
		Interactive: &conversation.WhatsAppInteractive{Type: conversation.WhatsAppFormReplyType, NFMReply: &conversation.WhatsAppNFMReply{ResponseJSON: `{"flow_token":"vf1_x"}`, Name: "flow"}},
	}
}

func formTargetFor() formReplyTarget {
	return formReplyTarget{workspaceID: "ws-1", entryID: "entry-1", entryType: shared.EntryTypeWhatsApp, conversationID: "conv-1", leadID: "lead-1", businessPhoneID: "phone-1"}
}

func TestExtractKeepsAFormAnswerAsTextWithItsPayload(t *testing.T) {
	uc := &handleWhatsAppMessageUseCase{}
	payload := &conversation.WhatsAppWebhookPayload{Entry: []conversation.WhatsAppEntry{{Changes: []conversation.WhatsAppChange{{Field: "messages", Value: conversation.WhatsAppChangeValue{
		Messages: []conversation.WhatsAppMessage{*formReplyMessage()},
	}}}}}}
	msg, _, _ := uc.extractInboundMessage(payload)
	if msg == nil || msg.Text == nil || msg.Text.Body != conversation.FormReplyPlaceholder {
		t.Fatalf("a form answer must never be skipped, got %+v", msg)
	}
	if _, ok := msg.FormReply(); !ok {
		t.Fatal("the payload travels with the message")
	}
}

func TestAFormAnswerBecomesTheSummaryTheReceiverReturns(t *testing.T) {
	replies := &formReplies{received: ReceivedFormReply{Text: "Formulário \"Cadastro\" respondido\nNome: Ana", Metadata: map[string]any{"form": "card"}, ResumedRunID: "run-1"}}
	uc := &handleWhatsAppMessageUseCase{formReplies: replies}
	msg := formReplyMessage()
	msg.Text = &conversation.WhatsAppTextMessage{Body: conversation.FormReplyPlaceholder}
	got, err := uc.receiveFormReply(context.Background(), msg, formTargetFor())
	if err != nil || got == nil || got.ResumedRunID != "run-1" || msg.Text.Body != replies.received.Text {
		t.Fatalf("got = %+v err = %v body = %q", got, err, msg.Text.Body)
	}
	in := replies.got[0]
	if in.WorkspaceID != "ws-1" || in.EntryID != "entry-1" || in.LeadID != "lead-1" || in.BusinessPhoneID != "phone-1" || in.InboundMessageID != "wamid.in" ||
		in.ContextMessageID != "wamid.out" || in.ContactNumber != "5511987654321" || in.ResponseJSON != `{"flow_token":"vf1_x"}` || in.SubmittedAt.IsZero() {
		t.Fatalf("inbound = %+v", in)
	}
}

func TestAPlainMessageIsNotAFormAnswer(t *testing.T) {
	uc := &handleWhatsAppMessageUseCase{}
	got, err := uc.receiveFormReply(context.Background(), &conversation.WhatsAppMessage{Text: &conversation.WhatsAppTextMessage{Body: "oi"}}, formTargetFor())
	if err != nil || got != nil {
		t.Fatalf("got = %+v err = %v", got, err)
	}
}

func TestFormAnswersAreRetriedWhenTheyCannotBeHandled(t *testing.T) {
	msg := formReplyMessage()
	msg.Text = &conversation.WhatsAppTextMessage{Body: conversation.FormReplyPlaceholder}
	if _, err := (&handleWhatsAppMessageUseCase{}).receiveFormReply(context.Background(), msg, formTargetFor()); !errors.Is(err, errFormRepliesUnwired) {
		t.Fatalf("an unwired receiver must not drop answers, got %v", err)
	}
	failing := &handleWhatsAppMessageUseCase{formReplies: &formReplies{err: errors.New("db down")}}
	if _, err := failing.receiveFormReply(context.Background(), msg, formTargetFor()); err == nil {
		t.Fatal("an infrastructure failure is returned so the webhook retries")
	}
}

func TestAnUnreadableAnswerKeepsThePlaceholder(t *testing.T) {
	msg := formReplyMessage()
	msg.Text = &conversation.WhatsAppTextMessage{Body: conversation.FormReplyPlaceholder}
	uc := &handleWhatsAppMessageUseCase{formReplies: &formReplies{err: whatsappform.ErrResponseInvalid}}
	got, err := uc.receiveFormReply(context.Background(), msg, formTargetFor())
	if err != nil || got != nil || msg.Text.Body != conversation.FormReplyPlaceholder {
		t.Fatalf("got = %+v err = %v", got, err)
	}
}

func TestAFormAnswerWithoutAConversationIsNotHandled(t *testing.T) {
	replies := &formReplies{}
	uc := &handleWhatsAppMessageUseCase{formReplies: replies}
	msg := formReplyMessage()
	msg.Text = &conversation.WhatsAppTextMessage{Body: conversation.FormReplyPlaceholder}
	target := formTargetFor()
	target.entryID = ""
	if got, err := uc.receiveFormReply(context.Background(), msg, target); err != nil || got != nil || len(replies.got) != 0 {
		t.Fatalf("got = %+v err = %v", got, err)
	}
}
