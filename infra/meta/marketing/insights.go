package marketing

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"vozko/domain/advertising"
	"vozko/infra/meta"
)

const (
	insightFields     = "campaign_id,adset_id,ad_id,date_start,account_currency,spend,impressions,clicks,inline_link_clicks,actions"
	liveFields        = "account_currency,spend,impressions,clicks,inline_link_clicks,actions"
	uniqueFields      = "reach,frequency,video_play_actions,video_p25_watched_actions,video_p50_watched_actions,video_p75_watched_actions,video_p95_watched_actions,video_p100_watched_actions,video_thruplay_watched_actions,video_avg_time_watched_actions"
	reportCompleted   = "Job Completed"
	reportPollBudget  = 60 * time.Second
	reportPollMaxWait = 10 * time.Second
)

var _ advertising.InsightsGateway = (*Gateway)(nil)

var reportPending = []string{"Job Not Started", "Job Started", "Job Running"}

type graphAction struct {
	ActionType string      `json:"action_type"`
	Value      graphNumber `json:"value"`
}

type graphMetrics struct {
	AccountCurrency  string        `json:"account_currency"`
	Spend            string        `json:"spend"`
	Impressions      graphNumber   `json:"impressions"`
	Clicks           graphNumber   `json:"clicks"`
	InlineLinkClicks graphNumber   `json:"inline_link_clicks"`
	Actions          []graphAction `json:"actions"`
}

func (m graphMetrics) daily(subject string) (advertising.DailyInsight, error) {
	if m.AccountCurrency == "" {
		return advertising.DailyInsight{}, fmt.Errorf("marketing: insight row for %s without currency", subject)
	}
	spend, err := advertising.DecimalToMicros(m.Spend)
	if err != nil {
		return advertising.DailyInsight{}, fmt.Errorf("marketing: insight row for %s: %w", subject, err)
	}
	out := advertising.DailyInsight{Currency: m.AccountCurrency, SpendMicros: spend, Actions: map[string]int64{}}
	if out.Impressions, err = m.Impressions.wholePart("impressions"); err != nil {
		return advertising.DailyInsight{}, err
	}
	if out.Clicks, err = m.Clicks.wholePart("clicks"); err != nil {
		return advertising.DailyInsight{}, err
	}
	if out.LinkClicks, err = m.InlineLinkClicks.wholePart("inline_link_clicks"); err != nil {
		return advertising.DailyInsight{}, err
	}
	tracked := advertising.InsightActionTypes()
	for _, action := range m.Actions {
		if !slices.Contains(tracked, action.ActionType) {
			continue
		}
		value, err := action.Value.wholePart(action.ActionType)
		if err != nil {
			return advertising.DailyInsight{}, err
		}
		out.Actions[action.ActionType] += value
	}
	return out, nil
}

type graphInsight struct {
	graphMetrics
	CampaignID meta.GraphID `json:"campaign_id"`
	AdSetID    meta.GraphID `json:"adset_id"`
	AdID       meta.GraphID `json:"ad_id"`
	DateStart  string       `json:"date_start"`
}

func (row graphInsight) toDomain() (advertising.DailyInsight, error) {
	if row.AdID == "" {
		return advertising.DailyInsight{}, fmt.Errorf("marketing: insight row without ad_id")
	}
	day, err := advertising.ParseDay(row.DateStart)
	if err != nil {
		return advertising.DailyInsight{}, err
	}
	out, err := row.daily("ad " + row.AdID.String())
	if err != nil {
		return advertising.DailyInsight{}, err
	}
	out.CampaignMetaID = row.CampaignID.String()
	out.AdSetMetaID = row.AdSetID.String()
	out.AdMetaID = row.AdID.String()
	out.Day = day
	return out, nil
}

func timeRangeParam(r advertising.DateRange) (string, error) {
	return jsonValue(map[string]string{
		"since": r.Since.Format(advertising.DayLayout),
		"until": r.Until.Format(advertising.DayLayout),
	})
}

func (g *Gateway) DailyInsights(ctx context.Context, token, metaAccountID string, r advertising.DateRange) ([]advertising.DailyInsight, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	path, err := accountPath(metaAccountID)
	if err != nil {
		return nil, err
	}
	timeRange, err := timeRangeParam(r)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("level", "ad")
	q.Set("time_increment", "1")
	q.Set("time_range", timeRange)
	q.Set("fields", insightFields)
	q.Set("action_breakdowns", `["action_type"]`)
	q.Set("limit", "500")
	rows, err := collect[graphInsight](ctx, g, path+"/insights", token, q)
	if err != nil {
		return nil, err
	}
	insights := make([]advertising.DailyInsight, 0, len(rows))
	for _, row := range rows {
		insight, err := row.toDomain()
		if err != nil {
			return nil, err
		}
		insights = append(insights, insight)
	}
	return insights, nil
}

type graphLiveRow struct {
	graphMetrics
	Reach        graphNumber   `json:"reach"`
	Frequency    graphNumber   `json:"frequency"`
	Plays        []graphAction `json:"video_play_actions"`
	P25          []graphAction `json:"video_p25_watched_actions"`
	P50          []graphAction `json:"video_p50_watched_actions"`
	P75          []graphAction `json:"video_p75_watched_actions"`
	P95          []graphAction `json:"video_p95_watched_actions"`
	P100         []graphAction `json:"video_p100_watched_actions"`
	ThruPlays    []graphAction `json:"video_thruplay_watched_actions"`
	AvgWatchTime []graphAction `json:"video_avg_time_watched_actions"`
}

func actionTotal(field string, actions []graphAction) (int64, error) {
	var total int64
	for _, a := range actions {
		v, err := a.Value.wholePart(field)
		if err != nil {
			return 0, err
		}
		total += v
	}
	return total, nil
}

func decimal(field string, n graphNumber) (float64, error) {
	if n == "" {
		return 0, nil
	}
	v, err := strconv.ParseFloat(string(n), 64)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("marketing: %s %q is not a number", field, string(n))
	}
	return v, nil
}

func (row graphLiveRow) values(subject string) (advertising.LiveMetrics, error) {
	daily, err := row.daily(subject)
	if err != nil {
		return advertising.LiveMetrics{}, err
	}
	out := advertising.LiveMetrics{Metrics: advertising.MetricsOf(daily, "")}
	if out.Reach, err = row.Reach.wholePart("reach"); err != nil {
		return advertising.LiveMetrics{}, err
	}
	if out.Frequency, err = decimal("frequency", row.Frequency); err != nil {
		return advertising.LiveMetrics{}, err
	}
	counts := []struct {
		field   string
		actions []graphAction
		target  *int64
	}{
		{"video_play_actions", row.Plays, &out.Video.Plays},
		{"video_p25_watched_actions", row.P25, &out.Video.P25},
		{"video_p50_watched_actions", row.P50, &out.Video.P50},
		{"video_p75_watched_actions", row.P75, &out.Video.P75},
		{"video_p95_watched_actions", row.P95, &out.Video.P95},
		{"video_p100_watched_actions", row.P100, &out.Video.P100},
		{"video_thruplay_watched_actions", row.ThruPlays, &out.Video.ThruPlays},
	}
	for _, c := range counts {
		if *c.target, err = actionTotal(c.field, c.actions); err != nil {
			return advertising.LiveMetrics{}, err
		}
	}
	if len(row.AvgWatchTime) > 0 {
		if out.Video.AvgWatchSeconds, err = decimal("video_avg_time_watched_actions", row.AvgWatchTime[0].Value); err != nil {
			return advertising.LiveMetrics{}, err
		}
	}
	return out, nil
}

func liveRowOf(raw json.RawMessage, q advertising.LiveQuery) (advertising.LiveRow, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return advertising.LiveRow{}, fmt.Errorf("marketing: unreadable insight row: %w", err)
	}
	idField := string(q.Level) + "_id"
	var id meta.GraphID
	if err := json.Unmarshal(fields[idField], &id); err != nil || id == "" {
		return advertising.LiveRow{}, fmt.Errorf("marketing: insight row without %s", idField)
	}
	out := advertising.LiveRow{ObjectID: id.String()}
	for _, b := range q.Breakdowns {
		var value string
		if err := json.Unmarshal(fields[string(b)], &value); err != nil || value == "" {
			return advertising.LiveRow{}, fmt.Errorf("marketing: insight row for %s without %s", id, b)
		}
		if out.Dimensions == nil {
			out.Dimensions = map[advertising.Breakdown]string{}
		}
		out.Dimensions[b] = value
	}
	var row graphLiveRow
	if err := json.Unmarshal(raw, &row); err != nil {
		return advertising.LiveRow{}, fmt.Errorf("marketing: unreadable insight row for %s: %w", id, err)
	}
	values, err := row.values(string(q.Level) + " " + id.String())
	if err != nil {
		return advertising.LiveRow{}, err
	}
	out.Values = values
	return out, nil
}

func liveParams(q advertising.LiveQuery) (url.Values, error) {
	if err := q.Validate(); err != nil {
		return nil, err
	}
	timeRange, err := timeRangeParam(q.Range)
	if err != nil {
		return nil, err
	}
	fields := liveFields + "," + string(q.Level) + "_id"
	if q.UniqueFieldsAllowed() {
		fields += "," + uniqueFields
	}
	params := url.Values{}
	params.Set("level", string(q.Level))
	params.Set("time_range", timeRange)
	params.Set("fields", fields)
	params.Set("action_breakdowns", `["action_type"]`)
	params.Set("limit", "500")
	if len(q.ObjectIDs) > 0 {
		filtering, err := jsonValue([]map[string]any{{"field": string(q.Level) + ".id", "operator": "IN", "value": q.ObjectIDs}})
		if err != nil {
			return nil, err
		}
		params.Set("filtering", filtering)
	}
	if len(q.Breakdowns) > 0 {
		breakdowns, err := jsonValue(q.Breakdowns)
		if err != nil {
			return nil, err
		}
		params.Set("breakdowns", breakdowns)
	}
	if len(q.Windows) > 0 {
		windows, err := jsonValue(q.Windows)
		if err != nil {
			return nil, err
		}
		params.Set("action_attribution_windows", windows)
	}
	return params, nil
}

func (g *Gateway) LiveInsights(ctx context.Context, token, metaAccountID string, q advertising.LiveQuery) ([]advertising.LiveRow, error) {
	path, err := accountPath(metaAccountID)
	if err != nil {
		return nil, err
	}
	params, err := liveParams(q)
	if err != nil {
		return nil, err
	}
	var raws []json.RawMessage
	if len(q.Breakdowns) > 0 {
		raws, err = g.asyncInsights(ctx, token, path, params)
	} else {
		raws, err = collect[json.RawMessage](ctx, g, path+"/insights", token, params)
	}
	if err != nil {
		return nil, err
	}
	rows := make([]advertising.LiveRow, 0, len(raws))
	for _, raw := range raws {
		row, err := liveRowOf(raw, q)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

type graphReportRun struct {
	Status       string `json:"async_status"`
	ErrorCode    int    `json:"error_code"`
	ErrorSubcode int    `json:"error_subcode"`
	ErrorMessage string `json:"error_message"`
	UserTitle    string `json:"error_user_title"`
	UserMessage  string `json:"error_user_msg"`
}

func (run graphReportRun) failure(reportID meta.GraphID) error {
	return remoteError(&meta.Error{
		Code:      run.ErrorCode,
		Subcode:   run.ErrorSubcode,
		Message:   fmt.Sprintf("insights report %s: %s %s", reportID, run.Status, run.ErrorMessage),
		UserTitle: run.UserTitle,
		UserMsg:   run.UserMessage,
	})
}

func (g *Gateway) asyncInsights(ctx context.Context, token, path string, params url.Values) ([]json.RawMessage, error) {
	var started struct {
		ReportRunID meta.GraphID `json:"report_run_id"`
	}
	if err := g.do(ctx, meta.Request{Method: http.MethodPost, Path: path + "/insights", Token: token, Form: params}, &started); err != nil {
		return nil, err
	}
	runPath, err := objectPath(started.ReportRunID.String())
	if err != nil {
		return nil, fmt.Errorf("marketing: insights report started without an id")
	}
	delay, waited := time.Second, time.Duration(0)
	for {
		var run graphReportRun
		if err := g.do(ctx, meta.Request{Method: http.MethodGet, Path: runPath, Token: token}, &run); err != nil {
			return nil, err
		}
		switch {
		case run.Status == reportCompleted:
			q := url.Values{}
			q.Set("limit", "500")
			return collect[json.RawMessage](ctx, g, runPath+"/insights", token, q)
		case slices.Contains(reportPending, run.Status):
		case strings.HasPrefix(run.Status, "Job "):
			return nil, run.failure(started.ReportRunID)
		default:
			return nil, fmt.Errorf("marketing: insights report %s has unknown status %q", started.ReportRunID, run.Status)
		}
		if waited >= reportPollBudget {
			return nil, &advertising.RemoteError{Kind: advertising.FailureRetryable, Message: fmt.Sprintf("insights report %s still running", started.ReportRunID)}
		}
		if err := g.wait(ctx, delay); err != nil {
			return nil, err
		}
		waited += delay
		delay = min(delay*2, reportPollMaxWait)
	}
}
