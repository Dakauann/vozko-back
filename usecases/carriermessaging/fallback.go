package carriermessaging_usecase

import (
	"context"
	"errors"
	"log"

	"vozko/domain/carriermessaging"
	"vozko/domain/conversation"
)

func (s *Service) runFallback(ctx context.Context, m carriermessaging.Message) {
	next, ok := m.Fallback.Plan().Next(m.Step, m.Status)
	if !ok {
		return
	}
	switch next {
	case carriermessaging.StepWhatsApp:
		s.fallbackToWhatsApp(ctx, m, *m.Fallback.WhatsApp)
	case carriermessaging.StepSMS:
		s.fallbackToSMS(ctx, m)
	}
}

func (s *Service) fallbackToWhatsApp(ctx context.Context, parent carriermessaging.Message, spec carriermessaging.WhatsAppFallback) {
	childID := s.NewID()
	reference, err := carriermessaging.ReferenceFor(childID)
	if err != nil {
		log.Printf("CRITICAL: [carrier] fallback id %q of %s: %v", childID, parent.ID, err)
		return
	}
	linked, err := s.Repos.Messages.LinkFallback(ctx, parent.ID, childID)
	if err != nil || !linked {
		if err != nil {
			log.Printf("[carrier] fallback link %s: %v", parent.ID, err)
		}
		return
	}
	child := carriermessaging.Message{
		ID: childID, WorkspaceID: parent.WorkspaceID, Channel: parent.Channel, Reference: reference,
		SenderID: parent.SenderID, Phone: parent.Phone, LeadID: parent.LeadID, ConversationID: parent.ConversationID,
		RunKind: parent.RunKind, Marketing: parent.Marketing, RunID: parent.RunID, Variant: parent.Variant,
		Step: carriermessaging.StepWhatsApp, Status: carriermessaging.StatusPending, ParentMessageID: parent.ID,
		Fallback:  carriermessaging.FallbackSpec{SMSRouteID: parent.Fallback.SMSRouteID, SMSText: parent.Fallback.SMSText},
		CreatedAt: s.Now(),
	}
	if err := s.Repos.Messages.Create(ctx, &child); err != nil {
		log.Printf("[carrier] whatsapp fallback row %s: %v", child.ID, err)
		return
	}
	_, err = s.WhatsApp.StartTemplate(ctx, WhatsAppStart{
		WorkspaceID: child.WorkspaceID, UserID: spec.ActorID, BusinessPhoneID: spec.BusinessPhoneID, TemplateID: spec.TemplateID,
		Phone: child.Phone, BodyParams: spec.BodyParams, HeaderParams: spec.HeaderParams, IdempotencyKey: child.ID,
	})
	if err != nil {
		log.Printf("[carrier] whatsapp fallback %s refused: %v", child.ID, err)
		if _, err := s.fail(ctx, child, carriermessaging.StatusFailed, ErrorCodeWhatsAppRefused); err != nil {
			log.Printf("[carrier] whatsapp fallback %s status: %v", child.ID, err)
		}
		return
	}
	if _, err := s.advance(ctx, child.Reference, carriermessaging.StatusAccepted, "", s.Now()); err != nil {
		log.Printf("[carrier] whatsapp fallback %s status: %v", child.ID, err)
	}
}

func (s *Service) fallbackToSMS(ctx context.Context, parent carriermessaging.Message) {
	_, err := s.deliver(ctx, DeliverRequest{
		WorkspaceID: parent.WorkspaceID, Channel: carriermessaging.ChannelSMS, SenderID: parent.Fallback.SMSRouteID,
		Phone: parent.Phone, RunKind: parent.RunKind, Marketing: parent.Marketing, RunID: parent.RunID,
		Variant: parent.Variant, Text: parent.Fallback.SMSText, SentBy: conversation.SentBySystem(),
	}, &parent)
	if err != nil && !errors.Is(err, errAlreadyFellBack) {
		log.Printf("[carrier] sms fallback of %s: %v", parent.ID, err)
	}
}
