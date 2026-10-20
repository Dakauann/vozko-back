package conversation_usecase

import (
	"context"
	"testing"

	"vozko/domain/conversation"
)

type buttonCapturingClient struct {
	conversation.WhatsAppClient
	sent []conversation.SendButtonMessageInput
}

func (c *buttonCapturingClient) SendButtonMessage(_ context.Context, in conversation.SendButtonMessageInput) (*conversation.SendTextMessageOutput, error) {
	c.sent = append(c.sent, in)
	return &conversation.SendTextMessageOutput{MessageID: "wamid.button"}, nil
}

type quotedMessageRepo struct {
	stubAudioMessageRepo
	quoted *conversation.Message
}

func (r *quotedMessageRepo) GetByID(id string) (*conversation.Message, error) {
	if r.quoted != nil && r.quoted.ID == id {
		return r.quoted, nil
	}
	return nil, conversation.ErrMessageNotFound
}

func TestAButtonReplySendsTheQuoteToWhatsAppLikeTextAndMedia(t *testing.T) {
	wamid := "wamid.customer"
	client := &buttonCapturingClient{}
	messages := &quotedMessageRepo{quoted: &conversation.Message{ID: "msg-quoted", WhatsAppMessageID: &wamid}}
	svc := &MessageSenderService{
		whatsappRepo:          stubAudioEntryRepo{},
		leadRepo:              stubAudioLeadRepo{},
		messageWindowRepo:     stubAudioWindowRepo{},
		whatsappClientFactory: stubAudioClientFactory{client: client},
		messageRepo:           messages,
	}

	sent, err := svc.SendButtonMessage("entry-1", "whatsapp", conversation.SendButtonInput{
		BodyText: "Escolha", Buttons: []conversation.ButtonPayload{{ID: "yes", Title: "Sim"}},
	}, conversation.SendOrigin{SenderUserID: "user-1", ReplyToMessageID: "msg-quoted", ClientRequestID: "req-1"})
	if err != nil {
		t.Fatal(err)
	}

	if len(client.sent) != 1 || client.sent[0].ContextMessageID != wamid {
		t.Fatalf("sent = %+v, want the quoted wamid as context", client.sent)
	}
	if sent.ReplyToMessageID == nil || *sent.ReplyToMessageID != "msg-quoted" {
		t.Fatalf("stored reply_to = %v", sent.ReplyToMessageID)
	}
	if buttons, ok := sent.Buttons(); !ok || buttons.Buttons[0].Title != "Sim" {
		t.Fatalf("the stored message lost its buttons, so it could never be resent: %+v", buttons)
	}
	if sent.ClientRequestID != "req-1" || sent.SenderUserID() != "user-1" {
		t.Fatalf("stored request %q sender %q", sent.ClientRequestID, sent.SenderUserID())
	}
}
