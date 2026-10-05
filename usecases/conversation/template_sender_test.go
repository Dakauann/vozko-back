package conversation_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/conversation"
	whatsappTemplate "vozko/domain/whatsapp/template"
	wo "vozko/domain/whatsapp_outreach"
)

type conversationTemplatesStub struct {
	wo.ConversationTemplateUseCase
	sends []wo.ConversationTemplateInput
	err   error
}

func (s *conversationTemplatesStub) Send(_ context.Context, in wo.ConversationTemplateInput) (*wo.SentConversationTemplate, error) {
	s.sends = append(s.sends, in)
	if s.err != nil {
		return nil, s.err
	}
	return &wo.SentConversationTemplate{MessageID: "wamid.1"}, nil
}

func wiredTemplateSender(templates wo.ConversationTemplateUseCase) *TemplateSenderService {
	sender := NewTemplateSenderService(nil)
	sender.UseConversationTemplates(templates)
	return sender
}

func TestATemplateFromTheConversationGoesThroughTheBilledSend(t *testing.T) {
	templates := &conversationTemplatesStub{}

	id, err := wiredTemplateSender(templates).SendTemplate("entry-1", "whatsapp", "t1", []string{"Ana"}, "u1", "ws1")

	if err != nil || id != "wamid.1" {
		t.Fatalf("id %q err %v", id, err)
	}
	want := wo.ConversationTemplateInput{WorkspaceID: "ws1", UserID: "u1", EntryID: "entry-1", TemplateID: "t1", BodyParams: []string{"Ana"}}
	got := templates.sends[0]
	got.IdempotencyKey = ""
	if len(templates.sends) != 1 || got.WorkspaceID != want.WorkspaceID || got.UserID != want.UserID ||
		got.EntryID != want.EntryID || got.TemplateID != want.TemplateID || len(got.BodyParams) != 1 || got.BodyParams[0] != "Ana" {
		t.Fatalf("sent %+v, want %+v", templates.sends, want)
	}
}

func TestEverySendOfTheSameTemplateIsItsOwnChargedAttempt(t *testing.T) {
	templates := &conversationTemplatesStub{}
	sender := wiredTemplateSender(templates)

	for i := 0; i < 2; i++ {
		if _, err := sender.SendTemplate("entry-1", "whatsapp", "t1", nil, "u1", "ws1"); err != nil {
			t.Fatal(err)
		}
	}

	first, second := templates.sends[0].IdempotencyKey, templates.sends[1].IdempotencyKey
	if first == "" || first == second {
		t.Fatalf("keys %q and %q; the same template sent twice is two sends and must be charged twice", first, second)
	}
}

func TestATemplateSendFailsClosedWithoutTheBilledSend(t *testing.T) {
	if _, err := NewTemplateSenderService(nil).SendTemplate("entry-1", "whatsapp", "t1", nil, "u1", "ws1"); !errors.Is(err, whatsappTemplate.ErrBillingNotConfigured) {
		t.Fatalf("err %v, nothing may go out unbilled", err)
	}
}

func TestATemplateIsOnlySentOnAnOfficialWhatsAppConversation(t *testing.T) {
	templates := &conversationTemplatesStub{}

	if _, err := wiredTemplateSender(templates).SendTemplate("entry-1", "instagram", "t1", nil, "u1", "ws1"); !errors.Is(err, conversation.ErrEntryTypeInvalid) || len(templates.sends) != 0 {
		t.Fatalf("err %v sends %d", err, len(templates.sends))
	}
}

func TestATemplateSendRefusalReachesTheCaller(t *testing.T) {
	templates := &conversationTemplatesStub{err: wo.ErrTemplateForbidden}

	if _, err := wiredTemplateSender(templates).SendTemplate("entry-1", "whatsapp", "t1", nil, "u1", "ws1"); !errors.Is(err, wo.ErrTemplateForbidden) {
		t.Fatalf("err %v", err)
	}
}
