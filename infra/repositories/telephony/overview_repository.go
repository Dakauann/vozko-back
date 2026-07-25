package telephony_repository

import (
	"math"
	"strings"

	"gorm.io/gorm"

	"vozko/domain/telephony"
)

type repository struct {
	db *gorm.DB
}

// New returns a telephony overview repository backed by the calls CDR table.
func New(db *gorm.DB) telephony.Repository {
	return &repository{db: db}
}

func (r *repository) GetOverview(workspaceID string, filter telephony.OverviewFilter) (*telephony.Overview, error) {
	slSec := filter.ServiceLevelSeconds
	if slSec <= 0 {
		slSec = telephony.DefaultServiceLevelSeconds
	}

	out := &telephony.Overview{
		Filter:       filter,
		Hourly:       make([]telephony.HourlyPoint, 24),
		Definitions:  telephony.DefaultDefinitions(),
		SLAAvailable: true,
	}
	out.KPIs.ServiceLevelSeconds = slSec
	for h := 0; h < 24; h++ {
		out.Hourly[h] = telephony.HourlyPoint{Hour: h}
	}
	if strings.TrimSpace(workspaceID) == "" {
		return out, nil
	}

	where, args := buildCallWhere(workspaceID, filter)

	// ── KPI counts ─────────────────────────────────────────────────────
	type kpiRow struct {
		Total      int64
		Answered   int64
		Failed     int64
		Abandoned  int64
		Inbound    int64
		Outbound   int64
		CRM        int64
		TrunkIn    int64
		TrunkOut   int64
		WithinSL   int64
		ShortAband int64
	}
	var kr kpiRow
	kpiSQL := `
		SELECT
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE answered_at IS NOT NULL) AS answered,
			COUNT(*) FILTER (WHERE status = 'failed') AS failed,
			COUNT(*) FILTER (WHERE status = 'abandoned') AS abandoned,
			COUNT(*) FILTER (WHERE direction = 'inbound') AS inbound,
			COUNT(*) FILTER (WHERE direction = 'outbound') AS outbound,
			COUNT(*) FILTER (WHERE type = 'crm') AS crm,
			COUNT(*) FILTER (WHERE type = 'trunk_inbound') AS trunk_in,
			COUNT(*) FILTER (WHERE type = 'trunk_outbound') AS trunk_out,
			COUNT(*) FILTER (
				WHERE answered_at IS NOT NULL
				  AND EXTRACT(EPOCH FROM (answered_at - started_at)) <= ?
			) AS within_sl,
			COUNT(*) FILTER (
				WHERE answered_at IS NOT NULL AND ended_at IS NOT NULL
				  AND EXTRACT(EPOCH FROM (ended_at - answered_at)) < 6
			) AS short_aband
		FROM calls
		WHERE ` + where
	kpiArgs := append([]interface{}{float64(slSec)}, args...)
	if err := r.db.Raw(kpiSQL, kpiArgs...).Scan(&kr).Error; err != nil {
		return nil, err
	}
	out.KPIs.TotalCalls = kr.Total
	out.KPIs.Answered = kr.Answered
	out.KPIs.Failed = kr.Failed
	out.KPIs.Abandoned = kr.Abandoned
	out.KPIs.Inbound = kr.Inbound
	out.KPIs.Outbound = kr.Outbound
	out.KPIs.HumanCRMCalls = kr.CRM
	out.KPIs.TrunkInbound = kr.TrunkIn
	out.KPIs.TrunkOutbound = kr.TrunkOut
	out.KPIs.AnsweredWithinSL = kr.WithinSL
	out.KPIs.ShortAbandons = kr.ShortAband
	if kr.Total > 0 {
		v := math.Round(float64(kr.Answered)/float64(kr.Total)*10000) / 100
		out.KPIs.ConnectRate = &v
		ab := math.Round(float64(kr.Abandoned)/float64(kr.Total)*10000) / 100
		out.KPIs.CDRAbandonRate = &ab
	}
	if kr.Answered > 0 {
		sl := math.Round(float64(kr.WithinSL)/float64(kr.Answered)*10000) / 100
		out.KPIs.ServiceLevelPct = &sl
	}

	// ── Time averages ──────────────────────────────────────────────────
	type timeRow struct {
		AvgRingSecs   *float64
		AvgTalkSecs   *float64
		AvgHandleSecs *float64
	}
	var tr timeRow
	timeSQL := `
		SELECT
			AVG(EXTRACT(EPOCH FROM (answered_at - started_at)))
				FILTER (WHERE answered_at IS NOT NULL) AS avg_ring_secs,
			AVG(EXTRACT(EPOCH FROM (ended_at - answered_at)))
				FILTER (WHERE answered_at IS NOT NULL AND ended_at IS NOT NULL) AS avg_talk_secs,
			AVG(duration_sec::float)
				FILTER (WHERE answered_at IS NOT NULL AND duration_sec > 0) AS avg_handle_secs
		FROM calls
		WHERE ` + where
	if err := r.db.Raw(timeSQL, args...).Scan(&tr).Error; err != nil {
		return nil, err
	}
	out.KPIs.AvgRingMins = secsToMins(tr.AvgRingSecs)
	out.KPIs.AvgTalkMins = secsToMins(tr.AvgTalkSecs)
	out.KPIs.AvgHandleMins = secsToMins(tr.AvgHandleSecs)
	// AHT approx: prefer duration_sec (includes ring+talk); talk alone if no duration.
	if out.KPIs.AvgHandleMins != nil {
		out.KPIs.AvgAHTMins = out.KPIs.AvgHandleMins
	} else {
		out.KPIs.AvgAHTMins = out.KPIs.AvgTalkMins
	}

	// ── Transfers (conversation_events) ────────────────────────────────
	var transferCount int64
	txSQL := `
		SELECT COUNT(*) FROM conversation_events
		WHERE workspace_id = ?::uuid
		  AND event_type = 'transfer_completed'
	`
	txArgs := []interface{}{workspaceID}
	if filter.DateFrom != nil {
		txSQL += " AND created_at >= ?"
		txArgs = append(txArgs, *filter.DateFrom)
	}
	if filter.DateTo != nil {
		txSQL += " AND created_at <= ?"
		txArgs = append(txArgs, *filter.DateTo)
	}
	_ = r.db.Raw(txSQL, txArgs...).Scan(&transferCount).Error
	out.KPIs.Transfers = transferCount

	// ── Hourly starts ──────────────────────────────────────────────────
	type hourRow struct {
		Hour  int
		Count int64
	}
	var hours []hourRow
	hourSQL := `
		SELECT EXTRACT(HOUR FROM started_at)::int AS hour, COUNT(*)::bigint AS count
		FROM calls
		WHERE ` + where + `
		GROUP BY 1
	`
	if err := r.db.Raw(hourSQL, args...).Scan(&hours).Error; err != nil {
		return nil, err
	}
	for _, h := range hours {
		if h.Hour >= 0 && h.Hour < 24 {
			out.Hourly[h.Hour].Count = h.Count
		}
	}

	// ── By type ────────────────────────────────────────────────────────
	type typeRow struct {
		Type  string
		Count int64
	}
	var types []typeRow
	typeSQL := `
		SELECT type, COUNT(*)::bigint AS count
		FROM calls
		WHERE ` + where + `
		GROUP BY type
		ORDER BY count DESC
	`
	if err := r.db.Raw(typeSQL, args...).Scan(&types).Error; err != nil {
		return nil, err
	}
	out.ByType = make([]telephony.TypeSlice, 0, len(types))
	for _, t := range types {
		pct := float64(0)
		if kr.Total > 0 {
			pct = math.Round(float64(t.Count)/float64(kr.Total)*10000) / 100
		}
		out.ByType = append(out.ByType, telephony.TypeSlice{
			Type: t.Type, Count: t.Count, Pct: pct,
		})
	}

	// ── By direction ───────────────────────────────────────────────────
	out.ByDirection = make([]telephony.DirectionSlice, 0, 2)
	if kr.Inbound > 0 {
		pct := float64(0)
		if kr.Total > 0 {
			pct = math.Round(float64(kr.Inbound)/float64(kr.Total)*10000) / 100
		}
		out.ByDirection = append(out.ByDirection, telephony.DirectionSlice{
			Direction: "inbound", Count: kr.Inbound, Pct: pct,
		})
	}
	if kr.Outbound > 0 {
		pct := float64(0)
		if kr.Total > 0 {
			pct = math.Round(float64(kr.Outbound)/float64(kr.Total)*10000) / 100
		}
		out.ByDirection = append(out.ByDirection, telephony.DirectionSlice{
			Direction: "outbound", Count: kr.Outbound, Pct: pct,
		})
	}

	// ── Dispositions (status + end_reason) ──────────────────────────────
	type dispRow struct {
		Code  string
		Count int64
	}
	var drows []dispRow
	dispSQL := `
		SELECT
			CASE
				WHEN surrender_reason IS NOT NULL AND TRIM(surrender_reason) <> '' THEN 'handed_off'
				WHEN end_reason IS NOT NULL AND TRIM(end_reason) <> '' THEN TRIM(end_reason)
				WHEN status IS NOT NULL AND TRIM(status) <> '' THEN TRIM(status)
				ELSE 'unknown'
			END AS code,
			COUNT(*)::bigint AS count
		FROM calls
		WHERE ` + where + `
		GROUP BY 1
		ORDER BY count DESC
		LIMIT 20
	`
	if err := r.db.Raw(dispSQL, args...).Scan(&drows).Error; err == nil {
		out.Dispositions = make([]telephony.DispositionSlice, 0, len(drows))
		for _, d := range drows {
			pct := float64(0)
			if kr.Total > 0 {
				pct = math.Round(float64(d.Count)/float64(kr.Total)*10000) / 100
			}
			out.Dispositions = append(out.Dispositions, telephony.DispositionSlice{
				Code:  d.Code,
				Label: dispositionLabel(d.Code),
				Count: d.Count,
				Pct:   pct,
			})
		}
	}

	// ── By human member ────────────────────────────────────────────────
	memberSQL := `
		SELECT
			TRIM(agent_id::text) AS user_id,
			COUNT(*)::bigint AS total_calls,
			COUNT(*) FILTER (WHERE answered_at IS NOT NULL)::bigint AS answered,
			COUNT(*) FILTER (WHERE status = 'failed')::bigint AS failed,
			COUNT(*) FILTER (WHERE status = 'abandoned')::bigint AS abandoned,
			COUNT(*) FILTER (
				WHERE answered_at IS NOT NULL
				  AND EXTRACT(EPOCH FROM (answered_at - started_at)) <= ?
			)::bigint AS within_sl,
			AVG(EXTRACT(EPOCH FROM (ended_at - answered_at)))
				FILTER (WHERE answered_at IS NOT NULL AND ended_at IS NOT NULL) AS avg_talk_secs,
			AVG(EXTRACT(EPOCH FROM (answered_at - started_at)))
				FILTER (WHERE answered_at IS NOT NULL) AS avg_ring_secs,
			AVG(duration_sec::float)
				FILTER (WHERE answered_at IS NOT NULL AND duration_sec > 0) AS avg_handle_secs
		FROM calls
		WHERE ` + where + `
		  AND agent_id IS NOT NULL AND TRIM(agent_id::text) <> ''
		  AND type IN ('crm', 'trunk_inbound', 'trunk_outbound')
		GROUP BY TRIM(agent_id::text)
		ORDER BY total_calls DESC
		LIMIT 100
	`
	memberArgs := append([]interface{}{float64(slSec)}, args...)
	type memberRow struct {
		UserID        string
		TotalCalls    int64
		Answered      int64
		Failed        int64
		Abandoned     int64
		WithinSL      int64
		AvgTalkSecs   *float64
		AvgRingSecs   *float64
		AvgHandleSecs *float64
	}
	var mrows []memberRow
	if err := r.db.Raw(memberSQL, memberArgs...).Scan(&mrows).Error; err == nil {
		out.ByMember = make([]telephony.MemberRow, 0, len(mrows))
		for _, m := range mrows {
			row := telephony.MemberRow{
				UserID:        m.UserID,
				TotalCalls:    m.TotalCalls,
				Answered:      m.Answered,
				Failed:        m.Failed,
				Abandoned:     m.Abandoned,
				WithinSL:      m.WithinSL,
				AvgTalkMins:   secsToMins(m.AvgTalkSecs),
				AvgRingMins:   secsToMins(m.AvgRingSecs),
				AvgHandleMins: secsToMins(m.AvgHandleSecs),
			}
			if m.TotalCalls > 0 {
				row.ConnectRate = math.Round(float64(m.Answered)/float64(m.TotalCalls)*10000) / 100
			}
			if m.Answered > 0 {
				sl := math.Round(float64(m.WithinSL)/float64(m.Answered)*10000) / 100
				row.ServiceLevel = &sl
			}
			out.ByMember = append(out.ByMember, row)
		}
	}

	return out, nil
}

func buildCallWhere(workspaceID string, f telephony.OverviewFilter) (string, []interface{}) {
	parts := []string{"workspace_id = ?::uuid", "deleted_at IS NULL"}
	args := []interface{}{workspaceID}
	if f.DateFrom != nil {
		parts = append(parts, "started_at >= ?")
		args = append(args, *f.DateFrom)
	}
	if f.DateTo != nil {
		parts = append(parts, "started_at <= ?")
		args = append(args, *f.DateTo)
	}
	if f.Direction == "inbound" || f.Direction == "outbound" {
		parts = append(parts, "direction = ?")
		args = append(args, f.Direction)
	}
	if f.CallType != "" {
		parts = append(parts, "type = ?")
		args = append(args, f.CallType)
	}
	actorID := strings.TrimSpace(f.AgentID)
	if actorID == "" {
		actorID = strings.TrimSpace(f.MemberID)
	}
	if actorID != "" {
		parts = append(parts, "agent_id = ?::uuid")
		args = append(args, actorID)
	}
	return strings.Join(parts, " AND "), args
}

func secsToMins(secs *float64) *float64 {
	if secs == nil || *secs < 0 {
		return nil
	}
	v := math.Round((*secs/60)*100) / 100
	return &v
}

func dispositionLabel(code string) string {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "completed", "in_progress":
		return "Concluída / em curso"
	case "failed":
		return "Falha"
	case "abandoned":
		return "Abandonada"
	case "hangup_caller":
		return "Cliente desligou"
	case "hangup_callee":
		return "Agente desligou"
	case "end_call_tool":
		return "Encerrada pela IA"
	case "error":
		return "Erro"
	case "timeout":
		return "Timeout"
	case "surrendered", "handed_off":
		return "Transferida a humano"
	case "initiated", "ringing":
		return "Não completou"
	default:
		if code == "" {
			return "Desconhecido"
		}
		return code
	}
}
