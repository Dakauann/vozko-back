package attendance_repository

import (
	"strings"
	"time"

	"gorm.io/gorm"

	"vozko/domain/attendance"
)

func (r *repository) GetTrend(
	workspaceID string,
	filter attendance.OverviewFilter,
	buckets int,
	loc *time.Location,
) (attendance.TrendResult, error) {
	out := attendance.TrendResult{Buckets: []attendance.TrendBucketRow{}}
	if strings.TrimSpace(workspaceID) == "" {
		return out, nil
	}
	if loc == nil {
		loc = time.UTC
	}
	count := attendance.ClampTrendBuckets(buckets)

	anchor := time.Now().In(loc)
	if filter.DateTo != nil {
		anchor = filter.DateTo.In(loc)
	}
	currentStart := time.Date(anchor.Year(), anchor.Month(), 1, 0, 0, 0, 0, loc)
	from := currentStart.AddDate(0, -(count - 1), 0)
	to := currentStart.AddDate(0, 1, 0)

	sources := selectedChannelSources(filter.Channel)
	if len(sources) == 0 {
		return out, nil
	}

	query := trendQuery(workspaceID, filter, sources, from, to, loc)
	if query.empty() {
		return out, nil
	}

	type bucketRow struct {
		Bucket   time.Time `gorm:"column:bucket"`
		Engaged  int64     `gorm:"column:engaged"`
		Finished int64     `gorm:"column:finished"`
		Created  int64     `gorm:"column:created"`
		Pending  int64     `gorm:"column:pending"`
	}

	sql, args := query.build()
	var rows []bucketRow
	if err := r.db.Raw(sql, args...).Scan(&rows).Error; err != nil {
		return out, err
	}

	unbucketed, err := trendUnbucketed(r.db, workspaceID, filter, sources, from, to)
	if err != nil {
		return out, err
	}
	out.Unbucketed = unbucketed

	byBucket := make(map[string]attendance.TrendBucketRow, len(rows))
	for _, row := range rows {
		key := row.Bucket.Format(attendance.TrendBucketLayout)
		byBucket[key] = attendance.TrendBucketRow{
			Bucket:   key,
			Engaged:  row.Engaged,
			Finished: row.Finished,
			Created:  row.Created,
			Pending:  row.Pending,
		}
	}

	for i := 0; i < count; i++ {
		key := from.AddDate(0, i, 0).Format(attendance.TrendBucketLayout)
		if existing, found := byBucket[key]; found {
			out.Buckets = append(out.Buckets, existing)
			continue
		}
		out.Buckets = append(out.Buckets, attendance.TrendBucketRow{Bucket: key})
	}
	return out, nil
}

func trendQuery(
	workspaceID string,
	filter attendance.OverviewFilter,
	sources []channelSource,
	from, to time.Time,
	loc *time.Location,
) *sqlQuery {
	union := trendUnion(workspaceID, filter, sources, from, to)
	if union.empty() {
		return newSQLQuery()
	}

	query := newSQLQuery()
	query.add("WITH scoped AS (")
	query.addQuery(union)
	query.add(`)
		SELECT date_trunc('month', bucket_at AT TIME ZONE ?) AS bucket,
			COUNT(*) FILTER (WHERE engaged AND kind = 'created')::bigint AS engaged,
			COUNT(*) FILTER (WHERE engaged AND kind = 'finished')::bigint AS finished,
			COUNT(*) FILTER (WHERE kind = 'created')::bigint AS created,
			COUNT(*) FILTER (WHERE engaged AND kind = 'created' AND status_bucket <> 'finished')::bigint AS pending
		FROM scoped
		GROUP BY 1
		ORDER BY 1
	`, loc.String())
	return query
}

func trendUnion(
	workspaceID string,
	filter attendance.OverviewFilter,
	sources []channelSource,
	from, to time.Time,
) *sqlQuery {
	branches := make([]*sqlQuery, 0, len(sources)*2)

	for _, src := range sources {
		joins := trendJoins(src)
		engaged := trendEngagedPredicate(src)

		created := newSQLQuery()
		created.add(`
			SELECT `+src.EntryAlias+`.created_at AS bucket_at,
				'created'::text AS kind,
				`+src.statusBucket()+` AS status_bucket,
				`+engaged+` AS engaged
			`+joins+`
			WHERE `+src.WorkspaceColumn+` = ?
			  AND `+src.EntryAlias+`.deleted_at IS NULL
			  AND `+src.EntryAlias+`.created_at >= ?
			  AND `+src.EntryAlias+`.created_at < ?
		`, workspaceID, from, to)
		created.addQuery(trendScopeClause(src, filter))
		branches = append(branches, created)

		if src.ClosedAtColumn == "" || src.StatusColumn == "" {
			continue
		}
		finished := newSQLQuery()
		finished.add(`
			SELECT `+src.ClosedAtColumn+` AS bucket_at,
				'finished'::text AS kind,
				`+src.statusBucket()+` AS status_bucket,
				`+engaged+` AS engaged
			`+joins+`
			WHERE `+src.WorkspaceColumn+` = ?
			  AND `+src.EntryAlias+`.deleted_at IS NULL
			  AND `+src.StatusColumn+` = 'finished'
			  AND `+src.ClosedAtColumn+` >= ?
			  AND `+src.ClosedAtColumn+` < ?
		`, workspaceID, from, to)
		finished.addQuery(trendScopeClause(src, filter))
		branches = append(branches, finished)
	}

	if len(branches) == 0 {
		return newSQLQuery()
	}
	return joinQueries(" UNION ALL ", branches)
}

func trendUnbucketed(
	db *gorm.DB,
	workspaceID string,
	filter attendance.OverviewFilter,
	sources []channelSource,
	from, to time.Time,
) (int64, error) {
	query := trendUnbucketedQuery(workspaceID, filter, sources, from, to)
	if query.empty() {
		return 0, nil
	}

	sql, args := query.build()
	var total int64
	if err := db.Raw(sql, args...).Scan(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

func trendUnbucketedQuery(
	workspaceID string,
	filter attendance.OverviewFilter,
	sources []channelSource,
	from, to time.Time,
) *sqlQuery {
	branches := make([]*sqlQuery, 0, len(sources))

	for _, src := range sources {
		if src.ClosedAtColumn == "" || src.StatusColumn == "" {
			continue
		}
		branch := newSQLQuery()
		branch.add(`
			SELECT COUNT(*)::bigint AS missing
			`+trendJoins(src)+`
			WHERE `+src.WorkspaceColumn+` = ?
			  AND `+src.EntryAlias+`.deleted_at IS NULL
			  AND `+src.StatusColumn+` = 'finished'
			  AND `+src.ClosedAtColumn+` IS NULL
			  AND `+src.EntryAlias+`.created_at >= ?
			  AND `+src.EntryAlias+`.created_at < ?
		`, workspaceID, from, to)
		branch.addQuery(trendScopeClause(src, filter))
		branches = append(branches, branch)
	}
	if len(branches) == 0 {
		return newSQLQuery()
	}

	query := newSQLQuery()
	query.add("SELECT COALESCE(SUM(missing), 0)::bigint FROM (")
	query.addQuery(joinQueries(" UNION ALL ", branches))
	query.add(") counts")
	return query
}

func trendJoins(src channelSource) string {
	return `FROM ` + src.EntryTable + `
			JOIN ` + src.ContainerTable + `
				ON ` + src.ContainerJoin + ` AND ` + src.ContainerAlias + `.deleted_at IS NULL
			LEFT JOIN inbox_assignments ia
				ON ia.entry_id = ` + src.EntryAlias + `.id AND ia.entry_type = '` + string(src.EntryType) + `'`
}

func trendEngagedPredicate(src channelSource) string {
	return `EXISTS (
				SELECT 1 FROM conversation_messages cm
				WHERE cm.entry_id = ` + src.EntryAlias + `.id
				  AND cm.entry_type = '` + string(src.EntryType) + `'
				  AND cm.deleted_at IS NULL
			)`
}

func trendScopeClause(src channelSource, filter attendance.OverviewFilter) *sqlQuery {
	query := newSQLQuery()
	if filter.CampaignID != "" {
		if filter.CampaignType == "" || filter.CampaignType == string(src.EntryType) {
			query.add(" AND "+src.ContainerIDColumn+" = ?", filter.CampaignID)
		} else {
			query.add(" AND FALSE")
		}
	}
	if filter.DepartmentID != "" {
		query.add(" AND "+src.DepartmentColumn+" = ?", filter.DepartmentID)
	}
	if filter.MemberID != "" {
		query.add(" AND ia.assigned_user_id = ?", filter.MemberID)
	}
	return query
}
