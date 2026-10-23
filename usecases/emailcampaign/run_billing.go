package emailcampaign_usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"vozko/domain/cache"
	"vozko/domain/emailcampaign"
	balance_usecase "vozko/usecases/balance"
)

const (
	billingLockWait = 20 * time.Second
	claimStaleAfter = 5 * time.Minute
)

var (
	ErrNotSending = errors.New("emailcampaign: the campaign is not sending, so no email slot is taken")
	ErrNoSlot     = errors.New("emailcampaign: no paid email slot is free for this recipient")
)

type BatchBilling interface {
	QuoteRun(ctx context.Context, workspaceID string, recipients int64) (balance_usecase.EmailQuote, error)
	ChargeBatch(ctx context.Context, workspaceID, campaignID string, batchNo int, quote balance_usecase.EmailQuote) error
	BatchCharged(ctx context.Context, campaignID string, batchNo int) (bool, error)
	RefundBatch(ctx context.Context, workspaceID, campaignID string, batchNo int, quote balance_usecase.EmailQuote, units int64) error
}

type BillingCampaigns interface {
	GetByID(ctx context.Context, id string) (*emailcampaign.Campaign, error)
	SetSettlement(ctx context.Context, id string, s emailcampaign.RunSettlement) (bool, error)
}

type RecipientCounter interface {
	Counts(ctx context.Context, campaignID string) (emailcampaign.RecipientCounts, error)
}

type RunBillingDeps struct {
	Campaigns  BillingCampaigns
	Store      emailcampaign.BillingStore
	Recipients RecipientCounter
	Billing    BatchBilling
	Locks      cache.Locker
	Now        func() time.Time
	Logf       func(format string, args ...any)
}

type RunBilling struct {
	d RunBillingDeps
}

func NewRunBilling(d RunBillingDeps) *RunBilling {
	return &RunBilling{d: d}
}

func billingLockKey(campaignID string) string { return "email_campaign_billing:" + campaignID }

func batchQuote(b emailcampaign.ChargeBatch) balance_usecase.EmailQuote {
	return balance_usecase.EmailQuote{Units: b.Units, UnitMicros: b.UnitMicros, UnitCostMicros: b.UnitCostMicros, TotalMicros: b.TotalMicros}
}

func (b *RunBilling) Claim(ctx context.Context, c emailcampaign.Campaign, recipientID string) error {
	claimed, err := b.d.Store.ClaimSlot(ctx, c.ID, recipientID, b.d.Now())
	if err != nil || claimed {
		return err
	}
	return b.locked(ctx, c.ID, func() error {
		claimed, err := b.d.Store.ClaimSlot(ctx, c.ID, recipientID, b.d.Now())
		if err != nil || claimed {
			return err
		}
		current, err := b.d.Campaigns.GetByID(ctx, c.ID)
		if err != nil {
			return err
		}
		if !current.Status.Sending() || current.Settlement != nil {
			return ErrNotSending
		}
		if err := b.chargeNextBatch(ctx, *current); err != nil {
			return err
		}
		claimed, err = b.d.Store.ClaimSlot(ctx, c.ID, recipientID, b.d.Now())
		if err != nil {
			return err
		}
		if !claimed {
			return ErrNoSlot
		}
		return nil
	})
}

func (b *RunBilling) Release(ctx context.Context, c emailcampaign.Campaign, recipientID string) error {
	_, err := b.d.Store.ReleaseSlot(ctx, c.ID, recipientID)
	return err
}

func (b *RunBilling) State(ctx context.Context, campaignID string) (emailcampaign.BillingState, error) {
	return b.d.Store.BillingState(ctx, campaignID)
}

func (b *RunBilling) chargeNextBatch(ctx context.Context, c emailcampaign.Campaign) error {
	state, err := b.d.Store.BillingState(ctx, c.ID)
	if err != nil {
		return err
	}
	batch := state.OpenBatch()
	if batch == nil {
		opened, err := b.openBatch(ctx, c, state)
		if errors.Is(err, emailcampaign.ErrNoOpenBatch) {
			return nil
		}
		if err != nil {
			return err
		}
		batch = &opened
	}
	if err := b.d.Billing.ChargeBatch(ctx, c.WorkspaceID, c.ID, batch.No, batchQuote(*batch)); err != nil {
		return err
	}
	_, err = b.d.Store.MarkBatchCharged(ctx, c.ID, batch.No, b.d.Now())
	return err
}

func (b *RunBilling) openBatch(ctx context.Context, c emailcampaign.Campaign, state emailcampaign.BillingState) (emailcampaign.ChargeBatch, error) {
	counts, err := b.d.Recipients.Counts(ctx, c.ID)
	if err != nil {
		return emailcampaign.ChargeBatch{}, err
	}
	units := emailcampaign.NextBatchUnits(counts.Pending, counts.Sent, state.Slots.Paid)
	if units == 0 {
		return emailcampaign.ChargeBatch{}, ErrNoSlot
	}
	quote, err := b.d.Billing.QuoteRun(ctx, c.WorkspaceID, units)
	if err != nil {
		return emailcampaign.ChargeBatch{}, err
	}
	next, err := emailcampaign.NewChargeBatch(state.NextBatchNo(), units, quote.UnitMicros, quote.UnitCostMicros)
	if err != nil {
		return emailcampaign.ChargeBatch{}, err
	}
	return b.d.Store.OpenBatch(ctx, c.ID, next)
}

func (b *RunBilling) Settle(ctx context.Context, c emailcampaign.Campaign) (bool, error) {
	if c.Settlement != nil {
		return true, nil
	}
	settled := false
	err := b.locked(ctx, c.ID, func() error {
		current, err := b.d.Campaigns.GetByID(ctx, c.ID)
		if err != nil {
			return err
		}
		if current.Settlement != nil {
			settled = true
			return nil
		}
		inFlight, err := b.d.Store.FreshClaims(ctx, c.ID, b.d.Now().Add(-claimStaleAfter))
		if err != nil || inFlight > 0 {
			return err
		}
		state, err := b.reconciledState(ctx, c.ID)
		if err != nil {
			return err
		}
		if len(state.Batches) == 0 {
			settled = true
			return nil
		}
		refunds := emailcampaign.PlanRefunds(state.Batches, state.Slots.Paid-state.Slots.Used)
		if err := b.refund(ctx, *current, state, refunds); err != nil {
			return err
		}
		units, micros := emailcampaign.RefundTotals(refunds)
		if _, err := b.d.Campaigns.SetSettlement(ctx, c.ID, emailcampaign.RunSettlement{RefundedUnits: units, RefundedMicros: micros, SettledAt: b.d.Now()}); err != nil {
			return err
		}
		settled = true
		return nil
	})
	return settled, err
}

func (b *RunBilling) reconciledState(ctx context.Context, campaignID string) (emailcampaign.BillingState, error) {
	state, err := b.d.Store.BillingState(ctx, campaignID)
	if err != nil {
		return state, err
	}
	open := state.OpenBatch()
	if open == nil {
		return state, nil
	}
	debited, err := b.d.Billing.BatchCharged(ctx, campaignID, open.No)
	if err != nil || !debited {
		return state, err
	}
	if _, err := b.d.Store.MarkBatchCharged(ctx, campaignID, open.No, b.d.Now()); err != nil {
		return state, err
	}
	return b.d.Store.BillingState(ctx, campaignID)
}

func (b *RunBilling) refund(ctx context.Context, c emailcampaign.Campaign, state emailcampaign.BillingState, refunds []emailcampaign.BatchRefund) error {
	byNo := make(map[int]emailcampaign.ChargeBatch, len(state.Batches))
	for _, batch := range state.Batches {
		byNo[batch.No] = batch
	}
	for _, r := range refunds {
		batch := byNo[r.BatchNo]
		if batch.Refunded() {
			continue
		}
		if err := b.d.Billing.RefundBatch(ctx, c.WorkspaceID, c.ID, r.BatchNo, batchQuote(batch), r.Units); err != nil {
			return fmt.Errorf("refund batch %d of %s: %w", r.BatchNo, c.ID, err)
		}
		if _, err := b.d.Store.MarkBatchRefunded(ctx, c.ID, r.BatchNo, r.Units, b.d.Now()); err != nil {
			return err
		}
	}
	return nil
}

func (b *RunBilling) locked(ctx context.Context, campaignID string, work func() error) error {
	wait, cancel := context.WithTimeout(ctx, billingLockWait)
	defer cancel()
	release, err := b.d.Locks.Acquire(wait, billingLockKey(campaignID))
	if err != nil {
		return fmt.Errorf("emailcampaign: the billing lock of %s: %w", campaignID, err)
	}
	defer func() {
		if _, err := release(); err != nil {
			b.d.Logf("could not release the billing lock of %s: %v", campaignID, err)
		}
	}()
	return work()
}
