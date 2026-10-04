package advertising_repository

import (
	"context"
	"strings"
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

func groupedOriginsSQL(groups int) string {
	rows := make([]string, groups)
	for i := range rows {
		rows[i] = "(CAST(? AS text), CAST(? AS text))"
	}
	return `SELECT g.group_key, cao.entry_id, cao.entry_type FROM conversation_ad_origins cao
		JOIN (VALUES ` + strings.Join(rows, ", ") + `) AS g(ad_id, group_key) ON g.ad_id = cao.ad_id
		JOIN (` + attendance_repository.EntryWorkspaceUnion() + `) e ON e.entry_id = cao.entry_id AND e.entry_type = cao.entry_type AND e.workspace_id = ?
		WHERE cao.arrived_at >= ? AND cao.arrived_at < ?
		AND EXISTS (SELECT 1 FROM ad_objects ao WHERE ao.meta_id = cao.ad_id AND ao.workspace_id = ?)`
}

func attributionByGroupSQL(groups int) string {
	return `WITH origins AS (` + groupedOriginsSQL(groups) + `
),
linked AS (
	SELECT org.group_key, org.entry_id, org.entry_type, o.id AS opportunity_id, o.status, o.value_cents,
		COALESCE(NULLIF(o.currency, ''), 'BRL') AS currency
	FROM origins org
	JOIN opportunity_conversations oc ON oc.entry_id = org.entry_id AND oc.entry_type = org.entry_type
	JOIN opportunities o ON o.id = oc.opportunity_id AND o.deleted_at IS NULL AND o.workspace_id = ?
),
leads AS (
	SELECT DISTINCT group_key, entry_id, entry_type FROM linked
),
won AS (
	SELECT group_key,
		COUNT(*)::bigint AS won_deals,
		CASE WHEN COUNT(DISTINCT currency) > 1 THEN 0 ELSE SUM(value_cents) END::bigint AS revenue,
		CASE WHEN COUNT(DISTINCT currency) > 1 THEN 'MIXED' ELSE MIN(currency) END AS revenue_currency
	FROM (SELECT DISTINCT group_key, opportunity_id, value_cents, currency FROM linked WHERE status = 'won') deals
	GROUP BY group_key
)
SELECT org.group_key,
	COUNT(*)::bigint AS conversations,
	COUNT(ld.entry_id)::bigint AS leads,
	COALESCE(MAX(w.won_deals), 0)::bigint AS won_deals,
	COALESCE(MAX(w.revenue), 0)::bigint AS revenue,
	COALESCE(MAX(w.revenue_currency), '') AS revenue_currency
FROM origins org
LEFT JOIN leads ld ON ld.group_key = org.group_key AND ld.entry_id = org.entry_id AND ld.entry_type = org.entry_type
LEFT JOIN won w ON w.group_key = org.group_key
GROUP BY org.group_key
ORDER BY org.group_key`
}

func (r *attributionRepository) ByGroup(ctx context.Context, workspaceID string, groups []advertising.AdGroup, from, to time.Time) ([]advertising.Attribution, error) {
	if blank(workspaceID) {
		return nil, advertising.ErrWorkspaceRequired
	}
	if len(groups) == 0 {
		return []advertising.Attribution{}, nil
	}
	type attributionRow struct {
		GroupKey        string `gorm:"column:group_key"`
		Conversations   int64  `gorm:"column:conversations"`
		Leads           int64  `gorm:"column:leads"`
		WonDeals        int64  `gorm:"column:won_deals"`
		Revenue         int64  `gorm:"column:revenue"`
		RevenueCurrency string `gorm:"column:revenue_currency"`
	}
	args := make([]any, 0, 2*len(groups)+5)
	for _, g := range groups {
		args = append(args, g.AdMetaID, g.Key)
	}
	args = append(args, workspaceID, from, to, workspaceID, workspaceID)
	var rows []attributionRow
	if err := r.db.WithContext(ctx).Raw(attributionByGroupSQL(len(groups)), args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]advertising.Attribution, 0, len(rows))
	for _, row := range rows {
		out = append(out, advertising.Attribution{
			Key:             row.GroupKey,
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
