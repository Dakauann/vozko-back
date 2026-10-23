package emailcampaign_usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"vozko/domain/campaign"
	audience "vozko/domain/campaignaudience"
	"vozko/domain/emailcampaign"
	"vozko/domain/selection"
	"vozko/usecases/campaignqueue"
)

const (
	scheduleBatchSize  = 200
	minimumScheduleGap = time.Minute
	holdLaunchFailed   = emailcampaign.HoldReason("launch_failed")
)

func (s *Service) Launch(ctx context.Context, a Actor, campaignID string, at *time.Time) (*emailcampaign.Campaign, error) {
	c, err := s.deps.Campaigns.Get(ctx, a.WorkspaceID, campaignID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	if at != nil && at.After(now.Add(minimumScheduleGap)) {
		return s.schedule(ctx, a, *c, at.UTC())
	}
	if at != nil && !at.After(now) {
		report := emailcampaign.LaunchReport{Blocking: []emailcampaign.Issue{{Code: emailcampaign.IssueScheduleInPast}}}
		return nil, &LaunchBlockedError{Report: report}
	}
	if err := s.start(ctx, scopeOf(a), *c, a.UserID); err != nil {
		return nil, err
	}
	return s.deps.Campaigns.Get(ctx, a.WorkspaceID, campaignID)
}

func (s *Service) schedule(ctx context.Context, a Actor, c emailcampaign.Campaign, at time.Time) (*emailcampaign.Campaign, error) {
	if _, err := emailcampaign.Transition(c.Status, emailcampaign.ActionSchedule); err != nil {
		return nil, err
	}
	r, err := s.review(ctx, scopeOf(a), c, &at)
	if err != nil {
		return nil, err
	}
	if !r.Check.ReadyLater() {
		return nil, &LaunchBlockedError{Report: r.Check}
	}
	ok, err := s.deps.Campaigns.Schedule(ctx, c.ID, c.Status, at, a.UserID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, emailcampaign.ErrNotEditable
	}
	return s.deps.Campaigns.Get(ctx, a.WorkspaceID, c.ID)
}

func (s *Service) Ready(ctx context.Context, a Actor, campaignID string) (*emailcampaign.Campaign, error) {
	c, err := s.deps.Campaigns.Get(ctx, a.WorkspaceID, campaignID)
	if err != nil {
		return nil, err
	}
	to, err := emailcampaign.Transition(c.Status, emailcampaign.ActionReady)
	if err != nil {
		return nil, err
	}
	r, err := s.review(ctx, scopeOf(a), *c, nil)
	if err != nil {
		return nil, err
	}
	if !r.Check.ReadyLater() {
		return nil, &LaunchBlockedError{Report: r.Check}
	}
	ok, err := s.deps.Campaigns.SwapStatus(ctx, c.ID, c.Status, to, emailcampaign.HoldNone)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, emailcampaign.ErrNotDraft
	}
	return s.deps.Campaigns.Get(ctx, a.WorkspaceID, campaignID)
}

func (s *Service) Unschedule(ctx context.Context, a Actor, campaignID string) (*emailcampaign.Campaign, error) {
	c, err := s.deps.Campaigns.Get(ctx, a.WorkspaceID, campaignID)
	if err != nil {
		return nil, err
	}
	to, err := emailcampaign.Transition(c.Status, emailcampaign.ActionUnschedule)
	if err != nil {
		return nil, err
	}
	ok, err := s.deps.Campaigns.SwapStatus(ctx, c.ID, c.Status, to, emailcampaign.HoldNone)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, emailcampaign.ErrNotScheduled
	}
	return s.deps.Campaigns.Get(ctx, a.WorkspaceID, campaignID)
}

func (s *Service) start(ctx context.Context, scope selection.Scope, c emailcampaign.Campaign, actorID string) error {
	if _, err := emailcampaign.Transition(c.Status, emailcampaign.ActionStart); err != nil {
		return err
	}
	if !c.Status.Editable() {
		return emailcampaign.ErrNotDraft
	}
	if err := s.ensureListLeads(ctx, c); err != nil {
		return err
	}
	r, err := s.review(ctx, scope, c, nil)
	if err != nil {
		return err
	}
	if !r.Check.Ready() {
		return &LaunchBlockedError{Report: r.Check}
	}
	if err := s.deps.Reactivation.Reactivate(ctx, c.WorkspaceID); err != nil {
		return err
	}
	ok, err := s.deps.Campaigns.MarkLaunched(ctx, c.ID, c.Status, emailcampaign.Launch{StartedAt: s.now(), LaunchedBy: actorID})
	if err != nil {
		return err
	}
	if !ok {
		return campaign.ErrAlreadyRunning
	}
	revert := func(cause error) error {
		if _, err := s.deps.Campaigns.SwapStatus(ctx, c.ID, emailcampaign.StatusRunning, c.Status, emailcampaign.HoldNone); err != nil {
			s.logf("could not revert the launch of %s: %v", c.ID, err)
		}
		return cause
	}
	recipients := r.plan.materialize(c)
	if err := s.deps.Recipients.Replace(ctx, c.WorkspaceID, c.ID, recipients); err != nil {
		return revert(err)
	}
	if err := s.enqueueLaunch(ctx, c, r.TestBatch); err != nil {
		return s.abortQueued(ctx, c, err)
	}
	s.releaseList(ctx, c, c.ListID)
	return nil
}

func (s *Service) ensureListLeads(ctx context.Context, c emailcampaign.Campaign) error {
	if c.ListID == "" {
		return nil
	}
	if s.audience == nil {
		return audience.ErrUnavailable
	}
	return s.audience.EnsureLeads(ctx, c.WorkspaceID, c.AudienceOwner(), c.ListID)
}

func (s *Service) abort(ctx context.Context, campaignID string, cause error) error {
	if _, err := s.deps.Campaigns.SwapStatus(ctx, campaignID, emailcampaign.StatusRunning, emailcampaign.StatusStopped, emailcampaign.HoldNone); err != nil {
		s.logf("could not stop %s after a failed launch: %v", campaignID, err)
	}
	return cause
}

func (s *Service) abortQueued(ctx context.Context, c emailcampaign.Campaign, cause error) error {
	if err := s.runner.StopCampaignConsumer(c.ID); err != nil {
		s.logf("could not stop the queue of %s: %v", c.ID, err)
	}
	if err := s.abort(ctx, c.ID, cause); err != nil && err != cause {
		return err
	}
	if err := s.settle(ctx, c); err != nil {
		s.logf("could not refund %s after a failed launch: %v", c.ID, err)
	}
	return cause
}

func (s *Service) enqueue(ctx context.Context, campaignID string, variants []emailcampaign.Variant) error {
	ids, err := s.pendingAfter(ctx, campaignID, variants, "")
	if err != nil {
		return err
	}
	return s.queueIDs(ctx, campaignID, ids)
}

func (s *Service) pendingAfter(ctx context.Context, campaignID string, variants []emailcampaign.Variant, after string) ([]string, error) {
	var out []string
	for {
		ids, err := s.deps.Recipients.PendingIDs(ctx, campaignID, variants, after, resolvePage)
		if err != nil {
			return nil, err
		}
		out = append(out, ids...)
		if len(ids) < resolvePage {
			return out, nil
		}
		after = ids[len(ids)-1]
	}
}

func (s *Service) queueIDs(ctx context.Context, campaignID string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	messages := make([]campaignqueue.Message, 0, len(ids))
	for _, id := range ids {
		messages = append(messages, campaignqueue.Message{CampaignID: campaignID, EntryID: id})
	}
	if err := s.runner.SubscribeToCampaign(campaignID); err != nil {
		return err
	}
	if err := s.dispatcher.Enqueue(campaignID, messages); err != nil {
		return err
	}
	counts, err := s.deps.Recipients.Counts(ctx, campaignID)
	if err != nil {
		return err
	}
	s.runner.SetCounter(campaignID, int(counts.Pending))
	return nil
}

func (s *Service) StartScheduled(ctx context.Context) error {
	now := s.now()
	due, err := s.deps.Campaigns.ListScheduledDue(ctx, now, scheduleBatchSize)
	if err != nil {
		return err
	}
	var errs []error
	for _, c := range due {
		claimed, err := s.deps.Campaigns.ClaimScheduledStart(c.ID, now)
		if err != nil {
			errs = append(errs, fmt.Errorf("claim the schedule of %s: %w", c.ID, err))
			continue
		}
		if !claimed {
			continue
		}
		scope := selection.Scope{WorkspaceID: c.WorkspaceID, ActorID: c.LaunchedBy}
		err = s.start(ctx, scope, c, c.LaunchedBy)
		if err == nil || errors.Is(err, campaign.ErrAlreadyRunning) {
			continue
		}
		if s.waitForChecks(ctx, c, err) {
			continue
		}
		reason := scheduleFailure(err)
		if _, swapErr := s.deps.Campaigns.SwapStatus(ctx, c.ID, emailcampaign.StatusScheduled, emailcampaign.StatusDraft, reason); swapErr != nil {
			errs = append(errs, swapErr)
		}
		s.notifyScheduleFailed(ctx, c, reason)
		s.logf("scheduled start of %s failed: %v", c.ID, err)
	}
	return errors.Join(errs...)
}

func scheduleFailure(err error) emailcampaign.HoldReason {
	var blocked *LaunchBlockedError
	if errors.As(err, &blocked) && len(blocked.Report.Blocking) > 0 {
		return emailcampaign.HoldReason(blocked.Report.Blocking[0].Code)
	}
	return holdLaunchFailed
}

func (s *Service) waitForChecks(ctx context.Context, c emailcampaign.Campaign, err error) bool {
	var blocked *LaunchBlockedError
	if !errors.As(err, &blocked) || !blocked.Report.OnlyWaiting() {
		return false
	}
	ok, swapErr := s.deps.Campaigns.Schedule(ctx, c.ID, emailcampaign.StatusScheduled, s.now().Add(minimumScheduleGap), c.LaunchedBy)
	if swapErr != nil || !ok {
		s.logf("scheduled start of %s waits for domain checks but could not be rescheduled: %v", c.ID, swapErr)
		return false
	}
	return true
}
