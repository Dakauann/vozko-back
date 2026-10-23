package emailcampaign_usecase

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"vozko/domain/campaign"
	audience "vozko/domain/campaignaudience"
	"vozko/domain/emailcampaign"
)

func (s *Service) Create(ctx context.Context, a Actor, d emailcampaign.Draft) (*emailcampaign.Campaign, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	c := &emailcampaign.Campaign{WorkspaceID: a.WorkspaceID, Status: emailcampaign.StatusDraft, CreatedBy: a.UserID}
	c.Apply(d)
	if c.ListID != "" {
		c.ID = uuid.NewString()
		if err := s.attachList(ctx, a, *c); err != nil {
			return nil, err
		}
	}
	if err := s.deps.Campaigns.Create(ctx, c); err != nil {
		s.releaseList(ctx, *c, c.ListID)
		return nil, err
	}
	return c, nil
}

func (s *Service) attachList(ctx context.Context, a Actor, c emailcampaign.Campaign) error {
	if s.audience == nil {
		return audience.ErrUnavailable
	}
	return s.audience.Attach(ctx, a, c.ListID, c.AudienceOwner())
}

func (s *Service) releaseList(ctx context.Context, c emailcampaign.Campaign, listID string) {
	if listID == "" || s.audience == nil {
		return
	}
	if err := s.audience.Release(context.WithoutCancel(ctx), c.WorkspaceID, c.AudienceOwner(), listID); err != nil {
		s.logf("could not release list %s of %s: %v", listID, c.ID, err)
	}
}

func (s *Service) Get(ctx context.Context, a Actor, campaignID string) (*emailcampaign.Campaign, error) {
	return s.deps.Campaigns.Get(ctx, a.WorkspaceID, campaignID)
}

type ListItem struct {
	Campaign emailcampaign.Campaign
	Metrics  emailcampaign.ListMetrics
	Actions  []emailcampaign.Command
}

func (s *Service) List(ctx context.Context, a Actor, q emailcampaign.ListQuery) ([]ListItem, int64, error) {
	if q.Status != "" && !q.Status.Valid() {
		return nil, 0, emailcampaign.ErrStatusInvalid
	}
	campaigns, total, err := s.deps.Campaigns.List(ctx, a.WorkspaceID, q)
	if err != nil {
		return nil, 0, err
	}
	ids := make([]string, 0, len(campaigns))
	for _, c := range campaigns {
		ids = append(ids, c.ID)
	}
	tallies, err := s.deps.Metrics.Summaries(ctx, a.WorkspaceID, ids)
	if err != nil {
		return nil, 0, err
	}
	items := make([]ListItem, 0, len(campaigns))
	for _, c := range campaigns {
		items = append(items, ListItem{Campaign: c, Metrics: emailcampaign.NewListMetrics(tallies[c.ID]), Actions: c.Actions()})
	}
	return items, total, nil
}

func (s *Service) Update(ctx context.Context, a Actor, campaignID string, d emailcampaign.Draft) (*emailcampaign.Campaign, error) {
	c, err := s.deps.Campaigns.Get(ctx, a.WorkspaceID, campaignID)
	if err != nil {
		return nil, err
	}
	if !c.Status.Editable() {
		return nil, emailcampaign.ErrNotEditable
	}
	if err := d.Validate(); err != nil {
		return nil, err
	}
	previous, from := c.ListID, c.Status
	c.Apply(d)
	c.Status = from.AfterEdit()
	attached := c.ListID != "" && c.ListID != previous
	if attached {
		if err := s.attachList(ctx, a, *c); err != nil {
			return nil, err
		}
	}
	if err := s.deps.Campaigns.Update(ctx, c, from); err != nil {
		if attached {
			s.releaseList(ctx, *c, c.ListID)
		}
		return nil, err
	}
	if previous != "" && previous != c.ListID {
		s.releaseList(ctx, *c, previous)
	}
	return s.deps.Campaigns.Get(ctx, a.WorkspaceID, campaignID)
}

func (s *Service) ReviseContent(ctx context.Context, a Actor, campaignID string, content emailcampaign.Content) (*emailcampaign.Campaign, error) {
	c, err := s.deps.Campaigns.Get(ctx, a.WorkspaceID, campaignID)
	if err != nil {
		return nil, err
	}
	if !c.Status.ContentRevisable() {
		return nil, emailcampaign.ErrNotRevisable
	}
	from := c.Status
	if err := c.ReviseContent(content); err != nil {
		return nil, err
	}
	c.Status = from.AfterEdit()
	if err := s.deps.Campaigns.Update(ctx, c, from); err != nil {
		return nil, err
	}
	return s.deps.Campaigns.Get(ctx, a.WorkspaceID, campaignID)
}

func (s *Service) Delete(ctx context.Context, a Actor, campaignID string) error {
	c, err := s.deps.Campaigns.Get(ctx, a.WorkspaceID, campaignID)
	if err != nil {
		return err
	}
	if err := s.deps.Campaigns.Delete(ctx, a.WorkspaceID, campaignID); err != nil {
		return err
	}
	s.releaseList(ctx, *c, c.ListID)
	return nil
}

func (s *Service) Pause(ctx context.Context, a Actor, campaignID string) (*emailcampaign.Campaign, error) {
	c, err := s.deps.Campaigns.Get(ctx, a.WorkspaceID, campaignID)
	if err != nil {
		return nil, err
	}
	if err := c.Resumable(); err != nil {
		return nil, err
	}
	to, err := emailcampaign.Transition(c.Status, emailcampaign.ActionPause)
	if err != nil {
		return nil, err
	}
	if err := s.swap(ctx, *c, to, campaign.ActionPause); err != nil {
		return nil, err
	}
	if err := s.runner.PauseCampaignConsumer(c.ID); err != nil {
		s.revertSwap(ctx, c.ID, to, c.Status, c.HoldReason)
		return nil, fmt.Errorf("could not pause the consumer: %w", err)
	}
	return s.deps.Campaigns.Get(ctx, a.WorkspaceID, campaignID)
}

func (s *Service) Resume(ctx context.Context, a Actor, campaignID string) (*emailcampaign.Campaign, error) {
	c, err := s.deps.Campaigns.Get(ctx, a.WorkspaceID, campaignID)
	if err != nil {
		return nil, err
	}
	if err := c.Resumable(); err != nil {
		return nil, err
	}
	if !c.Status.Launched() {
		return nil, emailcampaign.ErrNotLaunched
	}
	to, err := emailcampaign.Transition(c.Status, emailcampaign.ActionStart)
	if err != nil {
		return nil, err
	}
	if err := s.swap(ctx, *c, to, campaign.ActionStart); err != nil {
		return nil, err
	}
	if err := s.runner.ResumeCampaignConsumer(c.ID); err != nil {
		s.revertSwap(ctx, c.ID, to, c.Status, c.HoldReason)
		return nil, fmt.Errorf("could not resume the consumer: %w", err)
	}
	if !s.runner.IsSubscribed(c.ID) {
		if err := s.runner.SubscribeToCampaign(c.ID); err != nil {
			s.revertSwap(ctx, c.ID, to, c.Status, c.HoldReason)
			return nil, fmt.Errorf("could not subscribe to the campaign: %w", err)
		}
	}
	return s.deps.Campaigns.Get(ctx, a.WorkspaceID, campaignID)
}

func (s *Service) Stop(ctx context.Context, a Actor, campaignID string) (*emailcampaign.Campaign, error) {
	c, err := s.deps.Campaigns.Get(ctx, a.WorkspaceID, campaignID)
	if err != nil {
		return nil, err
	}
	to, err := emailcampaign.Transition(c.Status, emailcampaign.ActionStop)
	if err != nil {
		return nil, err
	}
	if err := s.swap(ctx, *c, to, campaign.ActionStop); err != nil {
		return nil, err
	}
	if c.HoldReason == emailcampaign.HoldBalanceShort {
		s.resolveBalanceHold(ctx, *c)
	}
	if c.HoldReason.FailedTestBatch() {
		s.resolveTestBatchHold(ctx, *c)
	}
	if c.Status.Launched() {
		if err := s.runner.StopCampaignConsumer(c.ID); err != nil {
			s.logf("could not delete the queue of %s: %v", c.ID, err)
		}
		if err := s.settle(ctx, *c); err != nil {
			return nil, err
		}
	}
	return s.deps.Campaigns.Get(ctx, a.WorkspaceID, campaignID)
}

func (s *Service) swap(ctx context.Context, c emailcampaign.Campaign, to emailcampaign.Status, action campaign.Action) error {
	ok, err := s.deps.Campaigns.SwapStatus(ctx, c.ID, c.Status, to, emailcampaign.HoldNone)
	if err != nil {
		return err
	}
	if !ok {
		return campaign.SwapFailure(action)
	}
	return nil
}

func (s *Service) revertSwap(ctx context.Context, campaignID string, from, to emailcampaign.Status, reason emailcampaign.HoldReason) {
	if _, err := s.deps.Campaigns.SwapStatus(ctx, campaignID, from, to, reason); err != nil {
		s.logf("could not revert the status of %s: %v", campaignID, err)
	}
}
