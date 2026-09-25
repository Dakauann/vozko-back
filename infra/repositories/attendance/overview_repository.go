package attendance_repository

import (
	"context"
	"math"
	"strings"
	"time"

	"gorm.io/gorm"

	"vozko/domain/attendance"
)

const (
	scopeEntriesTable         = "tmp_att_scope"
	scopeMessagesTable        = "tmp_att_scope_msg"
	analyticsStatementTimeout = "25s"
)

func (r *repository) analytics(fn func(tx *gorm.DB) error) error {
	return r.analyticsContext(context.Background(), fn)
}

func (r *repository) analyticsContext(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL jit = off").Error; err != nil {
			return err
		}
		if err := tx.Exec("SET LOCAL statement_timeout = '" + analyticsStatementTimeout + "'").Error; err != nil {
			return err
		}
		return fn(tx)
	})
}

func (r *repository) withScope(
	ctx context.Context,
	workspaceID string,
	filter attendance.OverviewFilter,
	fn func(tx *gorm.DB) error,
) error {
	return r.analyticsContext(ctx, func(tx *gorm.DB) error {
		if err := buildScope(tx, workspaceID, filter); err != nil {
			return err
		}
		return fn(tx)
	})
}

func buildScope(tx *gorm.DB, workspaceID string, filter attendance.OverviewFilter) error {
	selectBody, args := overviewEntrySelect(workspaceID, filter)
	statements := []struct {
		sql  string
		args []interface{}
	}{
		{sql: "CREATE TEMP TABLE " + scopeEntriesTable + " ON COMMIT DROP AS " + selectBody, args: args},
		{sql: "CREATE INDEX ON " + scopeEntriesTable + " (entry_id, entry_type)"},
		{sql: "ANALYZE " + scopeEntriesTable},
		{sql: scopeMessagesSQL()},
		{sql: "CREATE INDEX ON " + scopeMessagesTable + " (entry_id, entry_type)"},
		{sql: "CREATE INDEX ON " + scopeMessagesTable + " (department_id)"},
		{sql: "CREATE INDEX ON " + scopeMessagesTable + " (total_msgs)"},
		{sql: "ANALYZE " + scopeMessagesTable},
	}
	for _, statement := range statements {
		if err := tx.Exec(statement.sql, statement.args...).Error; err != nil {
			return err
		}
	}
	return nil
}

func scopeMessagesSQL() string {
	return `
		CREATE TEMP TABLE ` + scopeMessagesTable + ` ON COMMIT DROP AS
		SELECT
			se.entry_id,
			se.entry_type,
			se.department_id,
			se.assigned_user_id,
			se.status_bucket,
			se.is_new_contact,
			se.hour_bucket,
			se.close_source,
			se.close_outcome,
			se.closed_at,
			se.container_id,
			se.container_name,
			se.lead_id,
			se.created_at,
			msg.first_inbound_at,
			msg.first_agent_at,
			msg.last_agent_at,
			msg.first_assignee_op_at,
			msg.total_msgs,
			msg.inbound_msgs,
			msg.outbound_msgs,
			msg.template_msgs
		FROM ` + scopeEntriesTable + ` se
		LEFT JOIN LATERAL (
			SELECT
			MIN(cm.created_at) FILTER (
				WHERE cm.message_type IN ('user_message', 'audio', 'media')
			) AS first_inbound_at,
			MIN(cm.created_at) FILTER (
				WHERE cm.message_type IN ('operator', 'ai_response')
			) AS first_agent_at,
			MAX(cm.created_at) FILTER (
				WHERE cm.message_type IN ('operator', 'ai_response')
			) AS last_agent_at,
			MIN(cm.created_at) FILTER (
				WHERE cm.message_type = 'operator'
				  AND se.assigned_user_id <> ''
				  AND cm.from_participant = se.assigned_user_id
			) AS first_assignee_op_at,
			COUNT(cm.id)::int AS total_msgs,
			COUNT(cm.id) FILTER (
				WHERE cm.message_type IN ('user_message', 'audio', 'media')
			)::int AS inbound_msgs,
			COUNT(cm.id) FILTER (
				WHERE cm.message_type IN ('operator', 'ai_response', 'template')
			)::int AS outbound_msgs,
			COUNT(cm.id) FILTER (
				WHERE cm.message_type = 'template'
			)::int AS template_msgs
			FROM conversation_messages cm
			WHERE cm.entry_id = se.entry_id
			  AND cm.entry_type = se.entry_type
			  AND cm.deleted_at IS NULL
			  AND ` + realMessageSQL("cm") + `
		) msg ON TRUE
		WHERE msg.total_msgs > 0
	`
}

func blankWorkspace(workspaceID string) bool {
	return strings.TrimSpace(workspaceID) == ""
}

func emptySummary(filter attendance.OverviewFilter) *attendance.SummarySection {
	out := &attendance.SummarySection{
		Filter:      filter,
		Hourly:      make([]attendance.HourlyPoint, 24),
		Revenue:     attendance.UnavailableRevenue(attendance.ReasonNoRevenueRepository),
		Quality:     attendance.UnavailableQuality(attendance.ReasonCaptureDisabled),
		Projections: []attendance.MetricProjection{},
		GeneratedAt: time.Now().UTC(),
		Definitions: attendance.DefaultDefinitions(),
		ChannelMix:  []attendance.ChannelSlice{},
	}
	for h := range out.Hourly {
		out.Hourly[h] = attendance.HourlyPoint{Hour: h}
	}
	return out
}

func (r *repository) ReadSummary(
	ctx context.Context,
	workspaceID string,
	filter attendance.OverviewFilter,
) (*attendance.SummarySection, error) {
	out := emptySummary(filter)
	if blankWorkspace(workspaceID) {
		return out, nil
	}
	err := r.withScope(ctx, workspaceID, filter, func(tx *gorm.DB) error {
		if err := summaryStatusTX(tx, out); err != nil {
			return err
		}
		newLeads, err := overviewNewLeadsTX(tx, workspaceID, filter)
		if err != nil {
			return err
		}
		out.KPIs.NewLeads = newLeads
		if out.KPIs.AvgWaitMins, err = overviewAvgWaitMinsTX(tx, scopeMessagesTable); err != nil {
			return err
		}
		if out.KPIs.AvgHandleMins, err = overviewAvgHandleMinsTX(tx, scopeMessagesTable); err != nil {
			return err
		}
		if err := summaryHourlyTX(tx, out); err != nil {
			return err
		}
		if out.Quality, err = overviewQualityTX(tx, scopeMessagesTable, filter.Quality); err != nil {
			return err
		}
		return overviewFillExtendedTX(tx, workspaceID, scopeMessagesTable, filter, out)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func summaryStatusTX(tx *gorm.DB, out *attendance.SummarySection) error {
	type statusRow struct {
		Engaged        int64
		ShellBacklog   int64
		TotalScoped    int64
		EntriesCreated int64
		Finished       int64
		Ongoing        int64
		Pending        int64
	}
	var sr statusRow
	if err := tx.Raw(scopeStatusSQL()).Scan(&sr).Error; err != nil {
		return err
	}
	out.KPIs.Engaged = sr.Engaged
	out.KPIs.ShellBacklog = sr.ShellBacklog
	out.KPIs.TotalScoped = sr.TotalScoped
	out.KPIs.EntriesCreated = sr.EntriesCreated
	out.KPIs.Finished = sr.Finished
	out.KPIs.Ongoing = sr.Ongoing
	out.KPIs.Pending = sr.Pending
	out.StatusDistribution = attendance.StatusDistribution{
		Finished: sr.Finished,
		Ongoing:  sr.Ongoing,
		Pending:  sr.Pending,
		Total:    sr.Finished + sr.Ongoing + sr.Pending,
	}
	return nil
}

func scopeStatusSQL() string {
	return `
		SELECT
			COUNT(*) FILTER (WHERE total_msgs > 0) AS engaged,
			COUNT(*) FILTER (WHERE total_msgs = 0) AS shell_backlog,
			COUNT(*) AS total_scoped,
			COUNT(*) FILTER (WHERE is_new_contact) AS entries_created,
			COUNT(*) FILTER (WHERE total_msgs > 0 AND status_bucket = 'finished') AS finished,
			COUNT(*) FILTER (WHERE total_msgs > 0 AND status_bucket = 'ongoing') AS ongoing,
			COUNT(*) FILTER (WHERE total_msgs > 0 AND status_bucket = 'pending') AS pending
		FROM ` + scopeMessagesTable
}

func summaryHourlyTX(tx *gorm.DB, out *attendance.SummarySection) error {
	type hourRow struct {
		Hour  int
		Count int64
	}
	var hours []hourRow
	err := tx.Raw(`
		SELECT hour_bucket AS hour, COUNT(*)::bigint AS count
		FROM ` + scopeMessagesTable + `
		WHERE total_msgs > 0
		GROUP BY hour_bucket
	`).Scan(&hours).Error
	if err != nil {
		return err
	}
	for _, h := range hours {
		if h.Hour >= 0 && h.Hour < 24 {
			out.Hourly[h.Hour].Count = h.Count
		}
	}
	return nil
}

func (r *repository) ReadTeam(
	ctx context.Context,
	workspaceID string,
	filter attendance.OverviewFilter,
) (*attendance.TeamSection, error) {
	out := &attendance.TeamSection{
		ByDepartment: []attendance.DepartmentRow{},
		ByMember:     []attendance.MemberRow{},
		TeamRanking:  attendance.UnavailableTeamRanking(attendance.ReasonNoTeamRows),
	}
	if blankWorkspace(workspaceID) {
		return out, nil
	}
	err := r.withScope(ctx, workspaceID, filter, func(tx *gorm.DB) error {
		var err error
		if out.ByDepartment, err = overviewByDepartmentTX(tx, scopeMessagesTable); err != nil {
			return err
		}
		out.ByMember, err = overviewByMemberTX(tx, workspaceID, scopeMessagesTable, filter)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *repository) ReadStages(
	ctx context.Context,
	workspaceID string,
	filter attendance.OverviewFilter,
) (attendance.OverviewStages, error) {
	if blankWorkspace(workspaceID) {
		return attendance.BuildStageDistribution(nil, 0, 0), nil
	}
	var out attendance.OverviewStages
	err := r.withScope(ctx, workspaceID, filter, func(tx *gorm.DB) error {
		var engaged struct {
			Engaged int64
			Shell   int64
		}
		err := tx.Raw(`
			SELECT
				COUNT(*) FILTER (WHERE total_msgs > 0) AS engaged,
				COUNT(*) FILTER (WHERE total_msgs = 0) AS shell
			FROM ` + scopeMessagesTable).Scan(&engaged).Error
		if err != nil {
			return err
		}
		tallies, err := overviewStageTalliesTX(tx, workspaceID, scopeMessagesTable)
		if err != nil {
			return err
		}
		out = attendance.BuildStageDistribution(tallies, engaged.Engaged, engaged.Shell)
		return nil
	})
	if err != nil {
		return attendance.OverviewStages{}, err
	}
	return out, nil
}

func (r *repository) ReadBacklog(
	ctx context.Context,
	workspaceID string,
	filter attendance.OverviewFilter,
	now time.Time,
) (attendance.BacklogXray, error) {
	if blankWorkspace(workspaceID) {
		return emptyBacklogXray(), nil
	}
	var out attendance.BacklogXray
	err := r.withScope(ctx, workspaceID, filter, func(tx *gorm.DB) error {
		var err error
		out, err = overviewBacklogXrayTX(tx, workspaceID, scopeMessagesTable, filter, now)
		return err
	})
	if err != nil {
		return attendance.BacklogXray{}, err
	}
	return out, nil
}

func (r *repository) ReadRework(
	ctx context.Context,
	workspaceID string,
	filter attendance.OverviewFilter,
) (attendance.OverviewRework, error) {
	if blankWorkspace(workspaceID) {
		return attendance.UnavailableRework(attendance.ReasonReworkUnwatched), nil
	}
	var out attendance.OverviewRework
	err := r.withScope(ctx, workspaceID, filter, func(tx *gorm.DB) error {
		var err error
		out, err = overviewReworkTX(tx, workspaceID, scopeMessagesTable)
		return err
	})
	if err != nil {
		return attendance.OverviewRework{}, err
	}
	return out, nil
}

func overviewEntrySelect(workspaceID string, f attendance.OverviewFilter) (string, []interface{}) {
	var from, to *time.Time
	if f.DateFrom != nil {
		from = f.DateFrom
	}
	if f.DateTo != nil {
		to = f.DateTo
	}

	parts := []string{}
	var args []interface{}

	createdInRange := func(alias string) (string, []interface{}) {
		if from == nil && to == nil {
			return "TRUE", nil
		}
		conds := []string{}
		var a []interface{}
		if from != nil {
			conds = append(conds, alias+".created_at >= ?")
			a = append(a, *from)
		}
		if to != nil {
			conds = append(conds, alias+".created_at <= ?")
			a = append(a, *to)
		}
		return strings.Join(conds, " AND "), a
	}

	createdOutsideRange := func(alias string) (string, []interface{}) {
		if from == nil && to == nil {
			return "FALSE", nil
		}
		ors := []string{}
		var a []interface{}
		if from != nil {
			ors = append(ors, alias+".created_at < ?")
			a = append(a, *from)
		}
		if to != nil {
			ors = append(ors, alias+".created_at > ?")
			a = append(a, *to)
		}
		return "(" + strings.Join(ors, " OR ") + ")", a
	}

	filterCampaignDeptMember := func(src channelSource) (string, []interface{}) {
		extra := ""
		var a []interface{}
		if f.CampaignID != "" {
			if f.CampaignType == "" || f.CampaignType == string(src.EntryType) {
				extra += " AND " + src.ContainerIDColumn + " = ?"
				a = append(a, f.CampaignID)
			} else {
				extra += " AND FALSE"
			}
		}
		if f.DepartmentID != "" {
			extra += " AND " + src.DepartmentColumn + " = ?"
			a = append(a, f.DepartmentID)
		}
		if f.MemberID != "" {
			extra += " AND ia.assigned_user_id = ?"
			a = append(a, f.MemberID)
		}
		return extra, a
	}

	appendCreated := func(src channelSource, whereExtra string, whereArgs []interface{}) {
		sql := `
			SELECT ` + src.projection("TRUE") + `
			FROM ` + src.ContainerTable + `
			JOIN ` + src.EntryTable + `
				ON ` + src.ContainerJoin + ` AND ` + src.EntryAlias + `.deleted_at IS NULL
			LEFT JOIN inbox_assignments ia
				ON ia.entry_id = ` + src.EntryAlias + `.id AND ia.entry_type = '` + string(src.EntryType) + `'
			` + src.LeadJoin + `
			WHERE ` + src.WorkspaceColumn + ` = ? AND ` + src.ContainerAlias + `.deleted_at IS NULL
			  AND ` + src.LastMessageColumn + ` IS NOT NULL
			` + whereExtra
		a := []interface{}{workspaceID}
		a = append(a, whereArgs...)
		parts = append(parts, sql)
		args = append(args, a...)
	}

	appendActivity := func(src channelSource, fcd string, fca []interface{}) {
		if from == nil && to == nil {
			return
		}
		cout, couta := createdOutsideRange(src.EntryAlias)
		sql := `
			SELECT ` + src.projection("FALSE") + `
			FROM ` + src.ContainerTable + `
			JOIN ` + src.EntryTable + `
				ON ` + src.ContainerJoin + ` AND ` + src.EntryAlias + `.deleted_at IS NULL
			LEFT JOIN inbox_assignments ia
				ON ia.entry_id = ` + src.EntryAlias + `.id AND ia.entry_type = '` + string(src.EntryType) + `'
			` + src.LeadJoin + `
			WHERE ` + src.WorkspaceColumn + ` = ? AND ` + src.ContainerAlias + `.deleted_at IS NULL
			  AND ` + cout
		a := []interface{}{workspaceID}
		a = append(a, couta...)
		if from != nil {
			sql += " AND " + src.LastMessageColumn + " >= ?"
			a = append(a, *from)
		}
		sql += fcd + `
			  AND EXISTS (
				SELECT 1 FROM conversation_messages cm
				WHERE cm.entry_id = ` + src.EntryAlias + `.id
				  AND cm.entry_type = '` + string(src.EntryType) + `'
				  AND cm.deleted_at IS NULL
				  AND ` + realMessageSQL("cm")
		a = append(a, fca...)
		if from != nil {
			sql += " AND cm.created_at >= ?"
			a = append(a, *from)
		}
		if to != nil {
			sql += " AND cm.created_at <= ?"
			a = append(a, *to)
		}
		sql += `
			  )`
		parts = append(parts, sql)
		args = append(args, a...)
	}

	for _, src := range selectedChannelSources(f.Channel) {
		fcd, fca := filterCampaignDeptMember(src)
		if from == nil && to == nil {
			appendCreated(src, fcd, fca)
			continue
		}
		cin, cina := createdInRange(src.EntryAlias)
		a1 := append(append([]interface{}{}, fca...), cina...)
		appendCreated(src, fcd+" AND "+cin, a1)
		appendActivity(src, fcd, fca)
	}

	if len(parts) == 0 {
		return emptyEntryProjection(), nil
	}

	return strings.Join(parts, " UNION ALL "), args
}

func overviewNewLeadsTX(tx *gorm.DB, workspaceID string, f attendance.OverviewFilter) (int64, error) {
	q := tx.Table("leads").
		Where("workspace_id = ?", workspaceID).
		Where("deleted_at IS NULL")
	if f.DateFrom != nil {
		q = q.Where("created_at >= ?", *f.DateFrom)
	}
	if f.DateTo != nil {
		q = q.Where("created_at <= ?", *f.DateTo)
	}

	var count int64
	if err := q.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func overviewAvgWaitMinsTX(tx *gorm.DB, msgTmp string) (*float64, error) {
	sql := `
		SELECT AVG(EXTRACT(EPOCH FROM (first_agent_at - first_inbound_at))) AS avg_secs
		FROM ` + msgTmp + `
		WHERE first_agent_at IS NOT NULL
		  AND first_inbound_at IS NOT NULL
		  AND first_agent_at >= first_inbound_at
	`
	var avg *float64
	if err := tx.Raw(sql).Scan(&avg).Error; err != nil {
		return nil, err
	}
	if avg == nil || *avg <= 0 {
		return nil, nil
	}
	v := math.Round((*avg/60)*100) / 100
	return &v, nil
}

func overviewAvgHandleMinsTX(tx *gorm.DB, msgTmp string) (*float64, error) {
	sql := `
		SELECT AVG(EXTRACT(EPOCH FROM (last_agent_at - first_agent_at))) AS avg_secs
		FROM ` + msgTmp + `
		WHERE status_bucket = 'finished'
		  AND first_agent_at IS NOT NULL
		  AND last_agent_at IS NOT NULL
		  AND last_agent_at >= first_agent_at
	`
	var avg *float64
	if err := tx.Raw(sql).Scan(&avg).Error; err != nil {
		return nil, err
	}
	if avg == nil || *avg < 0 {
		return nil, nil
	}
	v := math.Round((*avg/60)*100) / 100
	return &v, nil
}

func overviewByDepartmentTX(tx *gorm.DB, msgTmp string) ([]attendance.DepartmentRow, error) {
	sql := `
		SELECT
			m.department_id,
			COALESCE(NULLIF(wd.name, ''), CASE WHEN m.department_id = '' THEN 'Sem departamento' ELSE m.department_id END) AS department_name,
			COUNT(*) FILTER (WHERE m.status_bucket = 'finished') AS finished,
			COUNT(*) FILTER (
				WHERE m.status_bucket = 'finished'
				  AND (m.close_source = 'human' OR m.close_source = '' OR m.close_source IS NULL)
			) AS finished_human,
			COUNT(*) FILTER (
				WHERE m.status_bucket = 'finished' AND m.close_source = 'ai'
			) AS finished_ai,
			COUNT(*) FILTER (
				WHERE m.status_bucket = 'finished' AND m.close_source = 'system'
			) AS finished_system,
			COUNT(*) FILTER (WHERE m.status_bucket = 'ongoing') AS ongoing,
			COUNT(*) FILTER (WHERE m.status_bucket = 'pending') AS pending,
			AVG(EXTRACT(EPOCH FROM (m.first_agent_at - m.first_inbound_at)))
				FILTER (WHERE m.first_agent_at IS NOT NULL AND m.first_inbound_at IS NOT NULL
				            AND m.first_agent_at >= m.first_inbound_at) AS avg_wait,
			AVG(EXTRACT(EPOCH FROM (m.last_agent_at - m.first_agent_at)))
				FILTER (WHERE m.status_bucket = 'finished'
				            AND m.first_agent_at IS NOT NULL AND m.last_agent_at IS NOT NULL
				            AND m.last_agent_at >= m.first_agent_at) AS avg_handle
		FROM ` + msgTmp + ` m
		LEFT JOIN workspace_departments wd ON wd.id::text = m.department_id
		WHERE m.total_msgs > 0
		GROUP BY m.department_id, wd.name
		ORDER BY finished DESC, ongoing DESC
	`
	type row struct {
		DepartmentID   string   `gorm:"column:department_id"`
		DepartmentName string   `gorm:"column:department_name"`
		Finished       int64    `gorm:"column:finished"`
		FinishedHuman  int64    `gorm:"column:finished_human"`
		FinishedAI     int64    `gorm:"column:finished_ai"`
		FinishedSystem int64    `gorm:"column:finished_system"`
		Ongoing        int64    `gorm:"column:ongoing"`
		Pending        int64    `gorm:"column:pending"`
		AvgWait        *float64 `gorm:"column:avg_wait"`
		AvgHandle      *float64 `gorm:"column:avg_handle"`
	}
	var rows []row
	if err := tx.Raw(sql).Scan(&rows).Error; err != nil {
		return nil, err
	}

	out := make([]attendance.DepartmentRow, 0, len(rows))
	for _, rw := range rows {
		out = append(out, attendance.DepartmentRow{
			DepartmentID:   rw.DepartmentID,
			DepartmentName: rw.DepartmentName,
			Finished:       rw.Finished,
			FinishedHuman:  rw.FinishedHuman,
			FinishedAI:     rw.FinishedAI,
			FinishedSystem: rw.FinishedSystem,
			Ongoing:        rw.Ongoing,
			Pending:        rw.Pending,
			AvgWaitMins:    nonNegativeMinutes(rw.AvgWait),
			AvgHandleMins:  nonNegativeMinutes(rw.AvgHandle),
		})
	}
	return out, nil
}

func nonNegativeMinutes(seconds *float64) *float64 {
	if seconds == nil || *seconds < 0 {
		return nil
	}
	v := math.Round((*seconds/60)*100) / 100
	return &v
}

func overviewByMemberTX(tx *gorm.DB, workspaceID, msgTmp string, filter attendance.OverviewFilter) ([]attendance.MemberRow, error) {
	sql := `
		SELECT
			m.assigned_user_id AS actor_id,
			COALESCE(NULLIF(u.username, ''), NULLIF(u.email, ''), m.assigned_user_id) AS display_name,
			COALESCE(u.email, '') AS email,
			COUNT(*) FILTER (WHERE m.status_bucket = 'ongoing') AS open_count,
			COUNT(*) FILTER (WHERE m.status_bucket = 'pending') AS pending_count,
			COUNT(*) FILTER (WHERE m.status_bucket = 'finished') AS resolved_count,
			COUNT(*) FILTER (
				WHERE m.status_bucket = 'finished'
				  AND (m.close_source = 'human' OR m.close_source = '' OR m.close_source IS NULL)
			) AS finished_human,
			COUNT(*) FILTER (
				WHERE m.status_bucket = 'finished' AND m.close_source = 'ai'
			) AS finished_ai,
			COUNT(*) FILTER (
				WHERE m.status_bucket = 'finished' AND m.close_source = 'system'
			) AS finished_system,
			COALESCE(SUM(m.total_msgs), 0)::bigint AS total_messages,
			COALESCE(SUM(m.inbound_msgs), 0)::bigint AS inbound_messages,
			AVG(EXTRACT(EPOCH FROM (m.first_assignee_op_at - m.first_inbound_at))) FILTER (
				WHERE m.first_assignee_op_at IS NOT NULL
				  AND m.first_inbound_at IS NOT NULL
				  AND m.first_assignee_op_at >= m.first_inbound_at
			) AS avg_response_secs
		FROM ` + msgTmp + ` m
		LEFT JOIN users u ON u.id::text = m.assigned_user_id
		WHERE m.assigned_user_id <> ''
		  AND m.total_msgs > 0
		GROUP BY m.assigned_user_id, u.username, u.email
		ORDER BY resolved_count DESC, open_count DESC
	`
	type row struct {
		ActorID         string   `gorm:"column:actor_id"`
		DisplayName     string   `gorm:"column:display_name"`
		Email           string   `gorm:"column:email"`
		OpenCount       int64    `gorm:"column:open_count"`
		PendingCount    int64    `gorm:"column:pending_count"`
		ResolvedCount   int64    `gorm:"column:resolved_count"`
		FinishedHuman   int64    `gorm:"column:finished_human"`
		FinishedAI      int64    `gorm:"column:finished_ai"`
		FinishedSystem  int64    `gorm:"column:finished_system"`
		TotalMessages   int64    `gorm:"column:total_messages"`
		InboundMessages int64    `gorm:"column:inbound_messages"`
		AvgResponseSecs *float64 `gorm:"column:avg_response_secs"`
	}
	var rows []row
	if err := tx.Raw(sql).Scan(&rows).Error; err != nil {
		return nil, err
	}

	presence, err := openPresenceTX(tx, workspaceID)
	if err != nil {
		return nil, err
	}

	out := make([]attendance.MemberRow, 0, len(rows))
	for _, rw := range rows {
		total := rw.OpenCount + rw.PendingCount + rw.ResolvedCount
		resPct := float64(0)
		if total > 0 {
			resPct = math.Round(float64(rw.ResolvedCount)/float64(total)*10000) / 100
		}
		state := "offline"
		if s, ok := presence[rw.ActorID]; ok && s != "" {
			state = s
		}
		out = append(out, attendance.MemberRow{
			ActorID:         rw.ActorID,
			ActorKind:       attendance.ActorKindHuman,
			DisplayName:     rw.DisplayName,
			Email:           rw.Email,
			Presence:        state,
			AvgResponseMins: nonNegativeMinutes(rw.AvgResponseSecs),
			ResolutionPct:   resPct,
			Open:            rw.OpenCount,
			Pending:         rw.PendingCount,
			Resolved:        rw.ResolvedCount,
			FinishedHuman:   rw.FinishedHuman,
			FinishedAI:      rw.FinishedAI,
			FinishedSystem:  rw.FinishedSystem,
			TotalMessages:   rw.TotalMessages,
			InboundMessages: rw.InboundMessages,
			AvgMessages:     avgMessagesPerConversation(rw.TotalMessages, total),
		})
	}

	if !filter.IncludeAI {
		return out, nil
	}
	aiRows, err := aiMemberRowsTX(tx, workspaceID, filter)
	if err != nil {
		return nil, err
	}
	return append(out, aiRows...), nil
}

func openPresenceTX(tx *gorm.DB, workspaceID string) (map[string]string, error) {
	type presRow struct {
		UserID string
		State  string
	}
	var rows []presRow
	err := tx.Raw(`
		SELECT DISTINCT ON (user_id) user_id, state
		FROM agent_presence_intervals
		WHERE workspace_id = ? AND ended_at IS NULL
		ORDER BY user_id, started_at DESC
	`, workspaceID).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, p := range rows {
		out[p.UserID] = p.State
	}
	return out, nil
}

func aiMemberRowsTX(tx *gorm.DB, workspaceID string, filter attendance.OverviewFilter) ([]attendance.MemberRow, error) {
	query := newSQLQuery().add(`
		SELECT s.agent_id::text AS actor_id,
			COALESCE(NULLIF(a.name, ''), s.agent_id::text) AS display_name,
			COUNT(*) FILTER (WHERE s.outcome = '' OR s.ended_at IS NULL) AS open_count,
			COUNT(*) FILTER (WHERE s.outcome = 'contained' OR s.outcome = 'handed_off') AS resolved_count,
			COUNT(*) AS sessions
		FROM ai_attendance_sessions s
		LEFT JOIN agents a ON a.id::text = s.agent_id
		WHERE s.workspace_id = ?::uuid
	`, workspaceID)
	query.addQuery(aiSessionScope("s.", filter))
	query.add(" GROUP BY s.agent_id, a.name ORDER BY sessions DESC")

	type aiRow struct {
		ActorID       string
		DisplayName   string
		OpenCount     int64
		ResolvedCount int64
		Sessions      int64
	}
	sql, args := query.build()
	var rows []aiRow
	if err := tx.Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]attendance.MemberRow, 0, len(rows))
	for _, ar := range rows {
		resPct := float64(0)
		if ar.Sessions > 0 {
			resPct = math.Round(float64(ar.ResolvedCount)/float64(ar.Sessions)*10000) / 100
		}
		out = append(out, attendance.MemberRow{
			ActorID:       "ai:" + ar.ActorID,
			ActorKind:     attendance.ActorKindAI,
			DisplayName:   ar.DisplayName + " (IA)",
			Presence:      "online",
			ResolutionPct: resPct,
			Open:          ar.OpenCount,
			Resolved:      ar.ResolvedCount,
		})
	}
	return out, nil
}

func aiSessionScope(prefix string, filter attendance.OverviewFilter) *sqlQuery {
	query := newSQLQuery()
	if filter.DateFrom != nil {
		query.add(" AND "+prefix+"started_at >= ?", *filter.DateFrom)
	}
	if filter.DateTo != nil {
		query.add(" AND "+prefix+"started_at <= ?", *filter.DateTo)
	}
	if campaign := strings.TrimSpace(filter.CampaignID); campaign != "" {
		query.add(" AND "+prefix+"campaign_id = ?", campaign)
	}
	if channel := strings.TrimSpace(filter.Channel); channel != "" {
		query.add(" AND "+prefix+"channel = ?", channel)
	}
	return query
}

func avgMessagesPerConversation(totalMessages, conversations int64) *float64 {
	if conversations <= 0 || totalMessages < 0 {
		return nil
	}
	avg := math.Round(float64(totalMessages)/float64(conversations)*100) / 100
	return &avg
}
