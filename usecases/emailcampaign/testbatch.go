package emailcampaign_usecase

import (
	"context"
	"errors"

	"vozko/domain/emailcampaign"
	"vozko/domain/emailsending"
	"vozko/domain/notification"
	"vozko/domain/workspace"
)

var testBatchCopy = map[emailcampaign.HoldReason]string{
	emailcampaign.HoldTestBatchBounces:    "Mais de 3% dos e-mails do lote de teste voltaram como endereços que não existem. O restante da lista não foi enviado. Revise a lista antes de uma nova campanha; esta só pode ser encerrada.",
	emailcampaign.HoldTestBatchComplaints: "Destinatários do lote de teste marcaram o e-mail como spam acima de 0,1%. O restante da lista não foi enviado. Confira se essas pessoas pediram para receber seus e-mails; esta campanha só pode ser encerrada.",
}

func testBatchEpisode(campaignID string) string {
	return "email_campaign_test_batch:" + campaignID
}

func launchVariants(c emailcampaign.Campaign) []emailcampaign.Variant {
	if c.ABTest != nil {
		return []emailcampaign.Variant{emailcampaign.VariantA, emailcampaign.VariantB}
	}
	return []emailcampaign.Variant{emailcampaign.VariantA}
}

func (s *Service) enqueueLaunch(ctx context.Context, c emailcampaign.Campaign, testBatch int) error {
	if testBatch <= 0 {
		return s.enqueue(ctx, c.ID, launchVariants(c))
	}
	ids, err := s.deps.Recipients.PendingIDs(ctx, c.ID, launchVariants(c), "", testBatch)
	if err != nil || len(ids) == 0 {
		return err
	}
	tb := emailcampaign.TestBatch{Size: len(ids), State: emailcampaign.TestBatchTesting, LastID: ids[len(ids)-1]}
	ok, err := s.deps.Campaigns.SetTestBatch(ctx, c.ID, emailcampaign.TestBatchNone, tb)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("emailcampaign: the test batch was already set")
	}
	return s.queueIDs(ctx, c.ID, ids)
}

func (s *Service) decideTestBatches(ctx context.Context) error {
	due, err := s.deps.Campaigns.ListTesting(ctx, tickBatchSize)
	if err != nil {
		return err
	}
	var errs []error
	for _, c := range due {
		if err := s.judgeTestBatch(ctx, c); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Service) judgeTestBatch(ctx context.Context, c emailcampaign.Campaign) error {
	tb := *c.TestBatch
	now := s.now()
	if tb.DrainedAt == nil {
		pending, err := s.deps.Recipients.PendingThrough(ctx, c.ID, tb.LastID)
		if err != nil || pending > 0 {
			return err
		}
		tb.DrainedAt = &now
		if _, err := s.deps.Campaigns.SetTestBatch(ctx, c.ID, emailcampaign.TestBatchTesting, tb); err != nil {
			return err
		}
	}
	outcome, err := s.deps.Outcomes.RunOutcome(ctx, emailsending.RunCampaign, c.ID)
	if err != nil {
		return err
	}
	billing, err := s.deps.Batches.BillingState(ctx, c.ID)
	if err != nil {
		return err
	}
	decision := emailcampaign.DecideTestBatch(emailcampaign.TestBatchOutcome{
		Sent: billing.Slots.Used, Delivered: outcome.Delivered, HardBounces: outcome.HardBounces, Complaints: outcome.Complaints,
		Drained: true, DrainedAt: *tb.DrainedAt,
	}, now)
	switch decision {
	case emailcampaign.GateWait:
		return nil
	case emailcampaign.GatePass:
		return s.passTestBatch(ctx, c, tb)
	}
	return s.failTestBatch(ctx, c, tb, decision.Hold())
}

func (s *Service) passTestBatch(ctx context.Context, c emailcampaign.Campaign, tb emailcampaign.TestBatch) error {
	decided := s.now()
	passed := tb
	passed.State, passed.DecidedAt = emailcampaign.TestBatchPassed, &decided
	ok, err := s.deps.Campaigns.SetTestBatch(ctx, c.ID, emailcampaign.TestBatchTesting, passed)
	if err != nil || !ok {
		return err
	}
	ids, err := s.pendingAfter(ctx, c.ID, launchVariants(c), tb.LastID)
	if err == nil {
		err = s.queueIDs(ctx, c.ID, ids)
	}
	if err != nil {
		if _, revertErr := s.deps.Campaigns.SetTestBatch(ctx, c.ID, emailcampaign.TestBatchPassed, tb); revertErr != nil {
			s.logf("CRITICAL the test batch of %s passed but the rest could not be queued nor the pass undone: %v", c.ID, revertErr)
		}
		return err
	}
	return nil
}

func (s *Service) failTestBatch(ctx context.Context, c emailcampaign.Campaign, tb emailcampaign.TestBatch, reason emailcampaign.HoldReason) error {
	decided := s.now()
	failed := tb
	failed.State, failed.DecidedAt = emailcampaign.TestBatchFailed, &decided
	ok, err := s.deps.Campaigns.SetTestBatch(ctx, c.ID, emailcampaign.TestBatchTesting, failed)
	if err != nil || !ok {
		return err
	}
	held, err := s.deps.Campaigns.SwapStatus(ctx, c.ID, c.Status, emailcampaign.StatusHeld, reason)
	if err != nil {
		return err
	}
	if !held {
		s.logf("the test batch of %s failed but the campaign moved on before it could be held", c.ID)
		return nil
	}
	s.publish(ctx, notification.Draft{
		WorkspaceID: c.WorkspaceID,
		Category:    notification.CategoryCampaigns,
		Severity:    notification.SeverityCritical,
		Title:       notification.ClipTitle("Campanha de e-mail parada no lote de teste: " + c.Name),
		Body:        testBatchCopy[reason],
		ActionURL:   campaignLink(c.ID),
		ActionLabel: "Ver campanha",
		Audience:    notification.Holding(workspace.ResourceEmailMarketing, workspace.ActionStart),
		EpisodeKey:  testBatchEpisode(c.ID),
		Source:      notificationSource,
	})
	return nil
}

func (s *Service) resolveTestBatchHold(ctx context.Context, c emailcampaign.Campaign) {
	if err := s.deps.Notifications.Resolve(ctx, c.WorkspaceID, testBatchEpisode(c.ID)); err != nil {
		s.logf("could not close the test batch notice of %s: %v", c.ID, err)
	}
}
