package carriercampaign_usecase

import (
	"context"
	"errors"
	"time"

	"vozko/domain/campaign"
	cc "vozko/domain/carriercampaign"
	"vozko/domain/carriermessaging"
	"vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/domain/rcs"
	"vozko/domain/sms"
	"vozko/usecases/campaignqueue"
	carriermessaging_usecase "vozko/usecases/carriermessaging"
)

const (
	retryBase        = 30 * time.Second
	retryCap         = 30 * time.Minute
	maxAttempts      = 5
	minQuietDelay    = time.Minute
	pausedRetryDelay = time.Minute

	ErrorCodeDeliveryFailed  = "delivery_failed"
	ErrorCodeVariantMissing  = "variant_missing"
	ErrorCodeFallbackInvalid = "fallback_invalid"
)

var pausingErrors = []error{
	carriermessaging.ErrChannelNotLaunched,
	carriermessaging.ErrQuietHours,
	carriermessaging.ErrInsufficientBalance,
	carriermessaging.ErrPriceUnavailable,
	rcs.ErrSenderCannotSend,
	rcs.ErrSenderNotFound,
	sms.ErrRouteCannotSend,
	sms.ErrRouteNotFound,
}

var skippingErrors = []struct {
	err    error
	reason cc.SkipReason
}{
	{carriermessaging.ErrFrequencyCap, cc.SkipFrequencyCap},
	{carriermessaging.ErrSuppressed, cc.SkipSuppressed},
	{carriermessaging.ErrPhoneInvalid, cc.SkipNoPhone},
	{campaign.ErrMissingVariable, cc.SkipMissingVariable},
}

func backoff(attempt int64) time.Duration {
	d := retryBase
	for i := int64(1); i < attempt && d < retryCap; i++ {
		d *= 2
	}
	return min(d, retryCap)
}

func attemptKey(recipientID string) string {
	return cc.QueueNamespace.Key + ":attempts:" + recipientID
}

type sendJob struct {
	recipient cc.Recipient
	run       cc.Run
	campaign  cc.Campaign
	lead      *lead.Lead
}

func (s *Service) Handle(msg campaignqueue.Message) campaignqueue.Result {
	ctx := context.Background()
	job, result, ok := s.load(ctx, msg)
	if !ok {
		return result
	}
	c := job.campaign
	rec := job.recipient
	if rec.Variant < 0 || rec.Variant >= len(c.Variants) {
		return s.fail(ctx, rec.ID, ErrorCodeVariantMissing)
	}
	vars, values, err := s.message(ctx, c, rec.Variant, job.lead)
	if err != nil {
		return s.outcome(ctx, job, nil, err)
	}
	fallback, err := c.Fallback.Spec(c.CreatedBy, values)
	if err != nil {
		s.deps.Logf("[carrier-campaign] fallback of recipient %s: %v", rec.ID, err)
		return s.fail(ctx, rec.ID, ErrorCodeFallbackInvalid)
	}
	v := c.Variants[rec.Variant]
	sent, err := s.deps.Carrier.Deliver(ctx, carriermessaging_usecase.DeliverRequest{
		MessageID: rec.ID, WorkspaceID: c.WorkspaceID, Channel: c.Channel, SenderID: c.SenderID,
		Phone: rec.Phone, Lead: job.lead, RunKind: carriermessaging.RunCampaign, Marketing: c.Kind.ObeysMarketingRules(),
		RunID: job.run.ID, Variant: rec.Variant, Content: v.Content, Text: v.Text, Vars: vars,
		Fallback: fallback, SentBy: conversation.SentByCampaign(c.ID),
	})
	return s.outcome(ctx, job, sent, err)
}

func (s *Service) load(ctx context.Context, msg campaignqueue.Message) (sendJob, campaignqueue.Result, bool) {
	rec, err := s.deps.Recipients.FindByID(ctx, msg.EntryID)
	if errors.Is(err, cc.ErrRecipientNotFound) {
		return sendJob{}, campaignqueue.Drop, false
	}
	if err != nil {
		return sendJob{}, s.retry(ctx, msg.EntryID, err), false
	}
	if rec.RunID != msg.CampaignID {
		return sendJob{}, campaignqueue.Drop, false
	}
	if rec.Status != cc.RecipientPending {
		return sendJob{}, campaignqueue.Done, false
	}
	run, err := s.deps.Runs.FindByID(ctx, rec.RunID)
	if errors.Is(err, cc.ErrRunNotFound) {
		return sendJob{}, campaignqueue.Drop, false
	}
	if err != nil {
		return sendJob{}, s.retry(ctx, rec.ID, err), false
	}
	switch campaign.NormalizeStatus(run.Status) {
	case campaign.StatusRunning:
	case campaign.StatusPaused:
		return sendJob{}, campaignqueue.RetryLater(pausedRetryDelay), false
	default:
		return sendJob{}, campaignqueue.Drop, false
	}
	c, err := s.deps.Campaigns.FindByIDUnscoped(ctx, run.CampaignID)
	if errors.Is(err, cc.ErrCampaignNotFound) {
		return sendJob{}, campaignqueue.Drop, false
	}
	if err != nil {
		return sendJob{}, s.retry(ctx, rec.ID, err), false
	}
	existing, err := s.deps.Messages.FindByID(ctx, rec.ID)
	switch {
	case err == nil && existing != nil:
		return sendJob{}, s.markSent(ctx, rec.ID, existing.ID), false
	case err != nil && !errors.Is(err, carriermessaging.ErrMessageNotFound):
		return sendJob{}, s.retry(ctx, rec.ID, err), false
	}
	contact, err := s.leadOf(c.WorkspaceID, rec.LeadID)
	if err != nil {
		return sendJob{}, s.retry(ctx, rec.ID, err), false
	}
	return sendJob{recipient: *rec, run: *run, campaign: *c, lead: contact}, campaignqueue.Result{}, true
}

func (s *Service) leadOf(workspaceID, leadID string) (*lead.Lead, error) {
	if leadID == "" {
		return nil, nil
	}
	found, err := s.deps.Leads.FindByIDs(workspaceID, []string{leadID})
	if err != nil {
		return nil, err
	}
	for _, l := range found {
		if l != nil && l.ID == leadID && l.WorkspaceID == workspaceID {
			return l, nil
		}
	}
	return nil, nil
}

func (s *Service) outcome(ctx context.Context, job sendJob, sent *carriermessaging.Message, err error) campaignqueue.Result {
	rec := job.recipient
	if err == nil {
		if sent == nil {
			return s.retry(ctx, rec.ID, errors.New("carrier campaign: the send returned no message"))
		}
		return s.markSent(ctx, rec.ID, sent.ID)
	}
	var quiet *carriermessaging.QuietHoursError
	if errors.As(err, &quiet) {
		return campaignqueue.RetryLater(max(quiet.NextOpen.Sub(s.now()), minQuietDelay))
	}
	for _, skip := range skippingErrors {
		if errors.Is(err, skip.err) {
			return s.skip(ctx, rec.ID, skip.reason)
		}
	}
	if errors.Is(err, carriermessaging.ErrRecipientRefused) {
		return s.skip(ctx, rec.ID, refusalOf(job.lead))
	}
	for _, pausing := range pausingErrors {
		if errors.Is(err, pausing) {
			return s.pauseForBlocker(ctx, job, err)
		}
	}
	return s.retry(ctx, rec.ID, err)
}

func refusalOf(l *lead.Lead) cc.SkipReason {
	reason, ok := cc.SkipFromShared(campaign.LeadRefusal(campaign.LeadFactsOf(l)))
	if !ok || reason == cc.SkipNoPhone {
		return cc.SkipBlocked
	}
	return reason
}

func (s *Service) markSent(ctx context.Context, recipientID, messageID string) campaignqueue.Result {
	if _, err := s.deps.Recipients.MarkSent(ctx, recipientID, messageID, s.now()); err != nil {
		return s.retry(ctx, recipientID, err)
	}
	s.clearAttempts(recipientID)
	return campaignqueue.Done
}

func (s *Service) skip(ctx context.Context, recipientID string, reason cc.SkipReason) campaignqueue.Result {
	if _, err := s.deps.Recipients.MarkSkipped(ctx, recipientID, reason); err != nil {
		return s.retry(ctx, recipientID, err)
	}
	s.clearAttempts(recipientID)
	return campaignqueue.Done
}

func (s *Service) fail(ctx context.Context, recipientID, code string) campaignqueue.Result {
	if _, err := s.deps.Recipients.MarkFailed(ctx, recipientID, code); err != nil {
		s.deps.Logf("[carrier-campaign] mark recipient %s failed: %v", recipientID, err)
	}
	s.clearAttempts(recipientID)
	return campaignqueue.Done
}

func (s *Service) retry(ctx context.Context, recipientID string, cause error) campaignqueue.Result {
	attempt, err := s.deps.Attempts.Incr(attemptKey(recipientID))
	if err != nil {
		s.deps.Logf("[carrier-campaign] attempts of recipient %s unreadable (%v) after: %v", recipientID, err, cause)
		return s.fail(ctx, recipientID, ErrorCodeDeliveryFailed)
	}
	if attempt >= maxAttempts {
		s.deps.Logf("[carrier-campaign] recipient %s failed after %d attempts: %v", recipientID, attempt, cause)
		return s.fail(ctx, recipientID, ErrorCodeDeliveryFailed)
	}
	return campaignqueue.RetryLater(backoff(attempt))
}

func (s *Service) clearAttempts(recipientID string) {
	if err := s.deps.Attempts.Del(attemptKey(recipientID)); err != nil {
		s.deps.Logf("[carrier-campaign] clear attempts of recipient %s: %v", recipientID, err)
	}
}

func (s *Service) pauseForBlocker(ctx context.Context, job sendJob, cause error) campaignqueue.Result {
	s.deps.Logf("[carrier-campaign] pausing run %s of campaign %s: %v", job.run.ID, job.campaign.ID, cause)
	if _, err := s.deps.Runs.SwapStatus(ctx, job.run.ID, campaign.StatusRunning, campaign.StatusPaused); err != nil {
		s.deps.Logf("[carrier-campaign] pause run %s: %v", job.run.ID, err)
	}
	if _, err := s.deps.Campaigns.SwapStatus(ctx, job.campaign.ID, cc.StatusRunning, cc.StatusPaused); err != nil {
		s.deps.Logf("[carrier-campaign] pause campaign %s: %v", job.campaign.ID, err)
	}
	if s.queue != nil {
		if err := s.queue.PauseCampaignConsumer(job.run.ID); err != nil {
			s.deps.Logf("[carrier-campaign] pause queue of run %s: %v", job.run.ID, err)
		}
	}
	return campaignqueue.Requeue
}
