package advertising_repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"vozko/domain/advertising"
	attendance_repository "vozko/infra/repositories/attendance"
)

type attributionRepository struct {
	db *gorm.DB
}

func NewAttributionRepository(db *gorm.DB) advertising.AttributionRepository {
	return &attributionRepository{db: db}
}

func scopedOriginsSQL(selectList string) string {
	return `SELECT ` + selectList + ` FROM conversation_ad_origins cao
		JOIN (` + attendance_repository.EntryWorkspaceUnion() + `) e ON e.entry_id = cao.entry_id AND e.entry_type = cao.entry_type AND e.workspace_id = ?
		WHERE cao.ad_id IN ? AND cao.arrived_at >= ? AND cao.arrived_at < ?
		AND EXISTS (SELECT 1 FROM ad_objects ao WHERE ao.meta_id = cao.ad_id AND ao.workspace_id = ?)`
}

func scopedOriginsArgs(workspaceID string, adMetaIDs []string, from, to time.Time) []any {
	return []any{workspaceID, adMetaIDs, from, to, workspaceID}
}

var attributionByAdSQL = `WITH origins AS (` + scopedOriginsSQL("cao.ad_id, cao.entry_id, cao.entry_type") + `
),
linked AS (
	SELECT org.ad_id, org.entry_id, org.entry_type, o.id AS opportunity_id, o.status, o.value_cents,
		COALESCE(NULLIF(o.currency, ''), 'BRL') AS currency
	FROM origins org
	JOIN opportunity_conversations oc ON oc.entry_id = org.entry_id AND oc.entry_type = org.entry_type
	JOIN opportunities o ON o.id = oc.opportunity_id AND o.deleted_at IS NULL AND o.workspace_id = ?
),
leads AS (
	SELECT DISTINCT ad_id, entry_id, entry_type FROM linked
),
won AS (
	SELECT ad_id,
		COUNT(*)::bigint AS won_deals,
		CASE WHEN COUNT(DISTINCT currency) > 1 THEN 0 ELSE SUM(value_cents) END::bigint AS revenue,
		CASE WHEN COUNT(DISTINCT currency) > 1 THEN 'MIXED' ELSE MIN(currency) END AS revenue_currency
	FROM (SELECT DISTINCT ad_id, opportunity_id, value_cents, currency FROM linked WHERE status = 'won') deals
	GROUP BY ad_id
)
SELECT org.ad_id AS ad_meta_id,
	COUNT(*)::bigint AS conversations,
	COUNT(ld.entry_id)::bigint AS leads,
	COALESCE(MAX(w.won_deals), 0)::bigint AS won_deals,
	COALESCE(MAX(w.revenue), 0)::bigint AS revenue,
	COALESCE(MAX(w.revenue_currency), '') AS revenue_currency
FROM origins org
LEFT JOIN leads ld ON ld.ad_id = org.ad_id AND ld.entry_id = org.entry_id AND ld.entry_type = org.entry_type
LEFT JOIN won w ON w.ad_id = org.ad_id
GROUP BY org.ad_id
ORDER BY org.ad_id`

func (r *attributionRepository) ByAd(ctx context.Context, workspaceID string, adMetaIDs []string, from, to time.Time) ([]advertising.Attribution, error) {
	if blank(workspaceID) {
		return nil, advertising.ErrWorkspaceRequired
	}
	if len(adMetaIDs) == 0 {
		return []advertising.Attribution{}, nil
	}
	type attributionRow struct {
		AdMetaID        string `gorm:"column:ad_meta_id"`
		Conversations   int64  `gorm:"column:conversations"`
		Leads           int64  `gorm:"column:leads"`
		WonDeals        int64  `gorm:"column:won_deals"`
		Revenue         int64  `gorm:"column:revenue"`
		RevenueCurrency string `gorm:"column:revenue_currency"`
	}
	var rows []attributionRow
	args := append(scopedOriginsArgs(workspaceID, adMetaIDs, from, to), workspaceID)
	if err := r.db.WithContext(ctx).Raw(attributionByAdSQL, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]advertising.Attribution, 0, len(rows))
	for _, row := range rows {
		out = append(out, advertising.Attribution{
			AdMetaID:        row.AdMetaID,
			Conversations:   row.Conversations,
			Leads:           row.Leads,
			WonDeals:        row.WonDeals,
			Revenue:         row.Revenue,
			RevenueCurrency: row.RevenueCurrency,
		})
	}
	return out, nil
}

func (r *attributionRepository) Conversations(ctx context.Context, workspaceID, adMetaID string, from, to time.Time) (int64, error) {
	if blank(workspaceID) {
		return 0, advertising.ErrWorkspaceRequired
	}
	var count int64
	err := r.db.WithContext(ctx).
		Raw(scopedOriginsSQL("COUNT(*)::bigint"), scopedOriginsArgs(workspaceID, []string{adMetaID}, from, to)...).
		Scan(&count).Error
	return count, err
}
