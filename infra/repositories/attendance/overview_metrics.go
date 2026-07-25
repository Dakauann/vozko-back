package attendance_repository

import (
	"math"
	"strings"

	"gorm.io/gorm"

	"vozko/domain/attendance"
)

// overviewFillExtendedTX enriches Overview with FRT, AI, channel mix, messaging,
// unassigned backlog, and reopen rates. Queue and occupancy are filled by the
// use case from dedicated ports (clean separation).
//
// msgTmp is the pre-aggregated per-entry message table (one scan of
// conversation_messages for the whole overview request).
func overviewFillExtendedTX(
	tx *gorm.DB,
	workspaceID string,
	_ string, // base (scoped CTE) retained for call-site stability; KPIs use msgTmp
	_ []interface{},
	msgTmp string,
	filter attendance.OverviewFilter,
	out *attendance.Overview,
) error {
	// Unassigned + channel mix: ENGAGED only (shells are not live-queue work).
	type scopeAgg struct {
		Unassigned int64
		Whatsapp   int64
	}
	var sa scopeAgg
	scopeSQL := `
		SELECT
			COUNT(*) FILTER (
				WHERE total_msgs > 0
				  AND assigned_user_id = ''
				  AND status_bucket <> 'finished'
			) AS unassigned,
			COUNT(*) FILTER (WHERE total_msgs > 0 AND entry_type = 'whatsapp') AS whatsapp
		FROM ` + msgTmp
	if err := tx.Raw(scopeSQL).Scan(&sa).Error; err != nil {
		return err
	}
	out.KPIs.UnassignedBacklog = sa.Unassigned
	totalCh := sa.Whatsapp
	out.ChannelMix = make([]attendance.ChannelSlice, 0, 2)
	if totalCh > 0 {
		if sa.Whatsapp > 0 {
			out.ChannelMix = append(out.ChannelMix, attendance.ChannelSlice{
				Channel: "whatsapp",
				Count:   sa.Whatsapp,
				Pct:     math.Round(float64(sa.Whatsapp)/float64(totalCh)*10000) / 100,
			})
		}
	}

	// Messaging KPIs: primary averages over engaged; dual denom over all scoped.
	// Templates are part of outbound AND exposed separately for ops / billing view.
	msgSQL := `
		SELECT
			AVG(total_msgs::float) FILTER (WHERE total_msgs > 0) AS avg_total,
			AVG(inbound_msgs::float) FILTER (WHERE total_msgs > 0) AS avg_inbound,
			AVG(outbound_msgs::float) FILTER (WHERE total_msgs > 0) AS avg_outbound,
			AVG(template_msgs::float) FILTER (WHERE total_msgs > 0) AS avg_template,
			AVG(total_msgs::float) AS avg_all_scoped,
			COALESCE(SUM(template_msgs), 0)::bigint AS template_total,
			COUNT(*) FILTER (WHERE total_msgs > 0) AS with_msgs,
			COUNT(*) FILTER (WHERE template_msgs > 0) AS with_template
		FROM ` + msgTmp
	type msgRow struct {
		AvgTotal      *float64
		AvgInbound    *float64
		AvgOutbound   *float64
		AvgTemplate   *float64
		AvgAllScoped  *float64
		TemplateTotal int64
		WithMsgs      int64
		WithTemplate  int64
	}
	var mr msgRow
	if err := tx.Raw(msgSQL).Scan(&mr).Error; err != nil {
		return err
	}
	out.Messaging = attendance.OverviewMessaging{
		ConversationsWithMessages: mr.WithMsgs,
		ConversationsWithTemplate: mr.WithTemplate,
		TemplateMessages:          mr.TemplateTotal,
		Available:                 mr.WithMsgs > 0 || mr.TemplateTotal > 0,
	}
	if mr.AvgTotal != nil {
		v := math.Round(*mr.AvgTotal*100) / 100
		out.Messaging.AvgMessagesPerConversation = &v
	}
	if mr.AvgInbound != nil {
		v := math.Round(*mr.AvgInbound*100) / 100
		out.Messaging.AvgInbound = &v
	}
	if mr.AvgOutbound != nil {
		v := math.Round(*mr.AvgOutbound*100) / 100
		out.Messaging.AvgOutbound = &v
	}
	if mr.AvgTemplate != nil {
		v := math.Round(*mr.AvgTemplate*100) / 100
		out.Messaging.AvgTemplate = &v
	}
	if mr.AvgAllScoped != nil {
		v := math.Round(*mr.AvgAllScoped*100) / 100
		out.Messaging.AvgMessagesAllScoped = &v
	}

	// FRT / AI / reopen use their own tables (not scoped_entries CTE) — still on same tx.
	sf := attendance.StatsFilter{
		DateFrom:     filter.DateFrom,
		DateTo:       filter.DateTo,
		CampaignID:   filter.CampaignID,
		CampaignType: filter.CampaignType,
	}
	// GetFRTStats uses r.db; call via raw on tx for consistency when possible.
	// Reuse package-level FRT through a lightweight reimplementation on tx.
	frt, err := overviewFRTStatsTX(tx, workspaceID, sf)
	if err != nil {
		return err
	}
	out.FRT = attendance.OverviewFRT{
		SampleCount:  frt.SampleCount,
		HumanSamples: frt.HumanSamples,
		AISamples:    frt.AISamples,
		Available:    frt.SampleCount > 0,
	}
	if frt.SampleCount > 0 {
		avg := frt.AvgFRTMins
		med := frt.MedianFRTMins
		out.FRT.AvgMins = &avg
		out.FRT.MedianMins = &med
		out.KPIs.AvgFRTMins = &avg
	}
	if frt.HumanSamples > 0 {
		v := frt.HumanAvgMins
		out.FRT.HumanAvgMins = &v
	}
	if frt.AISamples > 0 {
		v := frt.AIAvgMins
		out.FRT.AIAvgMins = &v
	}

	aiSQL := `
		SELECT
			COUNT(*) FILTER (WHERE ended_at IS NOT NULL) AS sessions,
			COUNT(*) FILTER (WHERE ended_at IS NULL) AS open_sessions,
			COUNT(*) FILTER (WHERE outcome = 'contained') AS contained,
			COUNT(*) FILTER (WHERE outcome = 'handed_off') AS handed_off,
			COUNT(*) FILTER (WHERE outcome = 'abandoned') AS abandoned,
			COALESCE(AVG(ai_message_count) FILTER (WHERE ended_at IS NOT NULL), 0) AS avg_ai_messages
		FROM ai_attendance_sessions
		WHERE workspace_id = ?::uuid
	`
	aiArgs := []interface{}{workspaceID}
	if filter.DateFrom != nil {
		aiSQL += " AND started_at >= ?"
		aiArgs = append(aiArgs, *filter.DateFrom)
	}
	if filter.DateTo != nil {
		aiSQL += " AND started_at <= ?"
		aiArgs = append(aiArgs, *filter.DateTo)
	}
	if strings.TrimSpace(filter.CampaignID) != "" {
		aiSQL += " AND campaign_id = ?"
		aiArgs = append(aiArgs, strings.TrimSpace(filter.CampaignID))
	}
	if filter.Channel == "whatsapp" {
		aiSQL += " AND channel = 'whatsapp'"
	}
	type aiAgg struct {
		Sessions      int64
		OpenSessions  int64
		Contained     int64
		HandedOff     int64
		Abandoned     int64
		AvgAIMessages float64
	}
	var aa aiAgg
	if err := tx.Raw(aiSQL, aiArgs...).Scan(&aa).Error; err != nil {
		out.AI = attendance.OverviewAI{Available: false}
	} else {
		out.AI = attendance.OverviewAI{
			Sessions:      aa.Sessions,
			OpenSessions:  aa.OpenSessions,
			Contained:     aa.Contained,
			HandedOff:     aa.HandedOff,
			Abandoned:     aa.Abandoned,
			AvgAIMessages: math.Round(aa.AvgAIMessages*100) / 100,
			Available:     aa.Sessions+aa.OpenSessions > 0,
		}
		if aa.Sessions > 0 {
			out.AI.ContainmentRate = math.Round(float64(aa.Contained)/float64(aa.Sessions)*10000) / 100
			out.AI.HandoffRate = math.Round(float64(aa.HandedOff)/float64(aa.Sessions)*10000) / 100
		}
	}

	reopenSQL := `
		SELECT
			COUNT(*) FILTER (WHERE event_type = 'reopened') AS reopened,
			COUNT(*) FILTER (WHERE event_type = 'finished') AS finished_ev
		FROM conversation_events
		WHERE workspace_id = ?::uuid
	`
	reArgs := []interface{}{workspaceID}
	if filter.DateFrom != nil {
		reopenSQL += " AND created_at >= ?"
		reArgs = append(reArgs, *filter.DateFrom)
	}
	if filter.DateTo != nil {
		reopenSQL += " AND created_at <= ?"
		reArgs = append(reArgs, *filter.DateTo)
	}
	type reRow struct {
		Reopened   int64
		FinishedEv int64
	}
	var rr reRow
	if err := tx.Raw(reopenSQL, reArgs...).Scan(&rr).Error; err != nil {
		out.Reopen = attendance.OverviewReopen{Available: false}
	} else {
		// Rate denominator = engaged finished (same as KPIs.Finished), not event telemetry.
		finKPI := out.KPIs.Finished
		out.Reopen = attendance.OverviewReopen{
			ReopenedCount:      rr.Reopened,
			FinishedCount:      finKPI,
			FinishedEventCount: rr.FinishedEv,
			Available:          rr.Reopened+finKPI+rr.FinishedEv > 0,
		}
		if finKPI > 0 {
			v := math.Round(float64(rr.Reopened)/float64(finKPI)*10000) / 100
			out.Reopen.ReopenRate = &v
		}
	}

	// Finished by close_source on the same ENGAGED set as KPIs.Finished.
	// human = explicit human OR empty (legacy hand close after backfill still human).
	// system = idle auto-close; ai = finish tool. Not queue abandon.
	closeSQL := `
		SELECT
			COUNT(*) FILTER (
				WHERE total_msgs > 0
				  AND status_bucket = 'finished'
				  AND (close_source = 'human' OR close_source = '' OR close_source IS NULL)
			) AS human_cnt,
			COUNT(*) FILTER (
				WHERE total_msgs > 0 AND status_bucket = 'finished' AND close_source = 'ai'
			) AS ai_cnt,
			COUNT(*) FILTER (
				WHERE total_msgs > 0 AND status_bucket = 'finished' AND close_source = 'system'
			) AS system_cnt
		FROM ` + msgTmp
	// Explicit gorm tags: bare names like "system" / "ai" map unreliably.
	type closeRow struct {
		Human  int64 `gorm:"column:human_cnt"`
		AI     int64 `gorm:"column:ai_cnt"`
		System int64 `gorm:"column:system_cnt"`
	}
	var cr closeRow
	if err := tx.Raw(closeSQL).Scan(&cr).Error; err != nil {
		out.FinishedBySource = attendance.OverviewFinishedBySource{Available: false}
	} else {
		total := cr.Human + cr.AI + cr.System
		out.FinishedBySource = attendance.OverviewFinishedBySource{
			Human:     cr.Human,
			AI:        cr.AI,
			System:    cr.System,
			Total:     total,
			Available: total > 0,
		}
		if total > 0 {
			hp := math.Round(float64(cr.Human)/float64(total)*10000) / 100
			ap := math.Round(float64(cr.AI)/float64(total)*10000) / 100
			sp := math.Round(float64(cr.System)/float64(total)*10000) / 100
			out.FinishedBySource.HumanPct = &hp
			out.FinishedBySource.AIPct = &ap
			out.FinishedBySource.SystemPct = &sp
		}
	}

	return nil
}

func overviewFRTStatsTX(tx *gorm.DB, workspaceID string, filter attendance.StatsFilter) (*attendance.FRTStats, error) {
	// LATERAL first outbound only: O(assignments) index seeks, not all messages per entry.
	query := `
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
				  AND m.message_type IN ('operator', 'ai_response')
				  AND m.created_at >= ah.started_at
				ORDER BY m.created_at ASC
				LIMIT 1
			) cm
			WHERE ah.workspace_id = ?`
	args := []interface{}{workspaceID}
	if filter.DateFrom != nil {
		query += " AND ah.started_at >= ?"
		args = append(args, *filter.DateFrom)
	}
	if filter.DateTo != nil {
		query += " AND ah.started_at <= ?"
		args = append(args, *filter.DateTo)
	}
	query += `
		) sub WHERE frt_secs IS NOT NULL AND frt_secs > 0`

	type row struct {
		ActorKind string
		FRTSecs   float64
	}
	var rows []row
	if err := tx.Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	st := &attendance.FRTStats{}
	var all []float64
	var humanSum, aiSum float64
	for _, rw := range rows {
		all = append(all, rw.FRTSecs)
		st.SampleCount++
		if rw.ActorKind == "ai" {
			st.AISamples++
			aiSum += rw.FRTSecs
		} else {
			st.HumanSamples++
			humanSum += rw.FRTSecs
		}
	}
	if st.SampleCount > 0 {
		var sum float64
		for _, v := range all {
			sum += v
		}
		st.AvgFRTMins = math.Round(sum/float64(st.SampleCount)/60*100) / 100
		for i := 0; i < len(all); i++ {
			for j := i + 1; j < len(all); j++ {
				if all[j] < all[i] {
					all[i], all[j] = all[j], all[i]
				}
			}
		}
		mid := all[len(all)/2]
		st.MedianFRTMins = math.Round(mid/60*100) / 100
	}
	if st.HumanSamples > 0 {
		st.HumanAvgMins = math.Round(humanSum/float64(st.HumanSamples)/60*100) / 100
	}
	if st.AISamples > 0 {
		st.AIAvgMins = math.Round(aiSum/float64(st.AISamples)/60*100) / 100
	}
	return st, nil
}
