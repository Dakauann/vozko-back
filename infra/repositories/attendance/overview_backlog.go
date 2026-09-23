package attendance_repository

import (
	"strings"
	"time"

	"gorm.io/gorm"

	"vozko/domain/attendance"
	"vozko/domain/lead_message_window"
)

const backlogPredicate = "total_msgs > 0 AND status_bucket <> 'finished'"

var backlogRecordFields = []string{"name", "age"}

type xrayCountRow struct {
	Key   string `gorm:"column:key"`
	Label string `gorm:"column:label"`
	Count int64  `gorm:"column:count"`
}

func overviewBacklogXrayTX(
	tx *gorm.DB,
	workspaceID, msgTmp string,
	filter attendance.OverviewFilter,
	now time.Time,
) (attendance.BacklogXray, error) {
	out := attendance.BacklogXray{
		Origin:             attendance.UnavailableXrayDimension(attendance.XrayDimensionOrigin, attendance.ReasonNoBacklog),
		Assignee:           attendance.UnavailableXrayDimension(attendance.XrayDimensionAssignee, attendance.ReasonNoBacklog),
		Age:                attendance.UnavailableXrayDimension(attendance.XrayDimensionAge, attendance.ReasonNoBacklog),
		Tenure:             attendance.UnavailableXrayDimension(attendance.XrayDimensionTenure, attendance.ReasonNoBacklog),
		Returning:          attendance.UnavailableXrayDimension(attendance.XrayDimensionReturning, attendance.ReasonNoBacklog),
		RecordCompleteness: attendance.BuildRecordCompleteness(nil, 0, 0),
		Reachability:       []attendance.Reachability{},
		Reason:             attendance.ReasonNoBacklog,
	}

	var total int64
	if err := tx.Raw(`SELECT COUNT(*)::bigint FROM ` + msgTmp + ` WHERE ` + backlogPredicate).Scan(&total).Error; err != nil {
		return out, err
	}
	out.Total = total
	if total == 0 {
		return out, nil
	}

	origin, err := backlogOriginTX(tx, msgTmp)
	if err != nil {
		return out, err
	}
	out.Origin = origin

	assignee, err := backlogAssigneeTX(tx, msgTmp)
	if err != nil {
		return out, err
	}
	out.Assignee = assignee

	age, err := backlogAgeTX(tx, msgTmp, now)
	if err != nil {
		return out, err
	}
	out.Age = age

	tenure, err := backlogTenureTX(tx, msgTmp, now)
	if err != nil {
		return out, err
	}
	out.Tenure = tenure

	returning, err := backlogReturningTX(tx, workspaceID, msgTmp, filter)
	if err != nil {
		return out, err
	}
	out.Returning = returning

	completeness, err := backlogRecordCompletenessTX(tx, msgTmp)
	if err != nil {
		return out, err
	}
	out.RecordCompleteness = completeness

	reach, err := backlogReachabilityTX(tx, msgTmp, filter, now)
	if err != nil {
		return out, err
	}
	out.Reachability = reach

	out.Available = true
	out.Reason = ""
	return out, nil
}

func backlogOriginTX(tx *gorm.DB, msgTmp string) (attendance.XrayDimension, error) {
	sql := `
		SELECT container_id AS key,
			COALESCE(NULLIF(MAX(container_name), ''), container_id) AS label,
			COUNT(*)::bigint AS count
		FROM ` + msgTmp + `
		WHERE ` + backlogPredicate + ` AND container_id <> ''
		GROUP BY container_id
	`
	var rows []xrayCountRow
	if err := tx.Raw(sql).Scan(&rows).Error; err != nil {
		return attendance.XrayDimension{}, err
	}

	unknown, err := backlogCountTX(tx, msgTmp, "container_id = ''")
	if err != nil {
		return attendance.XrayDimension{}, err
	}

	return attendance.BuildRankedDimension(
		attendance.XrayDimensionOrigin,
		toTallies(rows),
		unknown,
		attendance.XrayTopN,
	), nil
}

func backlogAssigneeTX(tx *gorm.DB, msgTmp string) (attendance.XrayDimension, error) {
	sql := `
		SELECT m.assigned_user_id AS key,
			COALESCE(NULLIF(u.username, ''), NULLIF(u.email, ''), m.assigned_user_id) AS label,
			COUNT(*)::bigint AS count
		FROM ` + msgTmp + ` m
		LEFT JOIN users u ON u.id::text = m.assigned_user_id
		WHERE m.total_msgs > 0 AND m.status_bucket <> 'finished' AND m.assigned_user_id <> ''
		GROUP BY m.assigned_user_id, u.username, u.email
	`
	var rows []xrayCountRow
	if err := tx.Raw(sql).Scan(&rows).Error; err != nil {
		return attendance.XrayDimension{}, err
	}

	unassigned, err := backlogCountTX(tx, msgTmp, "assigned_user_id = ''")
	if err != nil {
		return attendance.XrayDimension{}, err
	}

	return attendance.BuildRankedDimension(
		attendance.XrayDimensionAssignee,
		toTallies(rows),
		unassigned,
		attendance.XrayTopN,
	), nil
}

func backlogAgeQuery(msgTmp string, now time.Time) *sqlQuery {
	return newSQLQuery().add(`
		SELECT band AS key, band AS label, COUNT(*)::bigint AS count
		FROM (
			SELECT CASE
				WHEN created_at >= ? THEN '0_1d'
				WHEN created_at >= ? THEN '1_3d'
				WHEN created_at >= ? THEN '3_7d'
				WHEN created_at >= ? THEN '7_30d'
				ELSE '30d_plus'
			END AS band
			FROM `+msgTmp+`
			WHERE `+backlogPredicate+`
		) banded
		GROUP BY band
	`,
		now.AddDate(0, 0, -1),
		now.AddDate(0, 0, -3),
		now.AddDate(0, 0, -7),
		now.AddDate(0, 0, -30),
	)
}

func backlogAgeTX(tx *gorm.DB, msgTmp string, now time.Time) (attendance.XrayDimension, error) {
	sql, args := backlogAgeQuery(msgTmp, now).build()
	var rows []xrayCountRow
	if err := tx.Raw(sql, args...).Scan(&rows).Error; err != nil {
		return attendance.XrayDimension{}, err
	}
	return attendance.BuildOrderedDimension(
		attendance.XrayDimensionAge,
		withPositions(rows, []string{"0_1d", "1_3d", "3_7d", "7_30d", "30d_plus"}),
		0,
	), nil
}

func backlogTenureQuery(msgTmp string, now time.Time) *sqlQuery {
	return newSQLQuery().add(`
		SELECT band AS key, band AS label, COUNT(*)::bigint AS count
		FROM (
			SELECT CASE
				WHEN l.created_at >= ? THEN 'under_1m'
				WHEN l.created_at >= ? THEN '1_6m'
				WHEN l.created_at >= ? THEN '6_12m'
				ELSE '12m_plus'
			END AS band
			FROM `+msgTmp+` m
			JOIN leads l ON l.id::text = m.lead_id
			WHERE m.total_msgs > 0 AND m.status_bucket <> 'finished' AND m.lead_id <> ''
		) banded
		GROUP BY band
	`,
		now.AddDate(0, -1, 0),
		now.AddDate(0, -6, 0),
		now.AddDate(0, -12, 0),
	)
}

func backlogTenureTX(tx *gorm.DB, msgTmp string, now time.Time) (attendance.XrayDimension, error) {
	sql, args := backlogTenureQuery(msgTmp, now).build()
	var rows []xrayCountRow
	if err := tx.Raw(sql, args...).Scan(&rows).Error; err != nil {
		return attendance.XrayDimension{}, err
	}

	unknownSQL := `
		SELECT COUNT(*)::bigint
		FROM ` + msgTmp + ` m
		LEFT JOIN leads l ON l.id::text = m.lead_id
		WHERE m.total_msgs > 0 AND m.status_bucket <> 'finished' AND l.id IS NULL
	`
	var unknown int64
	if err := tx.Raw(unknownSQL).Scan(&unknown).Error; err != nil {
		return attendance.XrayDimension{}, err
	}

	return attendance.BuildOrderedDimension(
		attendance.XrayDimensionTenure,
		withPositions(rows, []string{"under_1m", "1_6m", "6_12m", "12m_plus"}),
		unknown,
	), nil
}

func backlogReturningTX(
	tx *gorm.DB,
	workspaceID, msgTmp string,
	filter attendance.OverviewFilter,
) (attendance.XrayDimension, error) {
	priorSQL, priorArgs := priorFinishedLeadsUnion(workspaceID, msgTmp, filter)
	if priorSQL == "" {
		return attendance.UnavailableXrayDimension(attendance.XrayDimensionReturning, attendance.ReasonNoCoverage), nil
	}

	sql := `
		WITH prior AS (` + priorSQL + `)
		SELECT CASE WHEN p.lead_id IS NULL THEN 'first_contact' ELSE 'returning' END AS key,
			CASE WHEN p.lead_id IS NULL THEN 'first_contact' ELSE 'returning' END AS label,
			COUNT(*)::bigint AS count
		FROM ` + msgTmp + ` m
		LEFT JOIN prior p ON p.lead_id = m.lead_id
		WHERE m.total_msgs > 0 AND m.status_bucket <> 'finished' AND m.lead_id <> ''
		GROUP BY 1, 2
	`
	var rows []xrayCountRow
	if err := tx.Raw(sql, priorArgs...).Scan(&rows).Error; err != nil {
		return attendance.XrayDimension{}, err
	}

	unknown, err := backlogCountTX(tx, msgTmp, "lead_id = ''")
	if err != nil {
		return attendance.XrayDimension{}, err
	}

	return attendance.BuildOrderedDimension(
		attendance.XrayDimensionReturning,
		withPositions(rows, []string{"returning", "first_contact"}),
		unknown,
	), nil
}

func priorFinishedLeadsUnion(workspaceID, msgTmp string, filter attendance.OverviewFilter) (string, []interface{}) {
	sources := selectedChannelSources(filter.Channel)
	parts := make([]string, 0, len(sources))
	args := make([]interface{}, 0, len(sources))

	for _, src := range sources {
		if src.LeadIDExpr == "" || src.StatusColumn == "" {
			continue
		}
		parts = append(parts, `
			SELECT DISTINCT `+src.LeadIDExpr+` AS lead_id
			FROM `+src.EntryTable+`
			JOIN `+src.ContainerTable+`
				ON `+src.ContainerJoin+` AND `+src.ContainerAlias+`.deleted_at IS NULL
			`+src.LeadJoin+`
			WHERE `+src.WorkspaceColumn+` = ?
			  AND `+src.EntryAlias+`.deleted_at IS NULL
			  AND `+src.StatusColumn+` = 'finished'
			  AND `+src.LeadIDExpr+` <> ''
			  AND `+src.LeadIDExpr+` IN (
				SELECT DISTINCT lead_id FROM `+msgTmp+`
				WHERE `+backlogPredicate+` AND lead_id <> ''
			  )
			  AND `+src.EntryAlias+`.id NOT IN (
				SELECT entry_id FROM `+msgTmp+` WHERE entry_type = '`+string(src.EntryType)+`'
			  )
		`)
		args = append(args, workspaceID)
	}
	if len(parts) == 0 {
		return "", nil
	}
	return strings.Join(parts, " UNION "), args
}

func backlogRecordCompletenessTX(tx *gorm.DB, msgTmp string) (attendance.RecordCompleteness, error) {
	type fillRow struct {
		Measured    int64 `gorm:"column:measured"`
		NameFilled  int64 `gorm:"column:name_filled"`
		AgeFilled   int64 `gorm:"column:age_filled"`
		FullyFilled int64 `gorm:"column:fully_filled"`
	}
	sql := `
		WITH backlog_leads AS (
			SELECT DISTINCT m.lead_id
			FROM ` + msgTmp + ` m
			WHERE m.total_msgs > 0 AND m.status_bucket <> 'finished' AND m.lead_id <> ''
		)
		SELECT COUNT(*)::bigint AS measured,
			COUNT(*) FILTER (WHERE COALESCE(TRIM(l.name), '') <> '')::bigint AS name_filled,
			COUNT(*) FILTER (WHERE l.age IS NOT NULL AND l.age > 0)::bigint AS age_filled,
			COUNT(*) FILTER (
				WHERE COALESCE(TRIM(l.name), '') <> '' AND l.age IS NOT NULL AND l.age > 0
			)::bigint AS fully_filled
		FROM backlog_leads bl
		JOIN leads l ON l.id::text = bl.lead_id
	`
	var row fillRow
	if err := tx.Raw(sql).Scan(&row).Error; err != nil {
		return attendance.RecordCompleteness{}, err
	}
	return attendance.BuildRecordCompleteness(
		[]attendance.RecordFieldTally{
			{Key: backlogRecordFields[0], Filled: row.NameFilled},
			{Key: backlogRecordFields[1], Filled: row.AgeFilled},
		},
		row.Measured,
		row.FullyFilled,
	), nil
}

func backlogReachabilityTX(
	tx *gorm.DB,
	msgTmp string,
	filter attendance.OverviewFilter,
	now time.Time,
) ([]attendance.Reachability, error) {
	sources := selectedChannelSources(filter.Channel)
	out := make([]attendance.Reachability, 0, len(sources))

	type channelRow struct {
		Measured   int64 `gorm:"column:measured"`
		WindowOpen int64 `gorm:"column:window_open"`
	}

	for _, src := range sources {
		channel := string(src.EntryType)
		measured, err := backlogCountTX(tx, msgTmp, "entry_type = '"+channel+"'")
		if err != nil {
			return nil, err
		}
		if !src.HasMsgWindow {
			out = append(out, attendance.BuildReachability(channel, measured, 0, false))
			continue
		}
		if measured == 0 {
			out = append(out, attendance.BuildReachability(channel, 0, 0, true))
			continue
		}

		sql := `
			SELECT COUNT(*)::bigint AS measured,
				COUNT(*) FILTER (WHERE w.last_message_at IS NOT NULL AND w.last_message_at >= ?)::bigint AS window_open
			FROM ` + msgTmp + ` m
			LEFT JOIN LATERAL (
				SELECT MAX(lmw.last_message_at) AS last_message_at
				FROM lead_message_windows lmw
				WHERE lmw.lead_id::text = m.lead_id
			) w ON TRUE
			WHERE m.total_msgs > 0 AND m.status_bucket <> 'finished'
			  AND m.entry_type = ? AND m.lead_id <> ''
		`
		var row channelRow
		cutoff := now.Add(-lead_message_window.MessageWindowDuration)
		if err := tx.Raw(sql, cutoff, channel).Scan(&row).Error; err != nil {
			return nil, err
		}
		out = append(out, attendance.BuildReachability(channel, row.Measured, row.WindowOpen, true))
	}
	return out, nil
}

func backlogCountTX(tx *gorm.DB, msgTmp, extra string) (int64, error) {
	sql := `SELECT COUNT(*)::bigint FROM ` + msgTmp + ` WHERE ` + backlogPredicate
	if extra != "" {
		sql += " AND " + extra
	}
	var count int64
	if err := tx.Raw(sql).Scan(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func toTallies(rows []xrayCountRow) []attendance.XrayTally {
	out := make([]attendance.XrayTally, 0, len(rows))
	for _, row := range rows {
		out = append(out, attendance.XrayTally{Key: row.Key, Label: row.Label, Count: row.Count})
	}
	return out
}

func withPositions(rows []xrayCountRow, order []string) []attendance.XrayTally {
	counts := make(map[string]int64, len(rows))
	for _, row := range rows {
		counts[row.Key] = row.Count
	}
	out := make([]attendance.XrayTally, 0, len(order))
	for i, key := range order {
		out = append(out, attendance.XrayTally{
			Key:      key,
			Label:    key,
			Count:    counts[key],
			Position: i,
		})
	}
	return out
}
