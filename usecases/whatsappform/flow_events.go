package whatsappform_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"

	"vozko/domain/notification"
	wf "vozko/domain/whatsappform"
	"vozko/domain/workspace"
)

const (
	formsPath             = "/dashboard/whatsapp-forms/"
	episodePrefix         = "whatsapp_form:"
	episodeLimited        = "limited"
	episodeBlocked        = "blocked"
	formNoticeSource      = "whatsapp_form"
	formNoticeActionLabel = "Ver formulário"
)

type formNotice struct {
	state    string
	severity notification.Severity
	title    string
	body     string
}

var statusNotices = map[wf.PublicationStatus]formNotice{
	wf.PublicationThrottled: {episodeLimited, notification.SeverityWarning, "Formulário limitado pela Meta",
		"A Meta passou a entregar no máximo 10 envios por hora deste formulário até a qualidade melhorar."},
	wf.PublicationBlocked: {episodeBlocked, notification.SeverityCritical, "Formulário bloqueado pela Meta",
		"A Meta parou de entregar este formulário. Os envios falham até a Meta liberar de novo, o que ela faz sozinha quando os erros caem."},
}

var alertNotice = formNotice{severity: notification.SeverityWarning, title: "Formulário com erros na Meta",
	body: "A Meta registrou erros acima do normal quando as pessoas abrem este formulário."}

func episodeKey(state, publicationID string) string {
	return episodePrefix + state + ":" + publicationID
}

func (s *Service) HandleFlowWebhook(ctx context.Context, body []byte) error {
	events, err := wf.ParseFlowWebhook(body)
	if err != nil {
		return err
	}
	var failures []error
	for _, event := range events {
		if err := s.handleFlowEvent(ctx, event); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (s *Service) handleFlowEvent(ctx context.Context, event wf.FlowEvent) error {
	pub, err := s.deps.Publications.FindByMetaFlow(ctx, event.MetaWABAID, event.MetaFlowID)
	if errors.Is(err, wf.ErrPublicationNotFound) {
		log.Printf("[whatsapp-forms] %s for flow %s that Vozko does not publish", event.Event, event.MetaFlowID)
		return nil
	}
	if err != nil {
		return err
	}
	form, err := s.deps.Forms.Get(ctx, pub.WorkspaceID, pub.FormID)
	if err != nil {
		return err
	}
	switch {
	case event.Event == wf.FlowStatusChange:
		return s.applyStatus(ctx, pub, form, event)
	case event.IsAlert() && event.Activated():
		return s.raise(ctx, pub, form, event.Event, alertNotice, event.Message)
	case event.IsAlert():
		return s.deps.Notices.Resolve(ctx, pub.WorkspaceID, episodeKey(event.Event, pub.ID))
	}
	return nil
}

func (s *Service) applyStatus(ctx context.Context, pub *wf.Publication, form *wf.Form, event wf.FlowEvent) error {
	next := wf.PublicationStatusFromMeta(event.NewStatus)
	if next == "" || !pub.Status.CanMoveTo(next) {
		log.Printf("[whatsapp-forms] publication %s ignores Meta status %q from %s", pub.ID, event.NewStatus, pub.Status)
		return nil
	}
	pub.Status = next
	if err := s.save(ctx, pub); err != nil {
		return err
	}
	if notice, ok := statusNotices[next]; ok {
		return s.raise(ctx, pub, form, notice.state, notice, event.Message)
	}
	return s.deps.Notices.Resolve(ctx, pub.WorkspaceID, episodeKey(episodeLimited, pub.ID), episodeKey(episodeBlocked, pub.ID))
}

func (s *Service) raise(ctx context.Context, pub *wf.Publication, form *wf.Form, state string, notice formNotice, metaMessage string) error {
	body := fmt.Sprintf("Formulário %q. %s", form.Name, notice.body)
	if metaMessage != "" {
		body += " Mensagem da Meta: " + metaMessage
	}
	_, err := s.deps.Notices.Publish(ctx, notification.Draft{
		WorkspaceID: pub.WorkspaceID, Category: notification.CategoryTemplates, Severity: notice.severity,
		Title: notification.ClipTitle(notice.title), Body: body, ActionURL: formsPath + form.ID, ActionLabel: formNoticeActionLabel,
		Audience: notification.Holding(workspace.ResourceWhatsAppForms, workspace.ActionConfigure), Email: true,
		EpisodeKey: episodeKey(state, pub.ID), Source: notification.SourceWhatsAppForm,
	})
	return err
}
