package carriercampaign_repository

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"vozko/domain/balance"
	"vozko/domain/carriermessaging"
	ccuc "vozko/usecases/carriercampaign"
)

type WhatsAppSpendReader struct {
	db          *gorm.DB
	attribution attribution
}

var _ ccuc.WhatsAppSpend = (*WhatsAppSpendReader)(nil)

func NewWhatsAppSpendReader(db *gorm.DB) *WhatsAppSpendReader {
	return &WhatsAppSpendReader{db: db, attribution: attribution{db: db}}
}

const whatsAppCampaignSpendSQL = `
SELECT COALESCE(SUM(CASE WHEN bt.type = 'debit' THEN bt.amount ELSE -bt.amount END), 0) AS spend
FROM balance_transactions bt
WHERE bt.workspace_id = ? AND bt.service_type = ? AND bt.created_at >= ? AND bt.created_at < ?
	AND ((bt.type = 'debit' AND bt.is_refund = false) OR (bt.type = 'credit' AND bt.is_refund = true))
	AND EXISTS (
		SELECT 1 FROM whatsapp_campaign_entries e
		JOIN whatsapp_campaigns wc ON wc.id = e.campaign_id
		WHERE wc.workspace_id = bt.workspace_id
			AND e.id::text = split_part(regexp_replace(bt.reference_id, '^refund:', ''), ':', 1)
	)`

func (r *WhatsAppSpendReader) WhatsAppCampaignSpend(ctx context.Context, workspaceID string, from, to time.Time, attributionDays int) (int64, int64, error) {
	if attributionDays < 1 || attributionDays > carriermessaging.MaxAttributionDays {
		return 0, 0, fmt.Errorf("%w: attribution days %d", errOverviewQuery, attributionDays)
	}
	if !from.Before(to) {
		return 0, 0, fmt.Errorf("%w: the range must start before it ends", errOverviewQuery)
	}
	var spend int64
	if err := r.db.WithContext(ctx).Raw(whatsAppCampaignSpendSQL, workspaceID, string(balance.ServiceWhatsAppCampaign), from, to).Scan(&spend).Error; err != nil {
		return 0, 0, err
	}
	deals, err := r.attribution.dealsWonBetween(ctx, workspaceID, from, to)
	if err != nil {
		return 0, 0, err
	}
	wins, err := r.attribution.lastTouches(ctx, workspaceID, deals, attributionDays)
	if err != nil {
		return 0, 0, err
	}
	var conversions int64
	for _, w := range wins {
		if w.touch.Channel == whatsAppChannel {
			conversions++
		}
	}
	return spend, conversions, nil
}
