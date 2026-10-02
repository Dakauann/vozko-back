package marketing

import (
	"context"
	"net/http"
	"reflect"
	"testing"
	"time"

	"vozko/domain/advertising"
)

func liveQuery(t *testing.T, level advertising.Level, breakdowns ...advertising.Breakdown) advertising.LiveQuery {
	t.Helper()
	return advertising.LiveQuery{Level: level, Range: septemberRange(t), Breakdowns: breakdowns}
}

func TestLiveInsightsSynchronousRequest(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{"data":[{"adset_id":"S1","account_currency":"BRL","spend":"10.50","impressions":"2000","clicks":"80",
		"inline_link_clicks":"50","reach":"1500","frequency":"1.3333",
		"actions":[{"action_type":"onsite_conversion.messaging_conversation_started_7d","value":"6"},{"action_type":"lead","value":"2"}],
		"video_play_actions":[{"action_type":"video_view","value":"900"}],"video_p25_watched_actions":[{"action_type":"video_view","value":"400"}],
		"video_p50_watched_actions":[{"action_type":"video_view","value":"300"}],"video_p75_watched_actions":[{"action_type":"video_view","value":"200"}],
		"video_p95_watched_actions":[{"action_type":"video_view","value":"120"}],"video_p100_watched_actions":[{"action_type":"video_view","value":"100"}],
		"video_thruplay_watched_actions":[{"action_type":"video_view","value":"150"}],"video_avg_time_watched_actions":[{"action_type":"video_view","value":"7.5"}]}]}`))
	q := liveQuery(t, advertising.LevelAdSet)
	q.ObjectIDs = []string{"S1", "S2"}
	q.Windows = []advertising.AttributionWindow{advertising.Window7DayClick, advertising.Window1DayView}

	rows, err := g.LiveInsights(context.Background(), "tok", "9", q)
	if err != nil {
		t.Fatal(err)
	}
	call := (*calls)[0]
	if len(*calls) != 1 || call.method != http.MethodGet || call.path != "/v26.0/act_9/insights" {
		t.Fatalf("calls = %+v", *calls)
	}
	want := map[string]string{
		"level":                      "adset",
		"time_range":                 `{"since":"2026-09-01","until":"2026-09-30"}`,
		"fields":                     liveFields + ",adset_id," + uniqueFields,
		"filtering":                  `[{"field":"adset.id","operator":"IN","value":["S1","S2"]}]`,
		"action_attribution_windows": `["7d_click","1d_view"]`,
	}
	for k, v := range want {
		if call.query.Get(k) != v {
			t.Fatalf("%s = %q, want %q", k, call.query.Get(k), v)
		}
	}
	if call.query.Has("time_increment") || call.query.Has("breakdowns") {
		t.Fatalf("query = %v", call.query)
	}
	row := rows[0]
	if row.ObjectID != "S1" || row.Dimensions != nil || row.Values.SpendMicros != 10_500_000 || row.Values.Currency != "BRL" || row.Values.Impressions != 2000 ||
		row.Values.LinkClicks != 50 || row.Values.Conversations != 6 || row.Values.Leads != 2 || row.Values.Reach != 1500 || row.Values.Frequency != 1.3333 {
		t.Fatalf("row = %+v", row)
	}
	wantVideo := advertising.VideoMetrics{Plays: 900, P25: 400, P50: 300, P75: 200, P95: 120, P100: 100, ThruPlays: 150, AvgWatchSeconds: 7.5}
	if row.Values.Video != wantVideo {
		t.Fatalf("video = %+v", row.Values.Video)
	}
}

func TestLiveInsightsAsyncWithBreakdowns(t *testing.T) {
	polls := 0
	g, calls := gatewayWith(t, func(call recordedCall) (int, string) {
		switch call.method + " " + call.path {
		case "POST /v26.0/act_9/insights":
			return http.StatusOK, `{"report_run_id":"R1"}`
		case "GET /v26.0/R1":
			polls++
			if polls < 3 {
				return http.StatusOK, `{"id":"R1","async_status":"Job Running","async_percent_completion":50}`
			}
			return http.StatusOK, `{"id":"R1","async_status":"Job Completed","async_percent_completion":100}`
		case "GET /v26.0/R1/insights":
			return http.StatusOK, `{"data":[{"ad_id":"A1","age":"18-24","gender":"female","account_currency":"BRL","spend":"1.00","impressions":"10"}]}`
		}
		t.Errorf("unexpected %s %s", call.method, call.path)
		return http.StatusNotFound, `{}`
	})
	var waits []time.Duration
	g.wait = func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}

	rows, err := g.LiveInsights(context.Background(), "tok", "9", liveQuery(t, advertising.LevelAd, advertising.BreakdownAge, advertising.BreakdownGender))
	if err != nil {
		t.Fatal(err)
	}
	start := (*calls)[0]
	if start.form.Get("breakdowns") != `["age","gender"]` || start.form.Get("level") != "ad" || start.form.Get("fields") != liveFields+",ad_id,"+uniqueFields {
		t.Fatalf("start = %+v", start)
	}
	if (*calls)[1].query.Get("fields") != "async_status,async_percent_completion,error_code,error_message" || (*calls)[len(*calls)-1].query.Get("limit") != "500" {
		t.Fatalf("calls = %+v", *calls)
	}
	if len(waits) != 2 || waits[0] != time.Second || waits[1] != 2*time.Second {
		t.Fatalf("waits = %v", waits)
	}
	want := map[advertising.Breakdown]string{advertising.BreakdownAge: "18-24", advertising.BreakdownGender: "female"}
	if len(rows) != 1 || rows[0].ObjectID != "A1" || !reflect.DeepEqual(rows[0].Dimensions, want) || rows[0].Values.Impressions != 10 {
		t.Fatalf("rows = %+v", rows)
	}
}

func completedReport(rows string) func(recordedCall) (int, string) {
	return func(call recordedCall) (int, string) {
		switch call.path {
		case "/v26.0/act_9/insights":
			return http.StatusOK, `{"report_run_id":"R1"}`
		case "/v26.0/R1":
			return http.StatusOK, `{"async_status":"Job Completed"}`
		}
		return http.StatusOK, rows
	}
}

func TestLiveInsightsHourlyDropsUniqueFields(t *testing.T) {
	g, calls := gatewayWith(t, completedReport(`{"data":[]}`))
	if _, err := g.LiveInsights(context.Background(), "tok", "9", liveQuery(t, advertising.LevelCampaign, advertising.BreakdownHourOfDay)); err != nil {
		t.Fatal(err)
	}
	if got := (*calls)[0].form.Get("fields"); got != liveFields+",campaign_id" {
		t.Fatalf("fields = %s", got)
	}
}

func reportStatus(status string) func(recordedCall) (int, string) {
	return func(call recordedCall) (int, string) {
		if call.method == http.MethodPost {
			return http.StatusOK, `{"report_run_id":"R1"}`
		}
		return http.StatusOK, status
	}
}

func TestLiveInsightsFailures(t *testing.T) {
	byAge := func(t *testing.T) advertising.LiveQuery {
		return liveQuery(t, advertising.LevelAd, advertising.BreakdownAge)
	}
	plain := func(t *testing.T) advertising.LiveQuery { return liveQuery(t, advertising.LevelAd) }
	tests := []struct {
		name    string
		query   func(t *testing.T) advertising.LiveQuery
		respond func(recordedCall) (int, string)
		calls   int
	}{
		{
			name: "invalid breakdowns never call meta",
			query: func(t *testing.T) advertising.LiveQuery {
				return liveQuery(t, advertising.LevelAd, advertising.BreakdownAge, advertising.BreakdownCountry)
			},
			respond: ok(`{}`),
		},
		{name: "failed report", query: byAge, respond: reportStatus(`{"async_status":"Job Failed","error_code":2601,"error_message":"too much data"}`), calls: 2},
		{name: "unknown report status", query: byAge, respond: reportStatus(`{"async_status":"Queued"}`), calls: 2},
		{name: "row without breakdown value", query: byAge, respond: completedReport(`{"data":[{"ad_id":"A1","account_currency":"BRL","spend":"1.00"}]}`), calls: 3},
		{name: "row without object id", query: plain, respond: ok(`{"data":[{"adset_id":"S1","account_currency":"BRL","spend":"1.00"}]}`), calls: 1},
		{name: "bad frequency", query: plain, respond: ok(`{"data":[{"ad_id":"A1","account_currency":"BRL","spend":"1.00","frequency":"often"}]}`), calls: 1},
		{name: "missing spend", query: plain, respond: ok(`{"data":[{"ad_id":"A1","account_currency":"BRL"}]}`), calls: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, calls := gatewayWith(t, tt.respond)
			if _, err := g.LiveInsights(context.Background(), "tok", "9", tt.query(t)); err == nil || len(*calls) != tt.calls {
				t.Fatalf("err %v calls %d", err, len(*calls))
			}
		})
	}
}

func TestLiveInsightsReportStillRunningIsRetryable(t *testing.T) {
	g, _ := gatewayWith(t, reportStatus(`{"async_status":"Job Started"}`))
	_, err := g.LiveInsights(context.Background(), "tok", "9", liveQuery(t, advertising.LevelAd, advertising.BreakdownAge))
	if advertising.Classify(err) != advertising.FailureRetryable {
		t.Fatalf("err = %v", err)
	}
}
