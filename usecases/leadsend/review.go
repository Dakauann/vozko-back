package leadsend_usecase

import (
	"context"
	"errors"
	"fmt"

	"vozko/domain/campaign"
	"vozko/domain/leadaction"
	"vozko/domain/metrics"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
	wd "vozko/domain/workspace/workspace_department"
)

type SendRequest struct {
	Actor       Actor
	Departments *wd.DepartmentFilter
	Channel     campaign.Channel
	CampaignIDs []string
	FirstN      int
}

type pricing struct {
	templateID      string
	businessPhoneID string
	instanceID      string
	dailyCap        int
	scope           uw.DepartmentScope
}

type ownedPart struct {
	part campaign.SendPart
	key  string
	pricing
}

func (s *Service) Review(ctx context.Context, req SendRequest) (*campaign.SendReview, error) {
	if _, err := s.authorize(req.Actor, req.Channel); err != nil {
		return nil, err
	}
	review, _, err := s.review(ctx, req.Actor, req.Departments, req.Channel, req.CampaignIDs)
	return review, err
}

func distinctIDs(ids []string) ([]string, error) {
	out := shared.DistinctTrimmed(ids)
	if len(out) == 0 || len(out) > campaign.MaxSelectionParts {
		return nil, campaign.ErrSendIncomplete
	}
	return out, nil
}

func (s *Service) owned(ctx context.Context, a Actor, departments *wd.DepartmentFilter, channel campaign.Channel, id string) (ownedPart, error) {
	if channel == campaign.ChannelOfficial {
		c, err := s.deps.Official.Access.Owned(a.WorkspaceID, departments, id)
		if err != nil {
			return ownedPart{}, err
		}
		if c.Source != campaign.SourceLeadSelection {
			return ownedPart{}, campaign.ErrNotFromSelection
		}
		return ownedPart{
			part:    campaign.SendPart{CampaignID: c.ID, Name: c.Name, Status: c.Status},
			key:     c.IdempotencyKey,
			pricing: pricing{templateID: c.TemplateID, businessPhoneID: c.BusinessPhoneID},
		}, nil
	}
	scope, err := s.unofficialScope(a)
	if err != nil {
		return ownedPart{}, err
	}
	c, err := s.deps.Unofficial.Access.Owned(ctx, a.WorkspaceID, scope, id)
	if err != nil {
		return ownedPart{}, err
	}
	if c.Source != campaign.SourceLeadSelection {
		return ownedPart{}, campaign.ErrNotFromSelection
	}
	return ownedPart{
		part:    campaign.SendPart{CampaignID: c.ID, Name: c.Name, Status: c.Status},
		key:     c.IdempotencyKey,
		pricing: pricing{instanceID: c.InstanceID, dailyCap: c.DailyCap, scope: scope},
	}, nil
}

func oneSend(parts []ownedPart) error {
	refs := make([]campaign.SendPartRef, 0, len(parts))
	for _, p := range parts {
		refs = append(refs, campaign.SendPartRef{
			CampaignID: p.part.CampaignID, Key: p.key, TemplateID: p.pricing.templateID, BusinessPhoneID: p.pricing.businessPhoneID, InstanceID: p.pricing.instanceID,
		})
	}
	return campaign.OneSend(refs)
}

func (s *Service) review(ctx context.Context, a Actor, departments *wd.DepartmentFilter, channel campaign.Channel, campaignIDs []string) (*campaign.SendReview, []ownedPart, error) {
	if !channel.Valid() {
		return nil, nil, fmt.Errorf("%w: channel %q", leadaction.ErrUnknownAction, channel)
	}
	ids, err := distinctIDs(campaignIDs)
	if err != nil {
		return nil, nil, err
	}
	parts := make([]ownedPart, 0, len(ids))
	for _, id := range ids {
		p, err := s.owned(ctx, a, departments, channel, id)
		if err != nil {
			return nil, nil, err
		}
		parts = append(parts, p)
	}
	if err := oneSend(parts); err != nil {
		return nil, nil, err
	}
	sendParts := make([]campaign.SendPart, 0, len(parts))
	for _, p := range parts {
		sendParts = append(sendParts, p.part)
	}
	var tallies []campaign.PartTally
	err = s.gated(ctx, func(ctx context.Context) error {
		var err error
		tallies, err = s.deps.Store.Tally(ctx, channel, a.WorkspaceID, ids, parts[0].pricing.businessPhoneID, s.now())
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	review := campaign.NewReview(channel, sendParts, tallies)
	review.Quote = campaign.SendQuote{Count: review.Eligible, Parts: len(parts), MaxPerCampaign: campaign.MaxEntries}
	if err := s.price(ctx, a, channel, &review.Quote, parts[0].pricing); err != nil {
		return nil, nil, err
	}
	return &review, parts, nil
}

func (s *Service) price(ctx context.Context, a Actor, channel campaign.Channel, q *campaign.SendQuote, p pricing) error {
	if channel == campaign.ChannelOfficial {
		tmpl, err := s.officialTemplate(a.WorkspaceID, p.templateID)
		if err != nil {
			return err
		}
		return s.priceOfficial(q, a.WorkspaceID, tmpl)
	}
	instance, err := s.deps.Unofficial.Instances.Usable(ctx, a.WorkspaceID, p.scope, p.instanceID)
	if err != nil {
		return err
	}
	s.priceUnofficial(q, instance, p.dailyCap)
	return nil
}

func (s *Service) Start(ctx context.Context, req SendRequest) (*campaign.SendReview, error) {
	began := s.now()
	action, err := s.authorize(req.Actor, req.Channel)
	if err != nil {
		return nil, err
	}
	review, parts, err := s.review(ctx, req.Actor, req.Departments, req.Channel, req.CampaignIDs)
	if err != nil {
		return nil, err
	}
	review, parts, err = s.dropLeadsAlreadySending(ctx, req, action, review, parts)
	if err != nil {
		return nil, err
	}
	stopped := stoppedParts(review, parts)
	if len(stopped) == 0 {
		if review.Started {
			return review, nil
		}
		return nil, campaign.ErrNothingEligible
	}
	eligible := make([]int, 0, len(stopped))
	total := 0
	for _, p := range stopped {
		eligible = append(eligible, p.Eligible)
		total += p.Eligible
	}
	allowed, err := review.Budget(total).Allow(req.FirstN)
	if err != nil {
		return nil, err
	}
	kept := campaign.KeepFirst(eligible, allowed)
	for i, p := range stopped {
		if kept[i] < p.Eligible {
			skipped, err := s.deps.Store.SkipBeyond(ctx, req.Channel, req.Actor.WorkspaceID, p.CampaignID, kept[i])
			if err != nil {
				return nil, err
			}
			s.deps.Metrics.AddLeadActionSkips(string(action), string(campaign.SkipOverCap), int(skipped))
		}
	}
	dispatched := false
	for i, p := range stopped {
		if kept[i] == 0 {
			continue
		}
		started, err := s.dispatch(ctx, req, parts, p.CampaignID)
		if err != nil {
			return nil, err
		}
		dispatched = dispatched || started
	}
	if dispatched {
		s.countSend(action, metrics.LeadActionStarted, began)
		RunLog(campaign.BaseOfKey(parts[0].key), req.Actor.WorkspaceID, action).Info("lead send: started", "actor_id", req.Actor.UserID,
			"campaign_ids", req.CampaignIDs, "eligible", total, "allowed", allowed)
	}
	review, _, err = s.review(ctx, req.Actor, req.Departments, req.Channel, req.CampaignIDs)
	return review, err
}

func (s *Service) dropLeadsAlreadySending(ctx context.Context, req SendRequest, action leadaction.Action, review *campaign.SendReview, parts []ownedPart) (*campaign.SendReview, []ownedPart, error) {
	marked := int64(0)
	for _, p := range stoppedParts(review, parts) {
		n, err := s.deps.Store.SkipInRunning(ctx, req.Channel, req.Actor.WorkspaceID, p.CampaignID)
		if err != nil {
			return nil, nil, err
		}
		s.deps.Metrics.AddLeadActionSkips(string(action), string(campaign.SkipAlreadyInRunningCampaign), int(n))
		marked += n
	}
	if marked == 0 {
		return review, parts, nil
	}
	return s.review(ctx, req.Actor, req.Departments, req.Channel, req.CampaignIDs)
}

func stoppedParts(review *campaign.SendReview, parts []ownedPart) []campaign.SendPart {
	var stopped []campaign.SendPart
	for i, p := range review.Parts {
		if parts[i].part.Status == campaign.StatusStopped && p.Eligible > 0 {
			stopped = append(stopped, p)
		}
	}
	return stopped
}

func (s *Service) dispatch(ctx context.Context, req SendRequest, parts []ownedPart, campaignID string) (bool, error) {
	for _, p := range parts {
		if p.part.CampaignID != campaignID {
			continue
		}
		var err error
		if req.Channel == campaign.ChannelOfficial {
			_, err = s.deps.Official.Start.StartReviewed(req.Actor.WorkspaceID, req.Departments, campaignID)
		} else {
			_, err = s.deps.Unofficial.Start.StartReviewed(ctx, req.Actor.WorkspaceID, p.pricing.scope, campaignID)
		}
		if errors.Is(err, campaign.ErrAlreadyRunning) {
			return false, nil
		}
		return err == nil, err
	}
	return false, campaign.ErrSendIncomplete
}

func (s *Service) Cancel(ctx context.Context, req SendRequest) error {
	if _, err := s.authorize(req.Actor, req.Channel); err != nil {
		return err
	}
	review, parts, err := s.review(ctx, req.Actor, req.Departments, req.Channel, req.CampaignIDs)
	if err != nil {
		return err
	}
	if review.Started {
		return campaign.ErrAlreadyStarted
	}
	for _, p := range parts {
		deleted, err := s.deps.Store.DeleteStopped(ctx, req.Channel, req.Actor.WorkspaceID, p.part.CampaignID)
		if err != nil {
			return err
		}
		if !deleted {
			return campaign.ErrAlreadyStarted
		}
		if req.Channel == campaign.ChannelOfficial {
			s.deps.Official.Cache.Forget(p.part.CampaignID)
		}
	}
	return nil
}
