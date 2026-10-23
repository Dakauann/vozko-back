package carriercampaign_usecase

import (
	"context"
	"errors"

	"vozko/domain/campaign"
	cc "vozko/domain/carriercampaign"
	"vozko/domain/carriermessaging"
	"vozko/domain/rcs"
)

func (s *Service) Pause(ctx context.Context, workspaceID, id string) error {
	c, run, err := s.active(ctx, workspaceID, id, cc.ActionPause)
	if err != nil {
		return err
	}
	if s.queue == nil {
		return ErrQueueMissing
	}
	paused, err := run.Apply(campaign.ActionPause)
	if err != nil {
		return err
	}
	if err := s.swapBoth(ctx, c, cc.StatusPaused, run, paused.Status); err != nil {
		return err
	}
	return s.queue.PauseCampaignConsumer(run.ID)
}

func (s *Service) Resume(ctx context.Context, workspaceID, id string) error {
	c, run, err := s.active(ctx, workspaceID, id, cc.ActionStart)
	if err != nil {
		return err
	}
	if s.queue == nil {
		return ErrQueueMissing
	}
	resumed, err := run.Apply(campaign.ActionStart)
	if err != nil {
		return err
	}
	if _, err := s.ready(ctx, c); err != nil {
		return err
	}
	if err := s.swapBoth(ctx, c, cc.StatusRunning, run, resumed.Status); err != nil {
		return err
	}
	run.Status = resumed.Status
	counts, err := s.deps.Recipients.Counts(ctx, run.ID)
	if err != nil {
		return err
	}
	if counts.Pending == 0 {
		_, err := s.completeRun(ctx, run)
		return err
	}
	if err := s.queue.ResumeCampaignConsumer(run.ID); err != nil {
		return err
	}
	if !s.queue.IsSubscribed(run.ID) {
		if err := s.queue.SubscribeToCampaign(run.ID); err != nil {
			return err
		}
	}
	s.queue.SeedCounterIfMissing(run.ID)
	return nil
}

func (s *Service) Stop(ctx context.Context, workspaceID, id string) error {
	c, err := s.deps.Campaigns.FindByID(ctx, workspaceID, id)
	if err != nil {
		return err
	}
	target, err := cc.Transition(c.Status, cc.ActionStop)
	if err != nil {
		return err
	}
	if c.Status == cc.StatusScheduled {
		swapped, err := s.deps.Campaigns.Reschedule(ctx, c.ID, c.Status, target, nil)
		if err != nil {
			return err
		}
		if !swapped {
			return campaign.SwapFailure(campaign.ActionStop)
		}
		return nil
	}
	if s.queue == nil {
		return ErrQueueMissing
	}
	run, err := s.deps.Runs.FindActive(ctx, c.ID)
	if err != nil {
		return err
	}
	stopped, err := run.Apply(campaign.ActionStop)
	if err != nil {
		return err
	}
	if err := s.swapBoth(ctx, *c, target, *run, stopped.Status); err != nil {
		return err
	}
	return s.queue.StopCampaignConsumer(run.ID)
}

func (s *Service) active(ctx context.Context, workspaceID, id string, action cc.Action) (cc.Campaign, cc.Run, error) {
	c, err := s.deps.Campaigns.FindByID(ctx, workspaceID, id)
	if err != nil {
		return cc.Campaign{}, cc.Run{}, err
	}
	if _, err := cc.Transition(c.Status, action); err != nil {
		return cc.Campaign{}, cc.Run{}, err
	}
	run, err := s.deps.Runs.FindActive(ctx, c.ID)
	if err != nil {
		return cc.Campaign{}, cc.Run{}, err
	}
	return *c, *run, nil
}

func (s *Service) swapBoth(ctx context.Context, c cc.Campaign, campaignTarget cc.Status, run cc.Run, runTarget campaign.Status) error {
	swapped, err := s.deps.Campaigns.SwapStatus(ctx, c.ID, c.Status, campaignTarget)
	if err != nil {
		return err
	}
	if !swapped {
		return cc.ErrStatusInvalid
	}
	swapped, err = s.deps.Runs.SwapStatus(ctx, run.ID, run.Status, runTarget)
	if err == nil && !swapped {
		err = cc.ErrRunFinished
	}
	if err != nil {
		s.revertCampaign(ctx, c.ID, campaignTarget, c.Status)
		return err
	}
	return nil
}

func (s *Service) ListRunningCampaignIDs() ([]string, error) {
	runs, err := s.deps.Runs.ListByStatus(context.Background(), campaign.StatusRunning)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(runs))
	for _, r := range runs {
		ids = append(ids, r.ID)
	}
	return ids, nil
}

func (s *Service) CountPendingEntries(runID string) (int64, error) {
	counts, err := s.deps.Recipients.Counts(context.Background(), runID)
	if err != nil {
		return 0, err
	}
	return counts.Pending, nil
}

func (s *Service) CompleteCampaign(runID string) (bool, error) {
	ctx := context.Background()
	run, err := s.deps.Runs.FindByID(ctx, runID)
	if err != nil {
		return false, err
	}
	return s.completeRun(ctx, *run)
}

func (s *Service) completeRun(ctx context.Context, run cc.Run) (bool, error) {
	now := s.now()
	done, err := s.deps.Runs.Complete(ctx, run.ID, now)
	if err != nil || !done {
		return false, err
	}
	c, err := s.deps.Campaigns.FindByIDUnscoped(ctx, run.CampaignID)
	if err != nil {
		return true, err
	}
	next, err := c.AfterRun(run.Occurrence, now)
	if err != nil {
		s.deps.Logf("[carrier-campaign] campaign %s after run %s: %v", c.ID, run.ID, err)
	} else if _, err := s.deps.Campaigns.Reschedule(ctx, c.ID, c.Status, next.Status, next.ScheduledAt); err != nil {
		s.deps.Logf("[carrier-campaign] reschedule campaign %s: %v", c.ID, err)
	}
	s.checkHealth(ctx, *c, run)
	return true, nil
}

func (s *Service) checkHealth(ctx context.Context, c cc.Campaign, run cc.Run) {
	if c.Channel != carriermessaging.ChannelRCS {
		return
	}
	f, err := s.deps.Funnels.Funnel(ctx, cc.FunnelQuery{WorkspaceID: c.WorkspaceID, Channel: c.Channel, CampaignID: c.ID, RunID: run.ID})
	if err != nil {
		s.deps.Logf("[carrier-campaign] health of run %s: %v", run.ID, err)
		return
	}
	if !carriermessaging.OptOutRateBreached(int(f.Delivered), int(f.OptOuts)) {
		return
	}
	if _, err := s.deps.SenderPauser.AdminPauseSender(ctx, c.SenderID, rcs.PauseOptOutRate); err != nil && !errors.Is(err, rcs.ErrSenderTransition) {
		s.deps.Logf("[carrier-campaign] pause sender %s for opt outs: %v", c.SenderID, err)
	}
}
