package attendance_repository

import (
	"strings"
	"time"

	"vozko/domain/attendance"
)

func (r *repository) GetRevenue(workspaceID string, from, to time.Time) ([]attendance.RevenueTally, int64, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return []attendance.RevenueTally{}, 0, nil
	}

	type tallyRow struct {
		Currency   string `gorm:"column:currency"`
		OwnerID    string `gorm:"column:owner_id"`
		WonCount   int64  `gorm:"column:won_count"`
		ValueCents int64  `gorm:"column:value_cents"`
	}
	sql, args := revenueTalliesQuery(workspaceID, from, to).build()
	var rows []tallyRow
	if err := r.db.Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, 0, err
	}

	unattributedSQL, unattributedArgs := revenueUnattributedQuery(workspaceID, from, to).build()
	var unattributed int64
	if err := r.db.Raw(unattributedSQL, unattributedArgs...).Scan(&unattributed).Error; err != nil {
		return nil, 0, err
	}

	out := make([]attendance.RevenueTally, 0, len(rows))
	for _, row := range rows {
		out = append(out, attendance.RevenueTally{
			Currency:   row.Currency,
			OwnerID:    row.OwnerID,
			WonCount:   row.WonCount,
			ValueCents: row.ValueCents,
		})
	}
	return out, unattributed, nil
}

func (r *repository) GetRevenueByMonth(
	workspaceID string,
	from, to time.Time,
	loc *time.Location,
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
	sql, args := revenueByMonthQuery(workspaceID, from, to, loc).build()
	var rows []monthRow
	if err := r.db.Raw(sql, args...).Scan(&rows).Error; err != nil {
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

func revenueTalliesQuery(workspaceID string, from, to time.Time) *sqlQuery {
	return newSQLQuery().add(`
		SELECT COALESCE(NULLIF(currency, ''), 'BRL') AS currency,
			COALESCE(owner_id::text, '') AS owner_id,
			COUNT(*)::bigint AS won_count,
			COALESCE(SUM(value_cents), 0)::bigint AS value_cents
		FROM opportunities
		WHERE workspace_id = ?
		  AND status = 'won'
		  AND deleted_at IS NULL
		  AND close_date IS NOT NULL
		  AND close_date >= ?
		  AND close_date < ?
		GROUP BY 1, 2
	`, workspaceID, from, to)
}

func revenueUnattributedQuery(workspaceID string, from, to time.Time) *sqlQuery {
	return newSQLQuery().add(`
		SELECT COUNT(*)::bigint
		FROM opportunities
		WHERE workspace_id = ?
		  AND status = 'won'
		  AND deleted_at IS NULL
		  AND close_date IS NULL
		  AND updated_at >= ?
		  AND updated_at < ?
	`, workspaceID, from, to)
}

func revenueByMonthQuery(workspaceID string, from, to time.Time, loc *time.Location) *sqlQuery {
	return newSQLQuery().add(`
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
		GROUP BY 1, 2
		ORDER BY 1
	`, loc.String(), workspaceID, from, to)
}
