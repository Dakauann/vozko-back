package carriermessaging_usecase

import (
	"context"
	"errors"

	wo "vozko/domain/whatsapp_outreach"
	"vozko/domain/whatsappform"
)

type OfficialStarter interface {
	Execute(ctx context.Context, in wo.StartConversationInput) (*wo.StartedConversation, error)
}

type ConversationTemplateSender interface {
	Send(ctx context.Context, in wo.ConversationTemplateInput) (*wo.SentConversationTemplate, error)
}

type whatsappStarter struct {
	official  OfficialStarter
	templates ConversationTemplateSender
}

func NewWhatsAppStarter(official OfficialStarter, templates ConversationTemplateSender) WhatsAppStarter {
	return &whatsappStarter{official: official, templates: templates}
}

func (w *whatsappStarter) StartTemplate(ctx context.Context, in WhatsAppStart) (string, error) {
	started, err := w.official.Execute(ctx, wo.StartConversationInput{
		WorkspaceID: in.WorkspaceID, UserID: in.UserID, BusinessPhoneID: in.BusinessPhoneID, TemplateID: in.TemplateID,
		PhoneNumber: in.Phone, Name: in.Name, BodyParams: in.BodyParams, HeaderParams: in.HeaderParams,
		IdempotencyKey: in.IdempotencyKey,
	})
	if err == nil {
		return started.MessageID, nil
	}
	if !errors.Is(err, wo.ErrWindowAlreadyOpen) {
		return "", err
	}
	if started == nil || started.EntryID == "" {
		return "", err
	}
	sent, err := w.templates.Send(ctx, wo.ConversationTemplateInput{
		WorkspaceID: in.WorkspaceID, UserID: in.UserID, EntryID: started.EntryID, TemplateID: in.TemplateID,
		BodyParams: in.BodyParams, HeaderParams: in.HeaderParams, IdempotencyKey: in.IdempotencyKey,
		Origin: whatsappform.OriginTemplate, OriginRef: in.IdempotencyKey,
	})
	if err != nil {
		return "", err
	}
	return sent.MessageID, nil
}
