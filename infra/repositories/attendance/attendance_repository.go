package attendance_repository

import (
	"math"

	"gorm.io/gorm"

	"vozko/domain/attendance"
)

type repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) attendance.Repository {
	return &repository{db: db}
}

// campaignJoinForEntryColumn narrows attendant stats to one container.
//
// "Campaign" is the channel's container: a WhatsApp campaign, or the account row
// for channels with none. Driving it from the channel registry means a new
// channel's per-account stats work on the day it is registered, instead of
// silently ignoring the filter and reporting the whole workspace — which is what
// the previous `default: return "", "", nil` did.
func campaignJoinForEntryColumn(filter attendance.StatsFilter, alias, entryColumn string) (joinSQL string, extraWhere string, extraArgs []interface{}) {
	if filter.CampaignID == "" {
		return "", "", nil
	}
	for _, src := range channelSources {
		if string(src.EntryType) != filter.CampaignType {
			continue
		}
		join := " JOIN " + src.EntryTable + " ON " + src.EntryAlias + ".id = " + entryColumn +
			" AND " + src.EntryAlias + ".deleted_at IS NULL"
		where := " AND " + alias + ".entry_type = '" + string(src.EntryType) + "'" +
			" AND " + src.ContainerIDColumn + " = ?"
		return join, where, []interface{}{filter.CampaignID}
	}
	return "", "", nil
}

func campaignJoinForIA(filter attendance.StatsFilter) (string, string, []interface{}) {
	return campaignJoinForEntryColumn(filter, "ia", "ia.entry_id")
}

func campaignJoinForCM(filter attendance.StatsFilter) (string, string, []interface{}) {
	return campaignJoinForEntryColumn(filter, "cm", "cm.entry_id")
}

func (r *repository) GetAttendantStats(workspaceID string, filter attendance.StatsFilter) ([]attendance.AttendantStats, error) {
	iaJoin, iaWhere, iaExtra := campaignJoinForIA(filter)
	cmJoin, cmWhere, cmExtra := campaignJoinForCM(filter)

	assignedQuery := `
		SELECT wm.user_id, u.username, u.email, wm.role,
			COUNT(ia.id) AS assigned_count
		FROM workspace_members wm
		JOIN users u ON u.id = wm.user_id
		LEFT JOIN inbox_assignments ia
			ON ia.assigned_user_id = wm.user_id AND ia.workspace_id = wm.workspace_id` + iaJoin
	assignedArgs := []interface{}{}
	assignedWhere := ""
	if iaWhere != "" {
		assignedWhere += iaWhere
		assignedArgs = append(assignedArgs, iaExtra...)
	}
	if filter.DateFrom != nil {
		assignedWhere += " AND ia.created_at >= ?"
		assignedArgs = append(assignedArgs, *filter.DateFrom)
	}
	if filter.DateTo != nil {
		assignedWhere += " AND ia.created_at <= ?"
		assignedArgs = append(assignedArgs, *filter.DateTo)
	}

	if assignedWhere != "" {

		assignedQuery = `
		SELECT wm.user_id, u.username, u.email, wm.role,
			COUNT(ia.id) AS assigned_count
		FROM workspace_members wm
		JOIN users u ON u.id = wm.user_id
		LEFT JOIN inbox_assignments ia
			ON ia.assigned_user_id = wm.user_id AND ia.workspace_id = wm.workspace_id` + iaJoin + assignedWhere
	}
	assignedQuery += `
		WHERE wm.workspace_id = ?
		GROUP BY wm.user_id, u.username, u.email, wm.role
		ORDER BY wm.role, u.username`
	assignedArgs = append(assignedArgs, workspaceID)

	type memberStatsRow struct {
		UserID        string
		Username      string
		Email         string
		Role          string
		AssignedCount int64
	}
	var memberStats []memberStatsRow
	if err := r.db.Raw(assignedQuery, assignedArgs...).Scan(&memberStats).Error; err != nil {
		return nil, err
	}

	if len(memberStats) == 0 {
		return []attendance.AttendantStats{}, nil
	}

	userIDs := make([]string, len(memberStats))
	for i, m := range memberStats {
		userIDs[i] = m.UserID
	}

	respondedQuery := `
		SELECT ia.assigned_user_id AS user_id, COUNT(DISTINCT cm.entry_id) AS responded_count
		FROM conversation_messages cm
		JOIN inbox_assignments ia ON ia.entry_id = cm.entry_id AND ia.entry_type = cm.entry_type` + cmJoin + `
		WHERE ia.workspace_id = ? AND ia.assigned_user_id IN ?
		AND cm.message_type = 'operator'
		AND cm.deleted_at IS NULL` + cmWhere
	respondedArgs := append([]interface{}{workspaceID, userIDs}, cmExtra...)
	if filter.DateFrom != nil {
		respondedQuery += " AND cm.created_at >= ?"
		respondedArgs = append(respondedArgs, *filter.DateFrom)
	}
	if filter.DateTo != nil {
		respondedQuery += " AND cm.created_at <= ?"
		respondedArgs = append(respondedArgs, *filter.DateTo)
	}
	respondedQuery += " GROUP BY ia.assigned_user_id"

	type respondedRow struct {
		UserID         string
		RespondedCount int64
	}
	var respondedRows []respondedRow
	if err := r.db.Raw(respondedQuery, respondedArgs...).Scan(&respondedRows).Error; err != nil {
		return nil, err
	}
	respondedMap := make(map[string]int64, len(respondedRows))
	for _, row := range respondedRows {
		respondedMap[row.UserID] = row.RespondedCount
	}

	// Drive from assignments (workspace filter) + LATERAL first inbound/outbound.
	// Avoids grouping all messages per entry when each entry has long history.
	avgQuery := `
		SELECT user_id, AVG(response_time_secs) AS avg_response_secs FROM (
			SELECT ia.assigned_user_id AS user_id,
				EXTRACT(EPOCH FROM (op.first_at - usr.first_at)) AS response_time_secs
			FROM inbox_assignments ia` + iaJoin + `
			CROSS JOIN LATERAL (
				SELECT m.created_at AS first_at
				FROM conversation_messages m
				WHERE m.entry_id = ia.entry_id AND m.entry_type = ia.entry_type
				  AND m.deleted_at IS NULL AND m.message_type = 'user_message'
				ORDER BY m.created_at ASC LIMIT 1
			) usr
			CROSS JOIN LATERAL (
				SELECT m.created_at AS first_at
				FROM conversation_messages m
				WHERE m.entry_id = ia.entry_id AND m.entry_type = ia.entry_type
				  AND m.deleted_at IS NULL AND m.message_type = 'operator'
				ORDER BY m.created_at ASC LIMIT 1
			) op
			WHERE ia.workspace_id = ? AND ia.assigned_user_id IN ?` + iaWhere
	avgArgs := []interface{}{workspaceID, userIDs}
	avgArgs = append(avgArgs, iaExtra...)
	if filter.DateFrom != nil {
		avgQuery += " AND ia.created_at >= ?"
		avgArgs = append(avgArgs, *filter.DateFrom)
	}
	if filter.DateTo != nil {
		avgQuery += " AND ia.created_at <= ?"
		avgArgs = append(avgArgs, *filter.DateTo)
	}
	avgQuery += `
		) sub WHERE response_time_secs > 0
		GROUP BY user_id`

	type avgRow struct {
		UserID          string
		AvgResponseSecs *float64
	}
	var avgRows []avgRow
	if err := r.db.Raw(avgQuery, avgArgs...).Scan(&avgRows).Error; err != nil {
		return nil, err
	}
	avgMap := make(map[string]float64, len(avgRows))
	for _, row := range avgRows {
		if row.AvgResponseSecs != nil && *row.AvgResponseSecs > 0 {
			avgMap[row.UserID] = *row.AvgResponseSecs
		}
	}

	results := make([]attendance.AttendantStats, len(memberStats))
	for i, m := range memberStats {
		respondedCount := respondedMap[m.UserID]
		rate := float64(0)
		if m.AssignedCount > 0 {
			rate = math.Round(float64(respondedCount)/float64(m.AssignedCount)*10000) / 100
		}
		avgMins := float64(0)
		if secs, ok := avgMap[m.UserID]; ok {
			avgMins = math.Round(secs/60*100) / 100
		}
		results[i] = attendance.AttendantStats{
			UserID:              m.UserID,
			Username:            m.Username,
			Email:               m.Email,
			Role:                m.Role,
			AssignedCount:       m.AssignedCount,
			RespondedCount:      respondedCount,
			ResponseRate:        rate,
			AvgResponseTimeMins: avgMins,
			ActorKind:           "human",
		}
	}

	return results, nil
}

func (r *repository) GetWindowStats(workspaceID string, filter attendance.StatsFilter) (*attendance.WindowStats, error) {
	type windowRow struct {
		RemainingHours float64
	}

	query := `
		SELECT EXTRACT(EPOCH FROM (lmw.last_message_at + INTERVAL '24 hours' - NOW())) / 3600.0 AS remaining_hours
		FROM lead_message_windows lmw
		JOIN whatsapp_business_phone_numbers bp ON bp.id = lmw.business_phone_id
		JOIN workspace_phone_access wpa ON wpa.phone_id = bp.id`
	args := []interface{}{}

	if filter.CampaignID != "" && filter.CampaignType == "whatsapp" {
		query += `
		JOIN whatsapp_campaign_entries wce ON wce.lead_id = lmw.lead_id
			AND wce.campaign_id = ? AND wce.deleted_at IS NULL`
		args = append(args, filter.CampaignID)
	}

	query += `
		WHERE wpa.workspace_id = ?
		AND lmw.last_message_at + INTERVAL '24 hours' > NOW()`
	args = append(args, workspaceID)

	var rows []windowRow
	if err := r.db.Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}

	var bucket0to10, bucket10to15, bucket15to20, bucket20plus int64
	for _, row := range rows {
		age := 24.0 - row.RemainingHours
		switch {
		case age < 10:
			bucket0to10++
		case age < 15:
			bucket10to15++
		case age < 20:
			bucket15to20++
		default:
			bucket20plus++
		}
	}

	return &attendance.WindowStats{
		TotalOpen: int64(len(rows)),
		Buckets: []attendance.WindowBucket{
			{Label: "0–10h", Count: bucket0to10},
			{Label: "10–15h", Count: bucket10to15},
			{Label: "15–20h", Count: bucket15to20},
			{Label: "20h+", Count: bucket20plus},
		},
	}, nil
}

func (r *repository) GetResponseTimeDistribution(workspaceID string, filter attendance.StatsFilter) (*attendance.ResponseTimeDistribution, error) {
	iaJoin, iaWhere, iaExtra := campaignJoinForIA(filter)

	// Assignments in workspace + LATERAL first messages (O(assignments), not full msg scan).
	query := `
		SELECT response_time_secs FROM (
			SELECT EXTRACT(EPOCH FROM (op.first_at - usr.first_at)) AS response_time_secs
			FROM inbox_assignments ia` + iaJoin + `
			CROSS JOIN LATERAL (
				SELECT m.created_at AS first_at
				FROM conversation_messages m
				WHERE m.entry_id = ia.entry_id AND m.entry_type = ia.entry_type
				  AND m.deleted_at IS NULL AND m.message_type = 'user_message'
				ORDER BY m.created_at ASC LIMIT 1
			) usr
			CROSS JOIN LATERAL (
				SELECT m.created_at AS first_at
				FROM conversation_messages m
				WHERE m.entry_id = ia.entry_id AND m.entry_type = ia.entry_type
				  AND m.deleted_at IS NULL AND m.message_type = 'operator'
				ORDER BY m.created_at ASC LIMIT 1
			) op
			WHERE ia.workspace_id = ?` + iaWhere
	args := []interface{}{workspaceID}
	args = append(args, iaExtra...)

	if filter.DateFrom != nil {
		query += " AND ia.created_at >= ?"
		args = append(args, *filter.DateFrom)
	}
	if filter.DateTo != nil {
		query += " AND ia.created_at <= ?"
		args = append(args, *filter.DateTo)
	}

	query += `
		) sub WHERE response_time_secs > 0`

	type row struct {
		ResponseTimeSecs float64
	}
	var rows []row
	if err := r.db.Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}

	var bucket15, bucket30, bucket60plus int64
	for _, r := range rows {
		switch {
		case r.ResponseTimeSecs <= 900:
			bucket15++
		case r.ResponseTimeSecs <= 1800:
			bucket30++
		default:
			bucket60plus++
		}
	}

	total := bucket15 + bucket30 + bucket60plus
	return &attendance.ResponseTimeDistribution{
		Total: total,
		Buckets: []attendance.ResponseTimeBucket{
			{Label: "≤ 15 min", Count: bucket15},
			{Label: "15–30 min", Count: bucket30},
			{Label: "> 30 min", Count: bucket60plus},
		},
	}, nil
}

func (r *repository) GetFRTStats(workspaceID string, filter attendance.StatsFilter) (*attendance.FRTStats, error) {
	// Ownership start → first outbound after start via LATERAL (one index seek per
	// assignment). Avoids joining every message on the entry (prod multi-k scale).
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
	if err := r.db.Raw(query, args...).Scan(&rows).Error; err != nil {
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
		// crude median
		// sort
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

func (r *repository) GetAIAgentStats(workspaceID string, filter attendance.StatsFilter) ([]attendance.AIAgentStats, error) {
	query := `
		SELECT agent_id,
			COUNT(*) AS sessions,
			COUNT(*) FILTER (WHERE outcome = 'contained') AS contained,
			COUNT(*) FILTER (WHERE outcome = 'handed_off') AS handed_off,
			COUNT(*) FILTER (WHERE outcome = 'abandoned') AS abandoned,
			COALESCE(AVG(ai_message_count), 0) AS avg_ai_messages
		FROM ai_attendance_sessions
		WHERE workspace_id = ? AND ended_at IS NOT NULL`
	args := []interface{}{workspaceID}
	if filter.DateFrom != nil {
		query += " AND started_at >= ?"
		args = append(args, *filter.DateFrom)
	}
	if filter.DateTo != nil {
		query += " AND started_at <= ?"
		args = append(args, *filter.DateTo)
	}
	query += " GROUP BY agent_id ORDER BY sessions DESC"

	type row struct {
		AgentID       string
		Sessions      int64
		Contained     int64
		HandedOff     int64
		Abandoned     int64
		AvgAIMessages float64
	}
	var rows []row
	if err := r.db.Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]attendance.AIAgentStats, len(rows))
	for i, rw := range rows {
		st := attendance.AIAgentStats{
			AgentID:       rw.AgentID,
			Sessions:      rw.Sessions,
			Contained:     rw.Contained,
			HandedOff:     rw.HandedOff,
			Abandoned:     rw.Abandoned,
			AvgAIMessages: math.Round(rw.AvgAIMessages*100) / 100,
		}
		if rw.Sessions > 0 {
			st.ContainmentRate = math.Round(float64(rw.Contained)/float64(rw.Sessions)*10000) / 100
			st.HandoffRate = math.Round(float64(rw.HandedOff)/float64(rw.Sessions)*10000) / 100
		}
		out[i] = st
	}
	return out, nil
}
