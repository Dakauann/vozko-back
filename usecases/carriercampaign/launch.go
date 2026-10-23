package carriercampaign_usecase

import (
	"context"
	"time"

	"vozko/domain/campaign"
	cc "vozko/domain/carriercampaign"
	"vozko/domain/carriermessaging"
	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/shared"
	"vozko/domain/sms"
	"vozko/usecases/campaignqueue"
)

const materializePage = 500

type LaunchResult struct {
	Campaign *cc.Campaign
	Run      *cc.Run
}

func (s *Service) Launch(ctx context.Context, workspaceID, id string, a Actor) (LaunchResult, error) {
	if a.UserID == "" {
		return LaunchResult{}, ErrActorRequired
	}
	c, err := s.deps.Campaigns.FindByID(ctx, workspaceID, id)
	if err != nil {
		return LaunchResult{}, err
	}
	if !c.Status.Editable() {
		return LaunchResult{}, cc.ErrNotEditable
	}
	if !s.launched(c.Channel) {
		return LaunchResult{}, carriermessaging.ErrChannelNotLaunched
	}
	now := s.now()
	defs, err := s.fields(workspaceID)
	if err != nil {
		return LaunchResult{}, err
	}
	if err := c.ReadyToLaunch(now, defs); err != nil {
		return LaunchResult{}, err
	}
	route, err := s.ready(ctx, *c)
	if err != nil {
		return LaunchResult{}, err
	}
	if err := s.confirmAudience(ctx, *c, a); err != nil {
		return LaunchResult{}, err
	}
	if c.ScheduledAt != nil || c.Recurrence != nil {
		return s.schedule(ctx, *c, now, defs)
	}
	run, err := s.startRun(ctx, *c, a, route, now, true)
	if err != nil {
		return LaunchResult{}, err
	}
	stored, err := s.deps.Campaigns.FindByIDUnscoped(ctx, c.ID)
	if err != nil {
		return LaunchResult{}, err
	}
	return LaunchResult{Campaign: stored, Run: run}, nil
}

func (s *Service) schedule(ctx context.Context, c cc.Campaign, now time.Time, defs []*customfield.Definition) (LaunchResult, error) {
	scheduled, err := c.Schedule(now, defs)
	if err != nil {
		return LaunchResult{}, err
	}
	scheduled.UpdatedAt = now
	swapped, err := s.deps.Campaigns.Reschedule(ctx, c.ID, c.Status, scheduled.Status, scheduled.ScheduledAt)
	if err != nil {
		return LaunchResult{}, err
	}
	if !swapped {
		return LaunchResult{}, cc.ErrNotEditable
	}
	return LaunchResult{Campaign: &scheduled}, nil
}

func (s *Service) startRun(ctx context.Context, c cc.Campaign, a Actor, route *sms.Route, occurrence time.Time, refuseEmpty bool) (*cc.Run, error) {
	if s.queue == nil {
		return nil, ErrQueueMissing
	}
	target, err := cc.Transition(c.Status, cc.ActionStart)
	if err != nil {
		return nil, err
	}
	q, err := s.quote(ctx, c, a, route)
	if err != nil {
		return nil, err
	}
	if q.audience == 0 && refuseEmpty {
		return nil, ErrAudienceEmpty
	}
	if q.shortfall > 0 {
		return nil, &carriermessaging.ShortfallError{MissingMicros: q.shortfall}
	}
	now := s.now()
	run := cc.Run{
		ID: s.deps.NewID(), WorkspaceID: c.WorkspaceID, CampaignID: c.ID, Occurrence: occurrence,
		Status: campaign.StatusRunning, StartedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	frozen, err := s.deps.Selections.Freeze(ctx, scopeOf(c.WorkspaceID, a), c.Audience, run.ID, nil)
	if err != nil {
		return nil, err
	}
	defer s.dropSnapshot(ctx, c.WorkspaceID, run.ID)
	if frozen.Size == 0 && refuseEmpty {
		return nil, ErrAudienceEmpty
	}
	swapped, err := s.deps.Campaigns.SwapStatus(ctx, c.ID, c.Status, target)
	if err != nil {
		return nil, err
	}
	if !swapped {
		return nil, campaign.SwapFailure(campaign.ActionStart)
	}
	created, err := s.deps.Runs.Create(ctx, &run)
	if err == nil && !created {
		err = ErrRunAlreadyExists
	}
	if err != nil {
		s.revertCampaign(ctx, c.ID, target, c.Status)
		return nil, err
	}
	messages, err := s.materialize(ctx, c, run)
	if err != nil {
		return nil, s.abortRun(ctx, c, run, err)
	}
	if len(messages) == 0 {
		if _, err := s.completeRun(ctx, run); err != nil {
			return nil, err
		}
		return &run, nil
	}
	if err := s.queue.SubscribeToCampaign(run.ID); err != nil {
		return nil, s.abortRun(ctx, c, run, err)
	}
	if err := s.deps.Enqueuer.Enqueue(run.ID, messages); err != nil {
		if stopErr := s.queue.StopCampaignConsumer(run.ID); stopErr != nil {
			s.deps.Logf("[carrier-campaign] stop queue of run %s: %v", run.ID, stopErr)
		}
		return nil, s.abortRun(ctx, c, run, err)
	}
	return &run, nil
}

func (s *Service) dropSnapshot(ctx context.Context, workspaceID, snapshotID string) {
	if err := s.deps.Selections.DropSnapshot(ctx, workspaceID, snapshotID); err != nil {
		s.deps.Logf("[carrier-campaign] drop snapshot %s: %v", snapshotID, err)
	}
}

func (s *Service) revertCampaign(ctx context.Context, id string, from, to cc.Status) {
	if _, err := s.deps.Campaigns.SwapStatus(ctx, id, from, to); err != nil {
		s.deps.Logf("[carrier-campaign] revert campaign %s to %s: %v", id, to, err)
	}
}

func (s *Service) abortRun(ctx context.Context, c cc.Campaign, run cc.Run, cause error) error {
	if _, err := s.deps.Runs.SwapStatus(ctx, run.ID, campaign.StatusRunning, campaign.StatusStopped); err != nil {
		s.deps.Logf("[carrier-campaign] stop run %s: %v", run.ID, err)
	}
	s.revertCampaign(ctx, c.ID, cc.StatusRunning, c.Status)
	return cause
}

func (s *Service) materialize(ctx context.Context, c cc.Campaign, run cc.Run) ([]campaignqueue.Message, error) {
	seen := map[string]bool{}
	var messages []campaignqueue.Message
	after := ""
	for {
		ids, err := s.deps.Selections.Snapshot(ctx, c.WorkspaceID, run.ID, after, materializePage)
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			return messages, nil
		}
		recipients, pending, err := s.page(ctx, c, run, ids, seen)
		if err != nil {
			return nil, err
		}
		if err := s.deps.Recipients.CreateMany(ctx, recipients); err != nil {
			return nil, err
		}
		messages = append(messages, pending...)
		if len(ids) < materializePage {
			return messages, nil
		}
		after = ids[len(ids)-1]
	}
}

type judgedRecipient struct {
	leadID string
	phone  string
	reason cc.SkipReason
}

func (s *Service) judgeRecipients(ctx context.Context, workspaceID string, kind cc.Kind, ids []string, seen map[string]bool) ([]judgedRecipient, error) {
	found, err := s.deps.Leads.FindByIDs(workspaceID, ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*lead.Lead, len(found))
	phones := make([]string, 0, len(found))
	for _, l := range found {
		if l == nil || l.WorkspaceID != workspaceID {
			continue
		}
		byID[l.ID] = l
		if phone := shared.NormalizePhone(l.Number); phone != "" {
			phones = append(phones, phone)
		}
	}
	marketing := kind.ObeysMarketingRules()
	suppressed := map[string]bool{}
	if marketing && len(phones) > 0 {
		if suppressed, err = s.deps.Suppressions.SuppressedAmong(ctx, workspaceID, phones); err != nil {
			return nil, err
		}
	}
	out := make([]judgedRecipient, 0, len(ids))
	for _, id := range ids {
		l := byID[id]
		phone := ""
		if l != nil {
			phone = shared.NormalizePhone(l.Number)
		}
		facts := cc.RecipientFacts{Lead: campaign.LeadFactsOf(l)}
		facts.Lead.HasIdentity = phone != ""
		facts.Lead.OptedOut = facts.Lead.OptedOut && marketing
		facts.Suppressed = marketing && suppressed[phone]
		facts.Duplicate = phone != "" && seen[phone]
		reason := cc.SkipFor(kind, facts)
		if reason == "" {
			seen[phone] = true
		}
		out = append(out, judgedRecipient{leadID: id, phone: phone, reason: reason})
	}
	return out, nil
}

func (s *Service) page(ctx context.Context, c cc.Campaign, run cc.Run, ids []string, seen map[string]bool) ([]cc.Recipient, []campaignqueue.Message, error) {
	judged, err := s.judgeRecipients(ctx, c.WorkspaceID, c.Kind, ids, seen)
	if err != nil {
		return nil, nil, err
	}
	now := s.now()
	recipients := make([]cc.Recipient, 0, len(judged))
	var pending []campaignqueue.Message
	for _, j := range judged {
		rec := cc.Recipient{
			ID: s.deps.NewID(), WorkspaceID: c.WorkspaceID, CampaignID: c.ID, RunID: run.ID, LeadID: j.leadID,
			Phone: j.phone, Status: cc.RecipientPending, CreatedAt: now, UpdatedAt: now,
		}
		rec.Variant = c.VariantFor(rec.ID)
		if j.reason != "" {
			rec.Status, rec.SkipReason = cc.RecipientSkipped, j.reason
		} else {
			pending = append(pending, campaignqueue.Message{CampaignID: run.ID, EntryID: rec.ID, PhoneNumber: j.phone})
		}
		recipients = append(recipients, rec)
	}
	return recipients, pending, nil
}

func (s *Service) confirmAudience(ctx context.Context, c cc.Campaign, a Actor) error {
	if !c.HasAudience() {
		return cc.ErrAudienceRequired
	}
	matched, err := s.deps.Selections.Selected(ctx, scopeOf(c.WorkspaceID, a), c.Audience)
	if err != nil {
		return err
	}
	return c.ConfirmAudience(matched)
}
