package whatsappform_repository

import (
	"context"
	"errors"
	"sort"
	"strings"

	"vozko/domain/whatsapp/messagestatus"
	"vozko/domain/whatsappform"
	"vozko/infra/database"
	"vozko/infra/database/sqlexpr"
)

var (
	deliveredStatuses = sqlexpr.LiteralList([]string{string(messagestatus.StatusDelivered), string(messagestatus.StatusRead)})
	readStatus        = sqlexpr.Literal(string(messagestatus.StatusRead))
)

var sendStatsSelectSQL = "SELECT s.origin, to_char(s.created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD') AS day, s.status, COUNT(*) AS sends," +
	" COUNT(*) FILTER (WHERE EXISTS (SELECT 1 FROM whatsapp_message_status_events e WHERE e.message_id = s.message_id AND e.status IN (" + deliveredStatuses + "))) AS delivered," +
	" COUNT(*) FILTER (WHERE EXISTS (SELECT 1 FROM whatsapp_message_status_events e WHERE e.message_id = s.message_id AND e.status = " + readStatus + ")) AS read_count" +
	" FROM whatsapp_form_sends s"

const (
	responseStatsSelectSQL = "SELECT r.origin, to_char(r.submitted_at AT TIME ZONE 'UTC', 'YYYY-MM-DD') AS day, COUNT(*) AS responses FROM whatsapp_form_responses r"

	medianSelectSQL = "SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (r.submitted_at - s.sent_at))) AS median" +
		" FROM whatsapp_form_responses r JOIN whatsapp_form_sends s ON s.id = r.send_id AND s.workspace_id = r.workspace_id"

	distributionSelectSQL = "SELECT o.option_id, COUNT(DISTINCT r.id) AS responses FROM whatsapp_form_responses r"

	distributionOptionsSQL = " CROSS JOIN LATERAL (SELECT jsonb_extract_path(r.answers, 'values', ?::text, 'optionIds') AS ids) a" +
		" CROSS JOIN LATERAL jsonb_array_elements_text(CASE WHEN jsonb_typeof(a.ids) = 'array' THEN a.ids ELSE '[]'::jsonb END) AS o(option_id)"
)

type sendStatRow struct {
	Origin    string
	Day       string
	Status    string
	Sends     int64
	Delivered int64
	ReadCount int64
}

type responseStatRow struct {
	Origin    string
	Day       string
	Responses int64
}

type optionCountRow struct {
	OptionID  string
	Responses int64
}

func checkStatsQuery(q whatsappform.StatsQuery) error {
	if !q.Scope.Resolved {
		return whatsappform.ErrForbidden
	}
	if q.From != nil && q.To != nil && q.From.After(*q.To) {
		return whatsappform.ErrListQueryInvalid
	}
	if !database.AllUUIDs(q.WorkspaceID, q.FormID) {
		return errMatchesNothing
	}
	return nil
}

func emptyStats() whatsappform.Stats {
	return whatsappform.Stats{Sends: map[whatsappform.SendStatus]int64{}, ByOrigin: []whatsappform.OriginStat{}, ByDay: []whatsappform.DayStat{}}
}

func scopedSends(q whatsappform.StatsQuery, head string) (*conditions, error) {
	scope, scopeArgs, err := scopeClause(q.Scope)
	if err != nil {
		return nil, err
	}
	return where(head+" WHERE s.workspace_id = ? AND s.form_id = ?", q.WorkspaceID, q.FormID).
		within("s.created_at", q.From, q.To).and(scope, scopeArgs...), nil
}

func scopedMatchedResponses(q whatsappform.StatsQuery, head string, headArgs ...interface{}) (*conditions, error) {
	scope, scopeArgs, err := scopeClause(q.Scope)
	if err != nil {
		return nil, err
	}
	return where(head, headArgs...).and(" WHERE r.workspace_id = ? AND r.form_id = ?"+matchClause(whatsappform.MatchFilterMatched), q.WorkspaceID, q.FormID).
		within("r.submitted_at", q.From, q.To).and(scope, scopeArgs...), nil
}

func sendStatsQuery(q whatsappform.StatsQuery) (string, []interface{}, error) {
	filter, err := scopedSends(q, sendStatsSelectSQL+joinsFor("s"))
	if err != nil {
		return "", nil, err
	}
	filter.and(" GROUP BY 1, 2, 3")
	return filter.String(), filter.args, nil
}

func responseStatsQuery(q whatsappform.StatsQuery) (string, []interface{}, error) {
	filter, err := scopedMatchedResponses(q, responseStatsSelectSQL+joinsFor("r"))
	if err != nil {
		return "", nil, err
	}
	filter.and(" GROUP BY 1, 2")
	return filter.String(), filter.args, nil
}

func medianQuery(q whatsappform.StatsQuery) (string, []interface{}, error) {
	filter, err := scopedMatchedResponses(q, medianSelectSQL+joinsFor("r"))
	if err != nil {
		return "", nil, err
	}
	filter.and(" AND s.sent_at IS NOT NULL")
	return filter.String(), filter.args, nil
}

func distributionQuery(q whatsappform.StatsQuery, fieldKey string) (string, []interface{}, error) {
	filter, err := scopedMatchedResponses(q, distributionSelectSQL+joinsFor("r")+distributionOptionsSQL, fieldKey)
	if err != nil {
		return "", nil, err
	}
	filter.and(" GROUP BY o.option_id")
	return filter.String(), filter.args, nil
}

func (s *ResponseStore) Stats(ctx context.Context, q whatsappform.StatsQuery) (whatsappform.Stats, error) {
	stats := emptyStats()
	if err := checkStatsQuery(q); err != nil {
		if errors.Is(err, errMatchesNothing) {
			return stats, nil
		}
		return stats, err
	}
	db := s.db.WithContext(ctx)
	sql, args, err := sendStatsQuery(q)
	if err != nil {
		return stats, err
	}
	var sends []sendStatRow
	if err := db.Raw(sql, args...).Scan(&sends).Error; err != nil {
		return stats, err
	}
	if sql, args, err = responseStatsQuery(q); err != nil {
		return stats, err
	}
	var responses []responseStatRow
	if err := db.Raw(sql, args...).Scan(&responses).Error; err != nil {
		return stats, err
	}
	if sql, args, err = medianQuery(q); err != nil {
		return stats, err
	}
	var median struct{ Median *float64 }
	if err := db.Raw(sql, args...).Scan(&median).Error; err != nil {
		return stats, err
	}
	stats.MedianSecondsToRespond = median.Median
	tally(&stats, sends, responses)
	return stats, nil
}

func tally(stats *whatsappform.Stats, sends []sendStatRow, responses []responseStatRow) {
	origins, days := map[whatsappform.SendOrigin]*whatsappform.OriginStat{}, map[string]*whatsappform.DayStat{}
	originOf := func(origin string) *whatsappform.OriginStat {
		key := whatsappform.SendOrigin(origin)
		if origins[key] == nil {
			origins[key] = &whatsappform.OriginStat{Origin: key}
		}
		return origins[key]
	}
	dayOf := func(day string) *whatsappform.DayStat {
		if days[day] == nil {
			days[day] = &whatsappform.DayStat{Day: day}
		}
		return days[day]
	}
	for _, row := range sends {
		status := whatsappform.SendStatus(row.Status)
		stats.Sends[status] += row.Sends
		stats.Delivered += row.Delivered
		stats.Read += row.ReadCount
		sent := int64(0)
		if status == whatsappform.SendSent || status == whatsappform.SendResponded {
			sent = row.Sends
		}
		originOf(row.Origin).Sent += sent
		dayOf(row.Day).Sent += sent
	}
	for _, row := range responses {
		stats.Responses += row.Responses
		if row.Origin != "" {
			originOf(row.Origin).Responses += row.Responses
		}
		dayOf(row.Day).Responses += row.Responses
	}
	for _, origin := range whatsappform.SendOrigins {
		if stat, ok := origins[origin]; ok {
			stats.ByOrigin = append(stats.ByOrigin, *stat)
			delete(origins, origin)
		}
	}
	unknown := make([]whatsappform.OriginStat, 0, len(origins))
	for _, stat := range origins {
		unknown = append(unknown, *stat)
	}
	sort.Slice(unknown, func(i, j int) bool { return unknown[i].Origin < unknown[j].Origin })
	stats.ByOrigin = append(stats.ByOrigin, unknown...)
	for _, stat := range days {
		stats.ByDay = append(stats.ByDay, *stat)
	}
	sort.Slice(stats.ByDay, func(i, j int) bool { return stats.ByDay[i].Day < stats.ByDay[j].Day })
}

func (s *ResponseStore) Distribution(ctx context.Context, q whatsappform.StatsQuery, fieldKey string) (map[string]int64, error) {
	counts := map[string]int64{}
	if err := checkStatsQuery(q); err != nil {
		if errors.Is(err, errMatchesNothing) {
			return counts, nil
		}
		return nil, err
	}
	fieldKey = strings.TrimSpace(fieldKey)
	if fieldKey == "" {
		return nil, whatsappform.ErrListQueryInvalid
	}
	sql, args, err := distributionQuery(q, fieldKey)
	if err != nil {
		return nil, err
	}
	var rows []optionCountRow
	if err := s.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		counts[row.OptionID] = row.Responses
	}
	return counts, nil
}
