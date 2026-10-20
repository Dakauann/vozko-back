package template_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/whatsapp/template"
	wf "vozko/domain/whatsappform"
	whatsappform_usecase "vozko/usecases/whatsappform"
)

type FormSends interface {
	PrepareTemplateSend(ctx context.Context, in whatsappform_usecase.TemplateSend) (wf.Send, error)
	SettleTemplateSend(ctx context.Context, token, messageID string, sendErr error) error
}

type FlowPublications interface {
	FindByMetaFlow(ctx context.Context, metaWABAID, metaFlowID string) (*wf.Publication, error)
}

type FormTicket struct {
	sends FormSends
	token string
}

func PrepareFormSend(ctx context.Context, sends FormSends, tmpl *template.Template, in whatsappform_usecase.TemplateSend) (FormTicket, error) {
	button, _, ok := tmpl.FormButton()
	if !ok {
		return FormTicket{}, nil
	}
	if sends == nil {
		return FormTicket{}, template.ErrFormNotSendable
	}
	in.MetaFlowID, in.TemplateID = button.FlowID, tmpl.ID
	send, err := sends.PrepareTemplateSend(ctx, in)
	if err != nil {
		if wf.ErrorCode(err) != "" {
			return FormTicket{}, fmt.Errorf("%w: %w", template.ErrFormNotSendable, err)
		}
		return FormTicket{}, fmt.Errorf("prepare the form send: %w", err)
	}
	if strings.TrimSpace(send.Token) == "" {
		return FormTicket{}, fmt.Errorf("%w: %w", template.ErrFormNotSendable, wf.ErrTokenRequired)
	}
	return FormTicket{sends: sends, token: send.Token}, nil
}

func (t FormTicket) Token() string {
	return t.token
}

func (t FormTicket) Settle(ctx context.Context, messageID string, sendErr error) {
	if t.token == "" {
		return
	}
	if sendErr == nil && strings.TrimSpace(messageID) == "" {
		sendErr = conversation.ErrSendOutcomeUnknown
	}
	if err := t.sends.SettleTemplateSend(ctx, t.token, messageID, sendErr); err != nil {
		log.Printf("[template-form-send] could not settle the form send of message %q: %v", messageID, err)
	}
}

func CheckFormPublished(ctx context.Context, publications FlowPublications, workspaceID, metaWABAID string, tmpl *template.Template) error {
	button, _, ok := tmpl.FormButton()
	if !ok {
		return nil
	}
	if publications == nil || strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(metaWABAID) == "" {
		return template.ErrFlowNotPublished
	}
	pub, err := publications.FindByMetaFlow(ctx, metaWABAID, button.FlowID)
	if errors.Is(err, wf.ErrPublicationNotFound) {
		return template.ErrFlowNotPublished
	}
	if err != nil {
		return fmt.Errorf("read the form publication of flow %s: %w", button.FlowID, err)
	}
	if pub == nil || pub.WorkspaceID != workspaceID || !pub.Sendable() {
		return template.ErrFlowNotPublished
	}
	return nil
}
