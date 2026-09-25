package attendance_repository

import (
	"context"
	"strings"
	"time"

	"vozko/domain/attendance"
)

var revenueOwnerSQL = actorIDSQL("owner_id", "owner_kind")

func (r *repository) GetRevenue(
	ctx context.Context,
	workspaceID string,
	from, to time.Time,
	scope attendance.RevenueScope,
) ([]attendance.RevenueTally, int64, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return []attendance.RevenueTally{}, 0, nil
	}

	type tallyRow struct {
		Currency        string `gorm:"column:currency"`
		OwnerID         string `gorm:"column:owner_id"`
		WonCount        int64  `gorm:"column:won_count"`
		ValueCents      int64  `gorm:"column:value_cents"`
		WonWithoutValue int64  `gorm:"column:won_without_value"`
	}
	sql, args := revenueTalliesQuery(workspaceID, from, to, scope).build()
	var rows []tallyRow
	if err := r.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, 0, err
	}

	unattributedSQL, unattributedArgs := revenueUnattributedQuery(workspaceID, from, to, scope).build()
	var unattributed int64
	if err := r.db.WithContext(ctx).Raw(unattributedSQL, unattributedArgs...).Scan(&unattributed).Error; err != nil {
		return nil, 0, err
	}

	out := make([]attendance.RevenueTally, 0, len(rows))
	for _, row := range rows {
		out = append(out, attendance.RevenueTally{
			Currency:        row.Currency,
			OwnerID:         row.OwnerID,
			WonCount:        row.WonCount,
			ValueCents:      row.ValueCents,
			WonWithoutValue: row.WonWithoutValue,
		})
	}
	return out, unattributed, nil
}

func (r *repository) GetRevenueByMonth(
	ctx context.Context,
	workspaceID string,
	from, to time.Time,
	loc *time.Location,
	ownerID string,
	scope attendance.RevenueScope,
) ([]attendance.RevenueMonthRow, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return []attendance.RevenueMonthRow{}, nil
	}
	if loc == nil {
		loc = time.UTC
	}

	type monthRow struct {
		Bucket     time.Time `gorm:"column:bucket"`
		Currency   string    `gorm:"column:currency"`
		ValueCents int64     `gorm:"column:value_cents"`
		WonCount   int64     `gorm:"column:won_count"`
	}
	sql, args := revenueByMonthQuery(workspaceID, from, to, loc, ownerID, scope).build()
	var rows []monthRow
	if err := r.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}

	out := make([]attendance.RevenueMonthRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, attendance.RevenueMonthRow{
			Bucket:     row.Bucket.Format(attendance.TrendBucketLayout),
			Currency:   row.Currency,
			ValueCents: row.ValueCents,
			WonCount:   row.WonCount,
		})
	}
	return out, nil
}

func revenueTalliesQuery(workspaceID string, from, to time.Time, scope attendance.RevenueScope) *sqlQuery {
	query := newSQLQuery().add(`
		SELECT COALESCE(NULLIF(currency, ''), 'BRL') AS currency,
			`+revenueOwnerSQL+` AS owner_id,
			COUNT(*)::bigint AS won_count,
			COALESCE(SUM(value_cents), 0)::bigint AS value_cents,
			COUNT(*) FILTER (WHERE value_cents = 0)::bigint AS won_without_value
		FROM opportunities
		WHERE workspace_id = ?
		  AND status = 'won'
		  AND deleted_at IS NULL
		  AND close_date IS NOT NULL
		  AND close_date >= ?
		  AND close_date < ?
	`, workspaceID, from, to)
	query.addQuery(revenueScopeClause(scope))
	return query.add(`
		GROUP BY 1, 2
	`)
}

func revenueUnattributedQuery(workspaceID string, from, to time.Time, scope attendance.RevenueScope) *sqlQuery {
	query := newSQLQuery().add(`
		SELECT COUNT(*)::bigint
		FROM opportunities
		WHERE workspace_id = ?
		  AND status = 'won'
		  AND deleted_at IS NULL
		  AND close_date IS NULL
		  AND updated_at >= ?
		  AND updated_at < ?
	`, workspaceID, from, to)
	return query.addQuery(revenueScopeClause(scope))
}

func revenueByMonthQuery(
	workspaceID string,
	from, to time.Time,
	loc *time.Location,
	ownerID string,
	scope attendance.RevenueScope,
) *sqlQuery {
	query := newSQLQuery().add(`
		SELECT date_trunc('month', close_date AT TIME ZONE ?) AS bucket,
			COALESCE(NULLIF(currency, ''), 'BRL') AS currency,
			COALESCE(SUM(value_cents), 0)::bigint AS value_cents,
			COUNT(*)::bigint AS won_count
		FROM opportunities
		WHERE workspace_id = ?
		  AND status = 'won'
		  AND deleted_at IS NULL
		  AND close_date IS NOT NULL
		  AND close_date >= ?
		  AND close_date < ?
	`, loc.String(), workspaceID, from, to)

	if strings.TrimSpace(ownerID) != "" {
		query.add(` AND `+revenueOwnerSQL+` = ?`, ownerID)
	}
	query.addQuery(revenueScopeClause(scope))

	return query.add(`
		GROUP BY 1, 2
		ORDER BY 1
	`)
}

func revenueScopeClause(scope attendance.RevenueScope) *sqlQuery {
	if scope.Empty() {
		return newSQLQuery()
	}
	branches := make([]*sqlQuery, 0, len(channelSources))
	for _, src := range selectedChannelSources(scope.Channel) {
		if scope.CampaignID != "" && scope.CampaignType != "" && scope.CampaignType != string(src.EntryType) {
			continue
		}
		branch := newSQLQuery().add(`
			SELECT 1 FROM opportunity_conversations oc
			JOIN ` + src.EntryTable + ` ON ` + src.EntryAlias + `.id = oc.entry_id
			JOIN ` + src.ContainerTable + ` ON ` + src.ContainerJoin + `
			WHERE oc.opportunity_id = opportunities.id
			  AND oc.entry_type = '` + string(src.EntryType) + `'`)
		if scope.CampaignID != "" {
			branch.add(` AND `+src.ContainerIDColumn+` = ?`, scope.CampaignID)
		}
		if scope.DepartmentID != "" {
			branch.add(` AND `+src.DepartmentColumn+` = ?`, scope.DepartmentID)
		}
		branches = append(branches, branch)
	}
	if len(branches) == 0 {
		return newSQLQuery().add(` AND FALSE`)
	}
	query := newSQLQuery().add(` AND EXISTS (`)
	query.addQuery(joinQueries(" UNION ALL ", branches))
	return query.add(`)`)
}
