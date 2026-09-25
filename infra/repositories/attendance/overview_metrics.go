package attendance_repository

import (
	"math"

	"gorm.io/gorm"

	"vozko/domain/attendance"
	"vozko/domain/conversation_event"
	"vozko/infra/database"
)

func overviewFillExtendedTX(
	tx *gorm.DB,
	workspaceID string,
	msgTmp string,
	filter attendance.OverviewFilter,
	out *attendance.SummarySection,
) error {
	if err := scopeMixTX(tx, msgTmp, out); err != nil {
		return err
	}
	if err := scopeMessagingTX(tx, msgTmp, out); err != nil {
		return err
	}

	frt, err := frtStatsTX(tx, workspaceID, attendance.StatsFilter{
		DateFrom:     filter.DateFrom,
		DateTo:       filter.DateTo,
		CampaignID:   filter.CampaignID,
		CampaignType: filter.CampaignType,
	})
	if err != nil {
		return err
	}
	out.FRT = overviewFRT(frt)
	out.KPIs.AvgFRTMins = out.FRT.AvgMins

	if out.AI, err = aiSessionsTX(tx, workspaceID, filter); err != nil {
		return err
	}
	if out.Reopen, err = reopenTX(tx, workspaceID, filter, out.KPIs.Finished); err != nil {
		return err
	}
	return nil
}

func scopeMixTX(tx *gorm.DB, msgTmp string, out *attendance.SummarySection) error {
	var scope struct {
		Unassigned int64
		Human      int64
		AI         int64
		System     int64
	}
	err := tx.Raw(`
		SELECT
			COUNT(*) FILTER (
				WHERE total_msgs > 0 AND assigned_user_id = '' AND status_bucket <> 'finished'
			) AS unassigned,
			COUNT(*) FILTER (
				WHERE total_msgs > 0 AND status_bucket = 'finished'
				  AND (close_source = 'human' OR close_source = '' OR close_source IS NULL)
			) AS human,
			COUNT(*) FILTER (
				WHERE total_msgs > 0 AND status_bucket = 'finished' AND close_source = 'ai'
			) AS ai,
			COUNT(*) FILTER (
				WHERE total_msgs > 0 AND status_bucket = 'finished' AND close_source = 'system'
			) AS system
		FROM ` + msgTmp).Scan(&scope).Error
	if err != nil {
		return err
	}
	out.KPIs.UnassignedBacklog = scope.Unassigned
	out.FinishedBySource = finishedBySource(scope.Human, scope.AI, scope.System)

	type channelRow struct {
		EntryType string `gorm:"column:entry_type"`
		Count     int64  `gorm:"column:cnt"`
	}
	var rows []channelRow
	err = tx.Raw(`
		SELECT entry_type, COUNT(*) AS cnt
		FROM ` + msgTmp + `
		WHERE total_msgs > 0 AND entry_type <> ''
		GROUP BY entry_type
		ORDER BY cnt DESC, entry_type ASC`).Scan(&rows).Error
	if err != nil {
		return err
	}
	var total int64
	for _, r := range rows {
		total += r.Count
	}
	out.ChannelMix = make([]attendance.ChannelSlice, 0, len(rows))
	if total == 0 {
		return nil
	}
	for _, r := range rows {
		out.ChannelMix = append(out.ChannelMix, attendance.ChannelSlice{
			Channel: r.EntryType,
			Count:   r.Count,
			Pct:     percentOf(r.Count, total),
		})
	}
	return nil
}

func finishedBySource(human, ai, system int64) attendance.OverviewFinishedBySource {
	total := human + ai + system
	out := attendance.OverviewFinishedBySource{
		Human:     human,
		AI:        ai,
		System:    system,
		Total:     total,
		Available: total > 0,
	}
	if total > 0 {
		hp, ap, sp := percentOf(human, total), percentOf(ai, total), percentOf(system, total)
		out.HumanPct, out.AIPct, out.SystemPct = &hp, &ap, &sp
	}
	return out
}

func percentOf(part, total int64) float64 {
	return math.Round(float64(part)/float64(total)*10000) / 100
}

func scopeMessagingTX(tx *gorm.DB, msgTmp string, out *attendance.SummarySection) error {
	var mr struct {
		AvgTotal      *float64
		AvgInbound    *float64
		AvgOutbound   *float64
		AvgTemplate   *float64
		AvgAllScoped  *float64
		TemplateTotal int64
		WithMsgs      int64
		WithTemplate  int64
	}
	err := tx.Raw(`
		SELECT
			AVG(total_msgs::float) FILTER (WHERE total_msgs > 0) AS avg_total,
			AVG(inbound_msgs::float) FILTER (WHERE total_msgs > 0) AS avg_inbound,
			AVG(outbound_msgs::float) FILTER (WHERE total_msgs > 0) AS avg_outbound,
			AVG(template_msgs::float) FILTER (WHERE total_msgs > 0) AS avg_template,
			AVG(total_msgs::float) AS avg_all_scoped,
			COALESCE(SUM(template_msgs), 0)::bigint AS template_total,
			COUNT(*) FILTER (WHERE total_msgs > 0) AS with_msgs,
			COUNT(*) FILTER (WHERE template_msgs > 0) AS with_template
		FROM ` + msgTmp).Scan(&mr).Error
	if err != nil {
		return err
	}
	out.Messaging = attendance.OverviewMessaging{
		ConversationsWithMessages:  mr.WithMsgs,
		ConversationsWithTemplate:  mr.WithTemplate,
		TemplateMessages:           mr.TemplateTotal,
		Available:                  mr.WithMsgs > 0 || mr.TemplateTotal > 0,
		AvgMessagesPerConversation: roundedCopy(mr.AvgTotal),
		AvgInbound:                 roundedCopy(mr.AvgInbound),
		AvgOutbound:                roundedCopy(mr.AvgOutbound),
		AvgTemplate:                roundedCopy(mr.AvgTemplate),
		AvgMessagesAllScoped:       roundedCopy(mr.AvgAllScoped),
	}
	return nil
}

func roundedCopy(value *float64) *float64 {
	if value == nil {
		return nil
	}
	v := math.Round(*value*100) / 100
	return &v
}

func overviewFRT(frt attendance.FRTStats) attendance.OverviewFRT {
	out := attendance.OverviewFRT{
		SampleCount:  frt.SampleCount,
		HumanSamples: frt.HumanSamples,
		AISamples:    frt.AISamples,
		Available:    frt.SampleCount > 0,
	}
	if frt.SampleCount > 0 {
		avg, med := frt.AvgFRTMins, frt.MedianFRTMins
		out.AvgMins, out.MedianMins = &avg, &med
	}
	if frt.HumanSamples > 0 {
		v := frt.HumanAvgMins
		out.HumanAvgMins = &v
	}
	if frt.AISamples > 0 {
		v := frt.AIAvgMins
		out.AIAvgMins = &v
	}
	return out
}

func aiSessionsTX(tx *gorm.DB, workspaceID string, filter attendance.OverviewFilter) (attendance.OverviewAI, error) {
	query := newSQLQuery().add(`
		SELECT
			COUNT(*) FILTER (WHERE ended_at IS NOT NULL) AS sessions,
			COUNT(*) FILTER (WHERE ended_at IS NULL) AS open_sessions,
			COUNT(*) FILTER (WHERE outcome = 'contained') AS contained,
			COUNT(*) FILTER (WHERE outcome = 'handed_off') AS handed_off,
			COUNT(*) FILTER (WHERE outcome = 'abandoned') AS abandoned,
			COALESCE(AVG(ai_message_count) FILTER (WHERE ended_at IS NOT NULL), 0) AS avg_ai_messages
		FROM ai_attendance_sessions
		WHERE workspace_id = ?::uuid
	`, workspaceID)
	query.addQuery(aiSessionScope("", filter))

	var aa struct {
		Sessions      int64
		OpenSessions  int64
		Contained     int64
		HandedOff     int64
		Abandoned     int64
		AvgAIMessages float64
	}
	sql, args := query.build()
	if err := tx.Raw(sql, args...).Scan(&aa).Error; err != nil {
		return attendance.OverviewAI{}, err
	}
	out := attendance.OverviewAI{
		Sessions:      aa.Sessions,
		OpenSessions:  aa.OpenSessions,
		Contained:     aa.Contained,
		HandedOff:     aa.HandedOff,
		Abandoned:     aa.Abandoned,
		AvgAIMessages: math.Round(aa.AvgAIMessages*100) / 100,
		Available:     aa.Sessions+aa.OpenSessions > 0,
	}
	if aa.Sessions > 0 {
		out.ContainmentRate = percentOf(aa.Contained, aa.Sessions)
		out.HandoffRate = percentOf(aa.HandedOff, aa.Sessions)
	}
	return out, nil
}

func reopenCountsQuery(workspaceID string, filter attendance.OverviewFilter) *sqlQuery {
	reopened := string(conversation_event.EventReopened)
	finished := string(conversation_event.EventFinished)
	query := newSQLQuery().add(`
		SELECT
			COUNT(*) FILTER (WHERE event_type = ?) AS reopened,
			COUNT(*) FILTER (WHERE event_type = ?) AS finished_ev
		FROM conversation_events
		WHERE workspace_id = ?::uuid
		  AND event_type IN (?, ?)
	`, reopened, finished, workspaceID, reopened, finished)
	if filter.DateFrom != nil {
		query.add(" AND created_at >= ?", *filter.DateFrom)
	}
	if filter.DateTo != nil {
		query.add(" AND created_at <= ?", *filter.DateTo)
	}
	return query
}

func reopenTX(
	tx *gorm.DB,
	workspaceID string,
	filter attendance.OverviewFilter,
	finished int64,
) (attendance.OverviewReopen, error) {
	var rr struct {
		Reopened   int64
		FinishedEv int64
	}
	sql, args := reopenCountsQuery(workspaceID, filter).build()
	if err := tx.Raw(sql, args...).Scan(&rr).Error; err != nil {
		return attendance.OverviewReopen{}, err
	}
	out := attendance.OverviewReopen{
		ReopenedCount:      rr.Reopened,
		FinishedCount:      finished,
		FinishedEventCount: rr.FinishedEv,
		Available:          rr.Reopened+finished+rr.FinishedEv > 0,
	}
	if finished > 0 {
		v := percentOf(rr.Reopened, finished)
		out.ReopenRate = &v
	}
	return out, nil
}

func frtSamplesQuery(workspaceID string, filter attendance.StatsFilter) *sqlQuery {
	query := newSQLQuery().add(`
		SELECT actor_kind, frt_secs FROM (
			SELECT ah.actor_kind,
				EXTRACT(EPOCH FROM (cm.created_at - ah.started_at)) AS frt_secs
			FROM assignment_history ah
			CROSS JOIN LATERAL (
				SELECT m.created_at
				FROM conversation_messages m
				WHERE m.entry_id = ah.entry_id
				  AND m.entry_type = ah.entry_type
				  AND m.deleted_at IS NULL
				  AND `+database.SentAsReplySQL("m")+`
				  AND m.created_at >= ah.started_at
				ORDER BY m.created_at ASC
				LIMIT 1
			) cm
			WHERE ah.workspace_id = ?`, workspaceID)
	if filter.DateFrom != nil {
		query.add(" AND ah.started_at >= ?", *filter.DateFrom)
	}
	if filter.DateTo != nil {
		query.add(" AND ah.started_at <= ?", *filter.DateTo)
	}
	return query.add(`
		) sub WHERE frt_secs IS NOT NULL AND frt_secs > 0`)
}

func frtStatsTX(tx *gorm.DB, workspaceID string, filter attendance.StatsFilter) (attendance.FRTStats, error) {
	sql, args := frtSamplesQuery(workspaceID, filter).build()
	var rows []struct {
		ActorKind string
		FRTSecs   float64
	}
	if err := tx.Raw(sql, args...).Scan(&rows).Error; err != nil {
		return attendance.FRTStats{}, err
	}
	samples := make([]attendance.FRTSample, 0, len(rows))
	for _, row := range rows {
		samples = append(samples, attendance.FRTSample{ActorKind: row.ActorKind, Seconds: row.FRTSecs})
	}
	return attendance.BuildFRTStats(samples), nil
}
