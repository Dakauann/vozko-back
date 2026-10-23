package carriercampaign_usecase

import (
	"context"
	"errors"

	cc "vozko/domain/carriercampaign"
	"vozko/domain/carriermessaging"
	"vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/domain/shared"
	carriermessaging_usecase "vozko/usecases/carriermessaging"
)

func (s *Service) TestSend(ctx context.Context, workspaceID, id string, a Actor, phone string, variant int) (*carriermessaging.Message, error) {
	c, err := s.deps.Campaigns.FindByID(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	if variant < 0 || variant >= len(c.Variants) {
		return nil, ErrVariantIndex
	}
	normalized := shared.NormalizePhone(phone)
	if normalized == "" {
		return nil, carriermessaging.ErrPhoneInvalid
	}
	if err := s.validateForSend(*c); err != nil {
		return nil, err
	}
	contact, err := s.deps.Leads.FindByNumber(workspaceID, normalized)
	if errors.Is(err, lead.ErrLeadNotFound) {
		contact, err = nil, nil
	}
	if err != nil {
		return nil, err
	}
	vars, values, err := s.message(ctx, *c, variant, contact)
	if err != nil {
		return nil, err
	}
	fallback, err := c.Fallback.Spec(c.CreatedBy, values)
	if err != nil {
		return nil, err
	}
	v := c.Variants[variant]
	return s.deps.Carrier.Deliver(ctx, carriermessaging_usecase.DeliverRequest{
		WorkspaceID: workspaceID, Channel: c.Channel, SenderID: c.SenderID, Phone: normalized, Lead: contact,
		RunKind: carriermessaging.RunTest, Variant: variant, Content: v.Content, Text: v.Text, Vars: vars,
		Fallback: fallback, SentBy: conversation.SentByPerson(a.UserID),
	})
}

func (s *Service) validateForSend(c cc.Campaign) error {
	launched := c
	launched.Status = cc.StatusRunning
	return s.validate(launched, s.now())
}
