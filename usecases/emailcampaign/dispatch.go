package emailcampaign_usecase

import (
	"context"
	"errors"
	"time"

	"vozko/domain/emailaddress"
	"vozko/domain/emailcampaign"
	"vozko/domain/emailconsent"
	"vozko/domain/emaildesign"
	"vozko/domain/emailsending"
	"vozko/domain/lead"
	balance_usecase "vozko/usecases/balance"
	"vozko/usecases/campaignqueue"
)

const (
	pauseRequeueDelay   = 3 * time.Second
	holdRetryDelay      = 15 * time.Minute
	balanceRetryDelay   = 5 * time.Second
	transientRetryDelay = time.Minute
	minimumCapDelay     = time.Minute
	retryHintFloor      = time.Second
	completionGrace     = 10 * time.Minute
	tickBatchSize       = 100
	unsubscribeLabel    = "Descadastrar"
	failureRender       = "render_failed"
	failureRejected     = "rejected"
	failureConfig       = "configuration"
)

var senderConfigErrors = []error{
	emailsending.ErrNoDefaultSender, emailsending.ErrSenderNotFound, emailsending.ErrDomainNotVerified,
	emailsending.ErrAccountNotProvisioned, emailsending.ErrAccountNotFound, emailsending.ErrDomainNotFound,
	emailsending.ErrFreeMailboxFrom, emailsending.ErrSenderDomainMismatch,
}

var renderErrors = []error{
	emaildesign.ErrInvalidDocument, emaildesign.ErrInvalidSubject, emaildesign.ErrUnsubscribeURLInvalid,
	emaildesign.ErrUnsubscribeLabelMissing, emaildesign.ErrLangInvalid, emailsending.ErrInvalidMessage,
	emailsending.ErrInsecureUnsubscribe,
}

func (s *Service) handle(msg campaignqueue.Message) campaignqueue.Result {
	ctx := context.Background()
	c, err := s.deps.Campaigns.GetByID(ctx, msg.CampaignID)
	if errors.Is(err, emailcampaign.ErrCampaignNotFound) {
		return campaignqueue.Drop
	}
	if err != nil {
		s.logf("campaign %s lookup failed: %v", msg.CampaignID, err)
		return campaignqueue.RetryLater(transientRetryDelay)
	}
	if c.HoldReason.FailedTestBatch() {
		return campaignqueue.Drop
	}
	switch c.Status {
	case emailcampaign.StatusRunning, emailcampaign.StatusHeld:
	case emailcampaign.StatusPaused:
		return campaignqueue.RetryLater(pauseRequeueDelay)
	default:
		return campaignqueue.Drop
	}
	rc, err := s.deps.Recipients.Get(ctx, msg.EntryID)
	if errors.Is(err, emailcampaign.ErrRecipientNotFound) {
		return campaignqueue.Drop
	}
	if err != nil {
		return campaignqueue.RetryLater(transientRetryDelay)
	}
	if rc.CampaignID != c.ID || rc.Status != emailcampaign.RecipientPending {
		return campaignqueue.Drop
	}
	if rc.Variant == emailcampaign.VariantWinner && (c.ABTest == nil || c.ABTest.Winner == "") {
		return campaignqueue.RetryLater(transientRetryDelay)
	}
	facts, skip, err := s.recheck(ctx, *c, *rc)
	if err != nil {
		return campaignqueue.RetryLater(transientRetryDelay)
	}
	subject, p := recipientMerge(*c, *rc, facts)
	if skip == emailcampaign.SkipNone && len(c.MissingExcluded(p)) > 0 {
		skip = emailcampaign.SkipMissingVariable
	}
	if skip != emailcampaign.SkipNone {
		s.markSkipped(ctx, rc.ID, skip)
		return campaignqueue.Done
	}
	if res, claimed := s.claimSlot(ctx, *c, *rc); !claimed {
		return res
	}
	sent, err := s.deps.Gateway.Send(ctx, emailsending.SendRequest{
		WorkspaceID: c.WorkspaceID,
		SenderID:    c.SenderID,
		To:          rc.Address,
		Kind:        emailsending.KindMarketing,
		RunKind:     emailsending.RunCampaign,
		RunID:       c.ID,
		RecipientID: rc.ID,
		Tags:        map[string]string{"campaign_id": c.ID, "variant": string(rc.Variant)},
	}, s.compose(*c, subject, p))
	return s.outcome(ctx, *c, *rc, sent, err)
}

func (s *Service) recheck(ctx context.Context, c emailcampaign.Campaign, rc emailcampaign.Recipient) (lead.EmailFacts, emailcampaign.SkipReason, error) {
	facts, err := s.factsOf(ctx, c.WorkspaceID, rc.LeadID)
	if err != nil {
		return facts, emailcampaign.SkipNone, err
	}
	subs := map[emailaddress.Address]emailconsent.Subscription{}
	sub, err := s.deps.Subscriptions.Get(ctx, c.WorkspaceID, rc.Address)
	switch {
	case errors.Is(err, emailconsent.ErrSubscriptionNotFound):
	case err != nil:
		return facts, emailcampaign.SkipNone, err
	default:
		subs[rc.Address] = *sub
	}
	consent := emailconsent.LeadFacts{OptedOut: facts.OptedOut, Blocked: facts.Blocked}
	return facts, consentSkip(subs, rc.Address, consent), nil
}

func (s *Service) factsOf(ctx context.Context, workspaceID, leadID string) (lead.EmailFacts, error) {
	if leadID == "" {
		return lead.EmailFacts{}, nil
	}
	found, err := s.deps.LeadFacts.EmailFacts(ctx, workspaceID, []string{leadID})
	if err != nil {
		return lead.EmailFacts{}, err
	}
	return found[leadID], nil
}

func recipientMerge(c emailcampaign.Campaign, rc emailcampaign.Recipient, facts lead.EmailFacts) (string, emaildesign.Personalization) {
	return c.SubjectFor(rc.Variant), c.Personalize(personalization(facts, rc.Address))
}

func personalization(f lead.EmailFacts, address emailaddress.Address) emaildesign.Personalization {
	return emaildesign.Personalization{
		FirstName: f.FirstName(),
		Name:      f.FullName(),
		Email:     address.String(),
		Company:   f.Company,
		Fields:    f.FieldValues(),
	}
}

func (s *Service) compose(c emailcampaign.Campaign, subject string, p emaildesign.Personalization) emailsending.ComposeFunc {
	return func(env emailsending.Envelope) (emailsending.Content, error) {
		return campaignContent(c, subject, p, emaildesign.RenderInput{UnsubscribeURL: env.UnsubscribeURL})
	}
}

func campaignContent(c emailcampaign.Campaign, subject string, p emaildesign.Personalization, in emaildesign.RenderInput) (emailsending.Content, error) {
	utm := c.TrackingUTM()
	in.Personalization, in.Subject, in.UnsubscribeLabel, in.UTM = p, subject, unsubscribeLabel, &utm
	rendered, err := emaildesign.Render(c.RenderDocument(), in)
	if err != nil {
		return emailsending.Content{}, err
	}
	line, err := emaildesign.RenderSubject(subject, p)
	if err != nil {
		return emailsending.Content{}, err
	}
	return emailsending.Content{Subject: line, HTML: rendered.HTML, Text: rendered.Text}, nil
}

func (s *Service) claimSlot(ctx context.Context, c emailcampaign.Campaign, rc emailcampaign.Recipient) (campaignqueue.Result, bool) {
	err := s.billing.Claim(ctx, c, rc.ID)
	var short *balance_usecase.InsufficientEmailBalanceError
	switch {
	case err == nil:
		return campaignqueue.Done, true
	case errors.As(err, &short):
		s.hold(ctx, c, emailcampaign.HoldBalanceShort)
		return campaignqueue.RetryLater(balanceRetryDelay), false
	case errors.Is(err, ErrNotSending):
		return campaignqueue.RetryLater(pauseRequeueDelay), false
	}
	s.logf("no paid slot for recipient %s of %s yet, retrying: %v", rc.ID, c.ID, err)
	return campaignqueue.RetryLater(transientRetryDelay), false
}

func (s *Service) outcome(ctx context.Context, c emailcampaign.Campaign, rc emailcampaign.Recipient, sent *emailsending.Message, err error) campaignqueue.Result {
	if err == nil {
		s.delivered(ctx, c, rc, sent)
		return campaignqueue.Done
	}
	res := s.refused(ctx, c, rc, err)
	if res.Outcome == campaignqueue.OutcomeRetryLater {
		if releaseErr := s.billing.Release(ctx, c, rc.ID); releaseErr != nil {
			s.logf("could not give back the slot of recipient %s: %v", rc.ID, releaseErr)
		}
	}
	return res
}

func (s *Service) delivered(ctx context.Context, c emailcampaign.Campaign, rc emailcampaign.Recipient, sent *emailsending.Message) {
	messageID := ""
	if sent != nil {
		messageID = sent.ID
	}
	if _, markErr := s.deps.Recipients.MarkSent(ctx, rc.ID, messageID, s.now()); markErr != nil {
		s.logf("recipient %s was sent but could not be marked: %v", rc.ID, markErr)
	}
	if c.Status != emailcampaign.StatusHeld {
		return
	}
	released, swapErr := s.deps.Campaigns.SwapStatus(ctx, c.ID, emailcampaign.StatusHeld, emailcampaign.StatusRunning, emailcampaign.HoldNone)
	if swapErr != nil {
		s.logf("could not release the hold of %s: %v", c.ID, swapErr)
		return
	}
	if released && c.HoldReason == emailcampaign.HoldBalanceShort {
		s.resolveBalanceHold(ctx, c)
	}
}

func (s *Service) refused(ctx context.Context, c emailcampaign.Campaign, rc emailcampaign.Recipient, err error) campaignqueue.Result {
	var capErr *emailsending.DailyCapError
	kind, isSendErr := emailsending.SendErrorKindOf(err)
	switch {
	case errors.Is(err, emailsending.ErrRecipientSuppressed):
		s.markSkipped(ctx, rc.ID, emailcampaign.SkipForSuppression(emailsending.SuppressReasonOf(err)))
		return campaignqueue.Done
	case errors.As(err, &capErr):
		s.hold(ctx, c, emailcampaign.HoldDailyCap)
		return campaignqueue.RetryLater(max(capErr.ResumeAt.Sub(s.now()), minimumCapDelay))
	case errors.Is(err, emailsending.ErrAccountPaused) || (isSendErr && kind == emailsending.SendErrorPaused):
		s.hold(ctx, c, emailcampaign.HoldAccountPaused)
		return campaignqueue.RetryLater(holdRetryDelay)
	case anyOf(err, senderConfigErrors):
		s.hold(ctx, c, emailcampaign.HoldSenderUnavailable)
		return campaignqueue.RetryLater(holdRetryDelay)
	case anyOf(err, renderErrors):
		s.markFailed(ctx, rc.ID, failureRender)
		return campaignqueue.Done
	case isSendErr && kind == emailsending.SendErrorRejected:
		s.markFailed(ctx, rc.ID, failureRejected)
		return campaignqueue.Done
	case isSendErr && kind == emailsending.SendErrorConfiguration:
		s.markFailed(ctx, rc.ID, failureConfig)
		return campaignqueue.Done
	}
	s.logf("send to recipient %s of %s will be retried: %v", rc.ID, c.ID, err)
	if hint, ok := emailsending.SendRetryAfter(err); ok {
		return campaignqueue.RetryLater(max(hint, retryHintFloor))
	}
	return campaignqueue.RetryLater(transientRetryDelay)
}

func anyOf(err error, targets []error) bool {
	for _, target := range targets {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}

func (s *Service) markSkipped(ctx context.Context, recipientID string, reason emailcampaign.SkipReason) {
	if _, err := s.deps.Recipients.MarkSkipped(ctx, recipientID, reason); err != nil {
		s.logf("could not skip recipient %s: %v", recipientID, err)
	}
}

func (s *Service) markFailed(ctx context.Context, recipientID, code string) {
	if _, err := s.deps.Recipients.MarkFailed(ctx, recipientID, code); err != nil {
		s.logf("could not fail recipient %s: %v", recipientID, err)
	}
}

func (s *Service) hold(ctx context.Context, c emailcampaign.Campaign, reason emailcampaign.HoldReason) {
	if c.Status == emailcampaign.StatusHeld && c.HoldReason == reason {
		return
	}
	ok, err := s.deps.Campaigns.SwapStatus(ctx, c.ID, c.Status, emailcampaign.StatusHeld, reason)
	if err != nil {
		s.logf("could not hold %s: %v", c.ID, err)
		return
	}
	if ok && reason != emailcampaign.HoldDailyCap {
		s.notifyHold(ctx, c, reason)
	}
}

type statusStore struct{ s *Service }

func (st statusStore) ListRunningCampaignIDs() ([]string, error) {
	items, err := st.s.deps.Campaigns.ListByStatus(context.Background(), emailcampaign.StatusRunning, emailcampaign.StatusHeld)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(items))
	for _, c := range items {
		ids = append(ids, c.ID)
	}
	return ids, nil
}

func (st statusStore) CompleteCampaign(campaignID string) (bool, error) {
	return st.s.completeIfDone(context.Background(), campaignID)
}

type pendingCounter struct{ s *Service }

func (p pendingCounter) CountPendingEntries(campaignID string) (int64, error) {
	counts, err := p.s.deps.Recipients.Counts(context.Background(), campaignID)
	if err != nil {
		return 0, err
	}
	return counts.Pending, nil
}

func (s *Service) completeIfDone(ctx context.Context, campaignID string) (bool, error) {
	c, err := s.deps.Campaigns.GetByID(ctx, campaignID)
	if err != nil || c.Status != emailcampaign.StatusRunning {
		return false, err
	}
	counts, err := s.deps.Recipients.Counts(ctx, campaignID)
	if err != nil || counts.Pending > 0 {
		return false, err
	}
	if counts.Total == 0 && (c.StartedAt == nil || s.now().Sub(*c.StartedAt) < completionGrace) {
		return false, nil
	}
	settled, err := s.billing.Settle(ctx, *c)
	if err != nil || !settled {
		return false, err
	}
	return s.deps.Campaigns.Complete(ctx, campaignID, s.now())
}

func (s *Service) settle(ctx context.Context, c emailcampaign.Campaign) error {
	_, err := s.billing.Settle(ctx, c)
	return err
}

func (s *Service) Tick(ctx context.Context) error {
	var errs []error
	if err := s.decideTestBatches(ctx); err != nil {
		errs = append(errs, err)
	}
	if err := s.decideWinners(ctx); err != nil {
		errs = append(errs, err)
	}
	if err := s.sweepCompletions(ctx); err != nil {
		errs = append(errs, err)
	}
	if err := s.sweepSettlements(ctx); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (s *Service) sweepSettlements(ctx context.Context) error {
	due, err := s.deps.Batches.ListUnsettled(ctx, tickBatchSize)
	if err != nil {
		return err
	}
	var errs []error
	for _, c := range due {
		if err := s.settle(ctx, c); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Service) decideWinners(ctx context.Context) error {
	due, err := s.deps.Campaigns.ListAwaitingWinner(ctx, s.now(), tickBatchSize)
	if err != nil {
		return err
	}
	var errs []error
	for _, c := range due {
		if c.ABTest == nil || c.TestBatch.Holding() {
			continue
		}
		results, err := s.deps.Metrics.VariantResults(ctx, c.WorkspaceID, c.ID)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		winner := emailcampaign.ChooseWinner(c.ABTest.WinnerMetric, results[emailcampaign.VariantA], results[emailcampaign.VariantB])
		if err := s.enqueue(ctx, c.ID, []emailcampaign.Variant{emailcampaign.VariantWinner}); err != nil {
			errs = append(errs, err)
			continue
		}
		if _, err := s.deps.Campaigns.SetWinner(ctx, c.ID, winner); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Service) sweepCompletions(ctx context.Context) error {
	running, err := s.deps.Campaigns.ListByStatus(ctx, emailcampaign.StatusRunning)
	if err != nil {
		return err
	}
	var errs []error
	for _, c := range running {
		if c.StartedAt == nil || s.now().Sub(*c.StartedAt) < completionGrace {
			continue
		}
		if _, err := s.completeIfDone(ctx, c.ID); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
