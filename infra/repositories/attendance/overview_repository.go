package attendance_repository

import (
	"fmt"
	"math"
	"strings"
	"time"

	"gorm.io/gorm"

	"vozko/domain/attendance"
)

// GetOverview builds the ops dashboard for a workspace under OverviewFilter.
// Definitions are frozen in domain/attendance/overview.go.
//
// Scale strategy (tens/hundreds of thousands of conversations):
//  1. Materialise scoped entries once (temp table + PK index).
//  2. Materialise per-entry message aggregates ONCE (single join to conversation_messages).
//     Wait / handle / messaging / member response / dept times all read that table.
//     Previously each widget re-joined messages (5× full scans on 60k+ entries).
//  3. Activity-in-period legs drive FROM messages in the date range (semi-join),
//     not EXISTS over every historical entry outside the window.
func (r *repository) GetOverview(workspaceID string, filter attendance.OverviewFilter) (*attendance.Overview, error) {
	out := &attendance.Overview{
		Filter:      filter,
		Hourly:      make([]attendance.HourlyPoint, 24),
		Definitions: attendance.DefaultDefinitions(),
		KPIs: attendance.OverviewKPIs{
			CSATAvailable: false,
			SLAAvailable:  false,
		},
	}
	for h := 0; h < 24; h++ {
		out.Hourly[h] = attendance.HourlyPoint{Hour: h}
	}
	if strings.TrimSpace(workspaceID) == "" {
		return out, nil
	}

	selectBody, args := overviewEntrySelect(workspaceID, filter)
	suffix := strings.ReplaceAll(fmt.Sprintf("%d", time.Now().UnixNano()), "-", "")
	tmp := "tmp_att_ov_" + suffix
	tmpMsg := "tmp_att_msg_" + suffix

	err := r.db.Transaction(func(tx *gorm.DB) error {
		createSQL := "CREATE TEMP TABLE " + tmp + " ON COMMIT DROP AS " + selectBody
		if err := tx.Exec(createSQL, args...).Error; err != nil {
			return err
		}
		// Nested-loop joins from messages need a real key on the temp table.
		if err := tx.Exec("CREATE INDEX " + tmp + "_pk ON " + tmp + " (entry_id, entry_type)").Error; err != nil {
			return err
		}
		_ = tx.Exec("ANALYZE " + tmp).Error

		// One pass over conversation_messages for this scope. All timing widgets use it.
		// Extra scope columns (is_new_contact, hour_bucket, close_source) enable
		// engaged-only KPIs without re-joining the entry temp table.
		msgSQL := `
			CREATE TEMP TABLE ` + tmpMsg + ` ON COMMIT DROP AS
			SELECT
				se.entry_id,
				se.entry_type,
				se.department_id,
				se.assigned_user_id,
				se.status_bucket,
				se.is_new_contact,
				se.hour_bucket,
				se.close_source,
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
			FROM ` + tmp + ` se
			LEFT JOIN conversation_messages cm
				ON cm.entry_id = se.entry_id
				AND cm.entry_type = se.entry_type
				AND cm.deleted_at IS NULL
			GROUP BY se.entry_id, se.entry_type, se.department_id, se.assigned_user_id,
				se.status_bucket, se.is_new_contact, se.hour_bucket, se.close_source
		`
		if err := tx.Exec(msgSQL).Error; err != nil {
			return err
		}
		if err := tx.Exec("CREATE INDEX " + tmpMsg + "_pk ON " + tmpMsg + " (entry_id, entry_type)").Error; err != nil {
			return err
		}
		if err := tx.Exec("CREATE INDEX " + tmpMsg + "_dept ON " + tmpMsg + " (department_id)").Error; err != nil {
			return err
		}
		if err := tx.Exec("CREATE INDEX " + tmpMsg + "_msgs ON " + tmpMsg + " (total_msgs)").Error; err != nil {
			return err
		}
		_ = tx.Exec("ANALYZE " + tmpMsg).Error

		base := "WITH scoped_entries AS (SELECT * FROM " + tmp + ") "
		var noArgs []interface{}

		// Primary attendance KPIs: ENGAGED only (total_msgs > 0).
		// Shells (0 messages) are reported as shell_backlog / total_scoped.
		type statusRow struct {
			Engaged        int64
			ShellBacklog   int64
			TotalScoped    int64
			EntriesCreated int64
			Finished       int64
			Ongoing        int64
			Pending        int64
			NewContacts    int64
		}
		var sr statusRow
		statusSQL := `
			SELECT
				COUNT(*) FILTER (WHERE total_msgs > 0) AS engaged,
				COUNT(*) FILTER (WHERE total_msgs = 0) AS shell_backlog,
				COUNT(*) AS total_scoped,
				COUNT(*) FILTER (WHERE is_new_contact) AS entries_created,
				COUNT(*) FILTER (WHERE total_msgs > 0 AND status_bucket = 'finished') AS finished,
				COUNT(*) FILTER (WHERE total_msgs > 0 AND status_bucket = 'ongoing') AS ongoing,
				COUNT(*) FILTER (WHERE total_msgs > 0 AND status_bucket = 'pending') AS pending,
				COUNT(*) FILTER (WHERE total_msgs > 0 AND is_new_contact) AS new_contacts
			FROM ` + tmpMsg
		if err := tx.Raw(statusSQL).Scan(&sr).Error; err != nil {
			return err
		}
		out.KPIs.Engaged = sr.Engaged
		out.KPIs.ShellBacklog = sr.ShellBacklog
		out.KPIs.TotalScoped = sr.TotalScoped
		out.KPIs.EntriesCreated = sr.EntriesCreated
		out.KPIs.Finished = sr.Finished
		out.KPIs.Ongoing = sr.Ongoing
		out.KPIs.Pending = sr.Pending
		out.KPIs.NewContacts = sr.NewContacts
		out.StatusDistribution = attendance.StatusDistribution{
			Finished: sr.Finished,
			Ongoing:  sr.Ongoing,
			Pending:  sr.Pending,
			Total:    sr.Finished + sr.Ongoing + sr.Pending,
		}

		waitMins, err := overviewAvgWaitMinsTX(tx, tmpMsg)
		if err != nil {
			return err
		}
		out.KPIs.AvgWaitMins = waitMins

		handleMins, err := overviewAvgHandleMinsTX(tx, tmpMsg)
		if err != nil {
			return err
		}
		out.KPIs.AvgHandleMins = handleMins

		// Hourly volume: engaged only (shell bulk-creates no longer dominate the chart).
		hourlySQL := `
			SELECT hour_bucket AS hour, COUNT(*)::bigint AS count
			FROM ` + tmpMsg + `
			WHERE total_msgs > 0
			GROUP BY hour_bucket
		`
		type hourRow struct {
			Hour  int
			Count int64
		}
		var hours []hourRow
		if err := tx.Raw(hourlySQL).Scan(&hours).Error; err != nil {
			return err
		}
		for _, h := range hours {
			if h.Hour >= 0 && h.Hour < 24 {
				out.Hourly[h.Hour].Count = h.Count
			}
		}

		deptRows, err := overviewByDepartmentTX(tx, tmpMsg)
		if err != nil {
			return err
		}
		out.ByDepartment = deptRows

		memberRows, err := overviewByMemberTX(tx, workspaceID, tmpMsg, filter)
		if err != nil {
			return err
		}
		out.ByMember = memberRows

		return overviewFillExtendedTX(tx, workspaceID, base, noArgs, tmpMsg, filter, out)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// overviewEntryCTE returns a WITH scoped_entries AS (...) prefix and its args.
func overviewEntryCTE(workspaceID string, f attendance.OverviewFilter) (string, []interface{}) {
	body, args := overviewEntrySelect(workspaceID, f)
	return `WITH scoped_entries AS (` + body + `) `, args
}

// overviewEntrySelect is the UNION body for scoped entries (no WITH wrapper).
// Columns: entry_id, entry_type, status_bucket, is_new_contact, hour_bucket,
// department_id, assigned_user_id, created_at, close_source
//
// Performance:
//   - Drive FROM campaigns filtered by workspace_id first.
//   - Created-in-range: indexable on entry.created_at.
//   - Activity-in-range (not created in range): drive FROM conversation_messages
//     with the period on cm.created_at (idx_cm_type_created_active /
//     idx_cm_entry_del_created), then join entries. Avoids EXISTS per historical row.
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

	// filterCampaignDeptMember narrows a channel branch by container, department
	// and assignee.
	//
	// "Campaign" means the channel's container: a WhatsApp campaign, or the
	// account row for channels with none. The CampaignType guard keeps a
	// voice-scoped filter from silently matching a messaging channel's ids.
	filterCampaignDeptMember := func(src channelSource) (string, []interface{}) {
		extra := ""
		var a []interface{}
		if f.CampaignID != "" {
			if f.CampaignType == "" || f.CampaignType == string(src.EntryType) {
				extra += " AND " + src.ContainerIDColumn + " = ?"
				a = append(a, f.CampaignID)
			} else {
				// The filter names a different channel's container, so this branch
				// must match nothing rather than everything.
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

	// appendCreated selects conversations CREATED in the window: the "new
	// contacts" half.
	appendCreated := func(src channelSource, whereExtra string, whereArgs []interface{}) {
		sql := `
			SELECT ` + src.projection("TRUE") + `
			FROM ` + src.ContainerTable + `
			JOIN ` + src.EntryTable + `
				ON ` + src.ContainerJoin + ` AND ` + src.EntryAlias + `.deleted_at IS NULL
			LEFT JOIN inbox_assignments ia
				ON ia.entry_id = ` + src.EntryAlias + `.id AND ia.entry_type = '` + string(src.EntryType) + `'
			WHERE ` + src.WorkspaceColumn + ` = ? AND ` + src.ContainerAlias + `.deleted_at IS NULL
			` + whereExtra
		a := []interface{}{workspaceID}
		a = append(a, whereArgs...)
		parts = append(parts, sql)
		args = append(args, a...)
	}

	// appendActivity selects conversations created BEFORE the window that were
	// messaged inside it — work an agent did on an older conversation, which
	// would otherwise vanish from the period's numbers.
	appendActivity := func(src channelSource, fcd string, fca []interface{}) {
		if from == nil && to == nil {
			return
		}
		cout, couta := createdOutsideRange(src.EntryAlias)
		sql := `
			SELECT ` + src.projection("FALSE") + `
			FROM conversation_messages cm
			JOIN ` + src.EntryTable + `
				ON ` + src.EntryAlias + `.id = cm.entry_id AND ` + src.EntryAlias + `.deleted_at IS NULL
			JOIN ` + src.ContainerTable + `
				ON ` + src.ContainerJoin + ` AND ` + src.ContainerAlias + `.deleted_at IS NULL
			LEFT JOIN inbox_assignments ia
				ON ia.entry_id = ` + src.EntryAlias + `.id AND ia.entry_type = '` + string(src.EntryType) + `'
			WHERE cm.entry_type = '` + string(src.EntryType) + `'
			  AND cm.deleted_at IS NULL
			  AND ` + src.WorkspaceColumn + ` = ?
		`
		a := []interface{}{workspaceID}
		if from != nil {
			sql += " AND cm.created_at >= ?"
			a = append(a, *from)
		}
		if to != nil {
			sql += " AND cm.created_at <= ?"
			a = append(a, *to)
		}
		sql += " AND " + cout + fcd
		a = append(a, couta...)
		a = append(a, fca...)
		sql += `
			GROUP BY ` + src.groupByColumns() + `
		`
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
		return `
			SELECT NULL::uuid AS entry_id, ''::text AS entry_type, 'pending'::text AS status_bucket,
				FALSE AS is_new_contact, 0 AS hour_bucket, ''::text AS department_id,
				''::text AS assigned_user_id, NOW() AS created_at, ''::text AS close_source
			WHERE FALSE
		`, nil
	}

	return strings.Join(parts, " UNION ALL "), args
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
	// Engaged only: shells inflate pending and hide real department workload.
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
			COUNT(*) FILTER (WHERE m.status_bucket = 'pending') AS pending
		FROM ` + msgTmp + ` m
		LEFT JOIN workspace_departments wd ON wd.id::text = m.department_id
		WHERE m.total_msgs > 0
		GROUP BY m.department_id, wd.name
		ORDER BY finished DESC, ongoing DESC
	`
	type row struct {
		DepartmentID   string `gorm:"column:department_id"`
		DepartmentName string `gorm:"column:department_name"`
		Finished       int64  `gorm:"column:finished"`
		FinishedHuman  int64  `gorm:"column:finished_human"`
		FinishedAI     int64  `gorm:"column:finished_ai"`
		FinishedSystem int64  `gorm:"column:finished_system"`
		Ongoing        int64  `gorm:"column:ongoing"`
		Pending        int64  `gorm:"column:pending"`
	}
	var rows []row
	if err := tx.Raw(sql).Scan(&rows).Error; err != nil {
		return nil, err
	}

	// Wait/handle from pre-aggregated message table (engaged rows only for wait samples).
	waitSQL := `
		SELECT department_id,
			AVG(EXTRACT(EPOCH FROM (first_agent_at - first_inbound_at)))
				FILTER (WHERE first_agent_at IS NOT NULL AND first_inbound_at IS NOT NULL
				            AND first_agent_at >= first_inbound_at) AS avg_wait,
			AVG(EXTRACT(EPOCH FROM (last_agent_at - first_agent_at)))
				FILTER (WHERE status_bucket = 'finished'
				            AND first_agent_at IS NOT NULL AND last_agent_at IS NOT NULL
				            AND last_agent_at >= first_agent_at) AS avg_handle
		FROM ` + msgTmp + `
		WHERE total_msgs > 0
		GROUP BY department_id
	`
	type whRow struct {
		DepartmentID string
		AvgWait      *float64
		AvgHandle    *float64
	}
	var wh []whRow
	_ = tx.Raw(waitSQL).Scan(&wh)
	whMap := map[string]whRow{}
	for _, w := range wh {
		whMap[w.DepartmentID] = w
	}

	out := make([]attendance.DepartmentRow, 0, len(rows))
	for _, rw := range rows {
		dr := attendance.DepartmentRow{
			DepartmentID:   rw.DepartmentID,
			DepartmentName: rw.DepartmentName,
			Finished:       rw.Finished,
			FinishedHuman:  rw.FinishedHuman,
			FinishedAI:     rw.FinishedAI,
			FinishedSystem: rw.FinishedSystem,
			Ongoing:        rw.Ongoing,
			Pending:        rw.Pending,
		}
		if w, ok := whMap[rw.DepartmentID]; ok {
			if w.AvgWait != nil && *w.AvgWait >= 0 {
				v := math.Round((*w.AvgWait/60)*100) / 100
				dr.AvgWaitMins = &v
			}
			if w.AvgHandle != nil && *w.AvgHandle >= 0 {
				v := math.Round((*w.AvgHandle/60)*100) / 100
				dr.AvgHandleMins = &v
			}
		}
		out = append(out, dr)
	}
	return out, nil
}

func overviewByMemberTX(tx *gorm.DB, workspaceID, msgTmp string, filter attendance.OverviewFilter) ([]attendance.MemberRow, error) {
	// Engaged assigned entries only — shells without messages are not agent workload.
	sql := `
		SELECT
			m.assigned_user_id AS actor_id,
			'human'::text AS actor_kind,
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
			) AS finished_system
		FROM ` + msgTmp + ` m
		LEFT JOIN users u ON u.id::text = m.assigned_user_id
		WHERE m.assigned_user_id <> ''
		  AND m.total_msgs > 0
		GROUP BY m.assigned_user_id, u.username, u.email
		ORDER BY resolved_count DESC, open_count DESC
	`
	type row struct {
		ActorID        string `gorm:"column:actor_id"`
		ActorKind      string `gorm:"column:actor_kind"`
		DisplayName    string `gorm:"column:display_name"`
		Email          string `gorm:"column:email"`
		OpenCount      int64  `gorm:"column:open_count"`
		PendingCount   int64  `gorm:"column:pending_count"`
		ResolvedCount  int64  `gorm:"column:resolved_count"`
		FinishedHuman  int64  `gorm:"column:finished_human"`
		FinishedAI     int64  `gorm:"column:finished_ai"`
		FinishedSystem int64  `gorm:"column:finished_system"`
	}
	var rows []row
	if err := tx.Raw(sql).Scan(&rows).Error; err != nil {
		return nil, err
	}

	// Assignee first-response from pre-aggregated message table (engaged).
	respSQL := `
		SELECT assigned_user_id AS actor_id,
			AVG(EXTRACT(EPOCH FROM (first_assignee_op_at - first_inbound_at))) AS avg_secs
		FROM ` + msgTmp + `
		WHERE assigned_user_id <> ''
		  AND total_msgs > 0
		  AND first_assignee_op_at IS NOT NULL
		  AND first_inbound_at IS NOT NULL
		  AND first_assignee_op_at >= first_inbound_at
		GROUP BY assigned_user_id
	`
	type respRow struct {
		ActorID string
		AvgSecs *float64
	}
	var resps []respRow
	_ = tx.Raw(respSQL).Scan(&resps)
	respMap := map[string]*float64{}
	for _, rp := range resps {
		if rp.AvgSecs != nil && *rp.AvgSecs >= 0 {
			v := math.Round((*rp.AvgSecs/60)*100) / 100
			respMap[rp.ActorID] = &v
		}
	}

	presSQL := `
		SELECT DISTINCT ON (user_id) user_id, state
		FROM agent_presence_intervals
		WHERE workspace_id = ? AND ended_at IS NULL
		ORDER BY user_id, started_at DESC
	`
	type presRow struct {
		UserID string
		State  string
	}
	var pres []presRow
	_ = tx.Raw(presSQL, workspaceID).Scan(&pres)
	presMap := map[string]string{}
	for _, p := range pres {
		presMap[p.UserID] = p.State
	}

	out := make([]attendance.MemberRow, 0, len(rows))
	for _, rw := range rows {
		total := rw.OpenCount + rw.PendingCount + rw.ResolvedCount
		resPct := float64(0)
		if total > 0 {
			resPct = math.Round(float64(rw.ResolvedCount)/float64(total)*10000) / 100
		}
		presence := "offline"
		if s, ok := presMap[rw.ActorID]; ok && s != "" {
			presence = s
		}
		out = append(out, attendance.MemberRow{
			ActorID:         rw.ActorID,
			ActorKind:       "human",
			DisplayName:     rw.DisplayName,
			Email:           rw.Email,
			Presence:        presence,
			AvgResponseMins: respMap[rw.ActorID],
			Rating:          nil,
			ResolutionPct:   resPct,
			Open:            rw.OpenCount,
			Pending:         rw.PendingCount,
			Resolved:        rw.ResolvedCount,
			FinishedHuman:   rw.FinishedHuman,
			FinishedAI:      rw.FinishedAI,
			FinishedSystem:  rw.FinishedSystem,
		})
	}

	if filter.IncludeAI {
		aiSQL := `
			SELECT s.agent_id::text AS actor_id,
				COALESCE(NULLIF(a.name, ''), s.agent_id::text) AS display_name,
				COUNT(*) FILTER (WHERE s.outcome = '' OR s.ended_at IS NULL) AS open_count,
				COUNT(*) FILTER (WHERE s.outcome = 'contained' OR s.outcome = 'handed_off') AS resolved_count,
				COUNT(*) FILTER (WHERE s.outcome = 'handed_off') AS handed_off,
				COUNT(*) AS sessions
			FROM ai_attendance_sessions s
			LEFT JOIN agents a ON a.id::text = s.agent_id
			WHERE s.workspace_id = ?::uuid
		`
		aiArgs := []interface{}{workspaceID}
		if filter.DateFrom != nil {
			aiSQL += " AND s.started_at >= ?"
			aiArgs = append(aiArgs, *filter.DateFrom)
		}
		if filter.DateTo != nil {
			aiSQL += " AND s.started_at <= ?"
			aiArgs = append(aiArgs, *filter.DateTo)
		}
		if strings.TrimSpace(filter.CampaignID) != "" {
			aiSQL += " AND s.campaign_id = ?"
			aiArgs = append(aiArgs, strings.TrimSpace(filter.CampaignID))
		}
		// The AI-session channel column holds the entry type, so any messaging
		// channel filters correctly. It used to match only the literal "whatsapp",
		// which meant filtering the page to Instagram silently returned WhatsApp's AI
		// numbers alongside it.
		if ch := strings.TrimSpace(filter.Channel); ch != "" {
			aiSQL += " AND s.channel = ?"
			aiArgs = append(aiArgs, ch)
		}
		aiSQL += " GROUP BY s.agent_id, a.name ORDER BY sessions DESC"
		type aiRow struct {
			ActorID       string
			DisplayName   string
			OpenCount     int64
			ResolvedCount int64
			HandedOff     int64
			Sessions      int64
		}
		var aiRows []aiRow
		if err := tx.Raw(aiSQL, aiArgs...).Scan(&aiRows).Error; err == nil {
			for _, ar := range aiRows {
				resPct := float64(0)
				if ar.Sessions > 0 {
					resPct = math.Round(float64(ar.ResolvedCount)/float64(ar.Sessions)*10000) / 100
				}
				out = append(out, attendance.MemberRow{
					ActorID:       "ai:" + ar.ActorID,
					ActorKind:     "ai",
					DisplayName:   ar.DisplayName + " (IA)",
					Presence:      "online",
					ResolutionPct: resPct,
					Open:          ar.OpenCount,
					Pending:       0,
					Resolved:      ar.ResolvedCount,
				})
			}
		}
	}

	return out, nil
}
