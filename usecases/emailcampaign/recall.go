package emailcampaign_usecase

import (
	"context"
	"errors"

	"vozko/domain/conversation"
	"vozko/domain/emailcampaign"
	"vozko/domain/emaildesign"
	em "vozko/domain/emailinbox"
	"vozko/domain/emailsending"
)

var recallGone = append([]error{
	emailsending.ErrMessageNotFound, emailsending.ErrSenderNotFound, emailsending.ErrNoDefaultSender, emailsending.ErrDomainNotFound,
}, renderErrors...)

func (s *Service) Original(ctx context.Context, sent emailsending.Message) (em.SentOriginal, bool, error) {
	if sent.RunKind != emailsending.RunCampaign && sent.RunKind != emailsending.RunTest {
		return em.SentOriginal{}, false, nil
	}
	c, err := s.deps.Campaigns.GetByID(ctx, sent.RunID)
	if errors.Is(err, emailcampaign.ErrCampaignNotFound) {
		return em.SentOriginal{}, false, nil
	}
	if err != nil {
		return em.SentOriginal{}, false, err
	}
	if c.WorkspaceID != sent.WorkspaceID {
		return em.SentOriginal{}, false, nil
	}
	subject, p, found, err := s.sentMerge(ctx, *c, sent)
	if err != nil || !found {
		return em.SentOriginal{}, false, err
	}
	content, err := s.viewerContent(ctx, *c, sent, subject, p)
	if anyOf(err, recallGone) {
		return em.SentOriginal{}, false, nil
	}
	if err != nil {
		return em.SentOriginal{}, false, err
	}
	return em.SentOriginal{
		Subject: content.Subject, Text: content.Text, SentBy: conversation.SentByCampaign(c.ID), CampaignID: c.ID, CampaignName: c.Name,
		Merge: &em.CampaignMerge{Subject: subject, Personalization: p},
	}, true, nil
}

func (s *Service) sentMerge(ctx context.Context, c emailcampaign.Campaign, sent emailsending.Message) (string, emaildesign.Personalization, bool, error) {
	if sent.RunKind == emailsending.RunTest {
		subject, p := s.testMerge(c, sent.Address)
		return subject, p, true, nil
	}
	rc, err := s.deps.Recipients.Get(ctx, sent.RecipientID)
	if errors.Is(err, emailcampaign.ErrRecipientNotFound) {
		return "", emaildesign.Personalization{}, false, nil
	}
	if err != nil {
		return "", emaildesign.Personalization{}, false, err
	}
	if rc.CampaignID != c.ID {
		return "", emaildesign.Personalization{}, false, nil
	}
	facts, err := s.factsOf(ctx, c.WorkspaceID, rc.LeadID)
	if err != nil {
		return "", emaildesign.Personalization{}, false, err
	}
	subject, p := recipientMerge(c, *rc, facts)
	return subject, p, true, nil
}

func (s *Service) viewerContent(ctx context.Context, c emailcampaign.Campaign, sent emailsending.Message, subject string, p emaildesign.Personalization) (emailsending.Content, error) {
	if _, err := s.envelopeOf(ctx, c, sent); err != nil {
		return emailsending.Content{}, err
	}
	return campaignContent(c, subject, p, emaildesign.RenderInput{InertUnsubscribe: true})
}

func (s *Service) envelopeOf(ctx context.Context, c emailcampaign.Campaign, sent emailsending.Message) (emailsending.Envelope, error) {
	return s.deps.Gateway.Envelope(ctx, emailsending.SendRequest{
		WorkspaceID: c.WorkspaceID, SenderID: c.SenderID, To: sent.Address, Kind: emailsending.KindMarketing, RunKind: sent.RunKind, RunID: c.ID,
	}, sent)
}

func (s *Service) OriginalHTML(ctx context.Context, sent emailsending.Message, merge em.CampaignMerge) (string, error) {
	if sent.RunKind != emailsending.RunCampaign && sent.RunKind != emailsending.RunTest {
		return "", emailcampaign.ErrCampaignNotFound
	}
	c, err := s.deps.Campaigns.GetByID(ctx, sent.RunID)
	if err != nil {
		return "", err
	}
	if c.WorkspaceID != sent.WorkspaceID {
		return "", emailcampaign.ErrCampaignNotFound
	}
	content, err := s.viewerContent(ctx, *c, sent, merge.Subject, merge.Personalization)
	if err != nil {
		return "", err
	}
	return content.HTML, nil
}
