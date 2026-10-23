package carriermessaging_usecase

import (
	"context"
	"errors"
	"testing"

	wo "vozko/domain/whatsapp_outreach"
)

type fakeOfficial struct {
	in     wo.StartConversationInput
	result *wo.StartedConversation
	err    error
}

func (f *fakeOfficial) Execute(_ context.Context, in wo.StartConversationInput) (*wo.StartedConversation, error) {
	f.in = in
	return f.result, f.err
}

type fakeConversationTemplates struct {
	in  wo.ConversationTemplateInput
	err error
}

func (f *fakeConversationTemplates) Send(_ context.Context, in wo.ConversationTemplateInput) (*wo.SentConversationTemplate, error) {
	f.in = in
	if f.err != nil {
		return nil, f.err
	}
	return &wo.SentConversationTemplate{MessageID: "wamid-2"}, nil
}

func start() WhatsAppStart {
	return WhatsAppStart{WorkspaceID: "ws-1", UserID: "user-1", BusinessPhoneID: "bp-1", TemplateID: "tpl-1", Phone: "5511999990000", BodyParams: []string{"Ana"}, IdempotencyKey: "child-1"}
}

func TestWhatsAppFallbackStartsAColdConversation(t *testing.T) {
	official := &fakeOfficial{result: &wo.StartedConversation{MessageID: "wamid-1"}}
	id, err := NewWhatsAppStarter(official, &fakeConversationTemplates{}).StartTemplate(context.Background(), start())
	if err != nil || id != "wamid-1" {
		t.Fatalf("StartTemplate = %q, %v", id, err)
	}
	if official.in.UserID != "user-1" || official.in.IsAdmin || official.in.PhoneNumber != "5511999990000" || official.in.IdempotencyKey != "child-1" {
		t.Fatalf("input = %+v, the fallback acts as the person who configured it", official.in)
	}
}

func TestWhatsAppFallbackSendsInsideAnOpenWindow(t *testing.T) {
	official := &fakeOfficial{result: &wo.StartedConversation{EntryID: "entry-1"}, err: wo.ErrWindowAlreadyOpen}
	templates := &fakeConversationTemplates{}
	id, err := NewWhatsAppStarter(official, templates).StartTemplate(context.Background(), start())
	if err != nil || id != "wamid-2" || templates.in.EntryID != "entry-1" || templates.in.TemplateID != "tpl-1" || templates.in.OriginRef != "child-1" {
		t.Fatalf("StartTemplate = %q, %v, in = %+v", id, err, templates.in)
	}
}

func TestWhatsAppFallbackPassesRefusalsOn(t *testing.T) {
	official := &fakeOfficial{err: wo.ErrWithinSpamWindow}
	if _, err := NewWhatsAppStarter(official, &fakeConversationTemplates{}).StartTemplate(context.Background(), start()); !errors.Is(err, wo.ErrWithinSpamWindow) {
		t.Fatalf("StartTemplate = %v", err)
	}
	official = &fakeOfficial{result: &wo.StartedConversation{}, err: wo.ErrWindowAlreadyOpen}
	if _, err := NewWhatsAppStarter(official, &fakeConversationTemplates{}).StartTemplate(context.Background(), start()); err == nil {
		t.Fatal("an open window without its conversation cannot be used")
	}
}
