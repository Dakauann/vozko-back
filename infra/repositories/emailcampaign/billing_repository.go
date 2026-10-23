package emailcampaign

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"gorm.io/gorm"

	domain "vozko/domain/emailcampaign"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

const openBatchSQL = `INSERT INTO email_campaign_charge_batches
	(campaign_id, batch_no, units, unit_micros, unit_cost_micros, total_micros, refunded_units, created_at)
SELECT CAST(@campaign AS uuid), CAST(@no AS int), CAST(@units AS bigint), CAST(@unit AS bigint), CAST(@cost AS bigint), CAST(@total AS bigint), 0, CAST(@at AS timestamptz)
WHERE NOT EXISTS (SELECT 1 FROM email_campaign_charge_batches WHERE campaign_id = @campaign AND charged_at IS NULL)
	AND COALESCE((SELECT max(batch_no) FROM email_campaign_charge_batches WHERE campaign_id = @campaign), 0) = CAST(@previous AS int)
	AND EXISTS (SELECT 1 FROM email_campaigns WHERE id = @campaign AND status IN (@running, @held) AND settled_at IS NULL)
ON CONFLICT (campaign_id, batch_no) DO NOTHING`

const markChargedSQL = `WITH charged AS (
	UPDATE email_campaign_charge_batches SET charged_at = @at
	WHERE campaign_id = @campaign AND batch_no = @no AND charged_at IS NULL
	RETURNING units
), paid AS (
	UPDATE email_campaigns c SET paid_units = c.paid_units + charged.units
	FROM charged WHERE c.id = @campaign
	RETURNING c.id
)
SELECT count(*) FROM paid`

const claimSlotSQL = `WITH target AS (
	SELECT r.id, r.slot_claimed_at IS NOT NULL AS held
	FROM email_campaign_recipients r
	JOIN email_campaigns c ON c.id = r.campaign_id AND c.status IN (@running, @held)
	WHERE r.id = @recipient AND r.campaign_id = @campaign AND r.status = @pending
	FOR UPDATE OF r
), taken AS (
	UPDATE email_campaigns c SET used_units = c.used_units + 1
	FROM target
	WHERE c.id = @campaign AND NOT target.held AND c.used_units < c.paid_units AND c.status IN (@running, @held)
	RETURNING c.id
), stamped AS (
	UPDATE email_campaign_recipients r SET slot_claimed_at = @at
	FROM target
	WHERE r.id = target.id AND (target.held OR EXISTS (SELECT 1 FROM taken))
	RETURNING r.id
)
SELECT count(*) FROM stamped`

const releaseSlotSQL = `WITH target AS (
	SELECT id, campaign_id FROM email_campaign_recipients
	WHERE id = @recipient AND campaign_id = @campaign AND status = @pending AND slot_claimed_at IS NOT NULL
	FOR UPDATE
), cleared AS (
	UPDATE email_campaign_recipients r SET slot_claimed_at = NULL
	FROM target WHERE r.id = target.id
	RETURNING target.campaign_id
), released AS (
	UPDATE email_campaigns c SET used_units = c.used_units - 1
	FROM cleared WHERE c.id = cleared.campaign_id AND c.used_units > 0
	RETURNING c.id
)
SELECT count(*) FROM cleared`

type BillingRepository struct {
	db  *gorm.DB
	now func() time.Time
}

func NewBillingRepository(db *gorm.DB) *BillingRepository {
	return &BillingRepository{db: db, now: func() time.Time { return time.Now().UTC() }}
}

var _ domain.BillingStore = (*BillingRepository)(nil)

func sendingArgs(values map[string]any) map[string]any {
	values["running"], values["held"] = string(domain.StatusRunning), string(domain.StatusHeld)
	return values
}

func (r *BillingRepository) count(ctx context.Context, query string, values map[string]any) (int64, error) {
	var n int64
	if err := r.db.WithContext(ctx).Raw(query, values).Scan(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

func (r *BillingRepository) BillingState(ctx context.Context, campaignID string) (domain.BillingState, error) {
	if len(database.UUIDArray([]string{campaignID})) == 0 {
		return domain.BillingState{}, domain.ErrCampaignNotFound
	}
	var state domain.BillingState
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var counters struct {
			PaidUnits int64
			UsedUnits int64
		}
		if err := tx.Model(&schema.EmailCampaign{}).Select("paid_units, used_units").Where("id = ?", campaignID).Take(&counters).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrCampaignNotFound
			}
			return err
		}
		var rows []schema.EmailCampaignChargeBatch
		if err := tx.Where("campaign_id = ?", campaignID).Order("batch_no").Find(&rows).Error; err != nil {
			return err
		}
		state.Slots = domain.SlotCounters{Paid: counters.PaidUnits, Used: counters.UsedUnits}
		state.Batches = make([]domain.ChargeBatch, 0, len(rows))
		for _, row := range rows {
			state.Batches = append(state.Batches, batchOf(row))
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return state, err
}

func batchOf(row schema.EmailCampaignChargeBatch) domain.ChargeBatch {
	return domain.ChargeBatch{
		No: row.BatchNo, Units: row.Units, UnitMicros: row.UnitMicros, UnitCostMicros: row.UnitCostMicros, TotalMicros: row.TotalMicros,
		RefundedUnits: row.RefundedUnits, ChargedAt: utc(row.ChargedAt), RefundedAt: utc(row.RefundedAt),
	}
}

func (r *BillingRepository) OpenBatch(ctx context.Context, campaignID string, b domain.ChargeBatch) (domain.ChargeBatch, error) {
	err := r.db.WithContext(ctx).Exec(openBatchSQL, sendingArgs(map[string]any{
		"campaign": campaignID, "no": b.No, "previous": b.No - 1, "units": b.Units, "unit": b.UnitMicros,
		"cost": b.UnitCostMicros, "total": b.TotalMicros, "at": r.now(),
	})).Error
	if err != nil {
		return domain.ChargeBatch{}, err
	}
	var row schema.EmailCampaignChargeBatch
	err = r.db.WithContext(ctx).Where("campaign_id = ? AND charged_at IS NULL", campaignID).Order("batch_no").Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.ChargeBatch{}, domain.ErrNoOpenBatch
	}
	if err != nil {
		return domain.ChargeBatch{}, err
	}
	return batchOf(row), nil
}

func (r *BillingRepository) MarkBatchCharged(ctx context.Context, campaignID string, batchNo int, at time.Time) (bool, error) {
	n, err := r.count(ctx, markChargedSQL, map[string]any{"campaign": campaignID, "no": batchNo, "at": at.UTC()})
	return n > 0, err
}

func (r *BillingRepository) MarkBatchRefunded(ctx context.Context, campaignID string, batchNo int, units int64, at time.Time) (bool, error) {
	result := r.db.WithContext(ctx).Model(&schema.EmailCampaignChargeBatch{}).
		Where("campaign_id = ? AND batch_no = ? AND refunded_at IS NULL AND charged_at IS NOT NULL AND units >= ?", campaignID, batchNo, units).
		Updates(map[string]any{"refunded_units": units, "refunded_at": at.UTC()})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (r *BillingRepository) ClaimSlot(ctx context.Context, campaignID, recipientID string, at time.Time) (bool, error) {
	if len(database.UUIDArray([]string{campaignID, recipientID})) != 2 {
		return false, nil
	}
	n, err := r.count(ctx, claimSlotSQL, sendingArgs(map[string]any{
		"campaign": campaignID, "recipient": recipientID, "pending": string(domain.RecipientPending), "at": at.UTC(),
	}))
	return n > 0, err
}

func (r *BillingRepository) ReleaseSlot(ctx context.Context, campaignID, recipientID string) (bool, error) {
	if len(database.UUIDArray([]string{campaignID, recipientID})) != 2 {
		return false, nil
	}
	n, err := r.count(ctx, releaseSlotSQL, map[string]any{
		"campaign": campaignID, "recipient": recipientID, "pending": string(domain.RecipientPending),
	})
	return n > 0, err
}

func (r *BillingRepository) FreshClaims(ctx context.Context, campaignID string, since time.Time) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&schema.EmailCampaignRecipient{}).
		Where("campaign_id = ? AND status = ? AND slot_claimed_at > ?", campaignID, string(domain.RecipientPending), since.UTC()).
		Count(&n).Error
	return n, err
}

func (r *BillingRepository) ListUnsettled(ctx context.Context, limit int) ([]domain.Campaign, error) {
	var rows []schema.EmailCampaign
	err := r.db.WithContext(ctx).
		Where("status IN ? AND settled_at IS NULL", []string{string(domain.StatusStopped), string(domain.StatusCompleted)}).
		Where("EXISTS (SELECT 1 FROM email_campaign_charge_batches b WHERE b.campaign_id = email_campaigns.id)").
		Order("updated_at").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return toDomainAll(rows)
}
