package marketing

import (
	"context"
	"testing"
	"time"

	"vozko/domain/advertising"
)

func septemberRange(t *testing.T) advertising.DateRange {
	t.Helper()
	r, err := advertising.NewDateRange("2026-09-01", "2026-09-30")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestDailyInsightsRequestUsesDefaultAttribution(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{"data":[]}`))

	if _, err := g.DailyInsights(context.Background(), "tok", "9", septemberRange(t)); err != nil {
		t.Fatal(err)
	}
	q := (*calls)[0].query
	if (*calls)[0].path != "/v26.0/act_9/insights" {
		t.Fatalf("path = %s", (*calls)[0].path)
	}
	want := map[string]string{
		"level":             "ad",
		"time_increment":    "1",
		"time_range":        `{"since":"2026-09-01","until":"2026-09-30"}`,
		"action_breakdowns": `["action_type"]`,
		"limit":             "500",
		"fields":            insightFields,
	}
	for k, v := range want {
		if q.Get(k) != v {
			t.Fatalf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
	if q.Has("action_attribution_windows") {
		t.Fatalf("attribution windows must not be sent: %v", q)
	}
}

func TestDailyInsightsMapsSpendAndTrackedActions(t *testing.T) {
	g, _ := gatewayWith(t, ok(`{"data":[{"campaign_id":"C1","adset_id":"S1","ad_id":"A1","date_start":"2026-09-02","account_currency":"BRL",
		"spend":"12.34","impressions":"1000","clicks":"40","inline_link_clicks":"25",
		"actions":[{"action_type":"onsite_conversion.messaging_conversation_started_7d","value":"7"},
		           {"action_type":"lead","value":"2.0"},
		           {"action_type":"video_view","value":"300"}]}]}`))

	rows, err := g.DailyInsights(context.Background(), "tok", "9", septemberRange(t))
	if err != nil {
		t.Fatal(err)
	}
	row := rows[0]
	if row.CampaignMetaID != "C1" || row.AdSetMetaID != "S1" || row.AdMetaID != "A1" || row.Currency != "BRL" {
		t.Fatalf("row = %+v", row)
	}
	if !row.Day.Equal(time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)) || row.SpendMicros != 12_340_000 {
		t.Fatalf("day %v spend %d", row.Day, row.SpendMicros)
	}
	if row.Impressions != 1000 || row.Clicks != 40 || row.LinkClicks != 25 {
		t.Fatalf("counts = %+v", row)
	}
	if len(row.Actions) != 2 || row.Actions[advertising.ActionConversationStarted] != 7 || row.Actions[advertising.ActionLead] != 2 {
		t.Fatalf("actions = %v", row.Actions)
	}
}

func TestDailyInsightsRejectsUnreadableRows(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "missing spend", body: `{"data":[{"ad_id":"A1","date_start":"2026-09-02","account_currency":"BRL","impressions":"1"}]}`},
		{name: "bad spend", body: `{"data":[{"ad_id":"A1","date_start":"2026-09-02","account_currency":"BRL","spend":"1,5"}]}`},
		{name: "missing currency", body: `{"data":[{"ad_id":"A1","date_start":"2026-09-02","spend":"1.00"}]}`},
		{name: "bad day", body: `{"data":[{"ad_id":"A1","date_start":"02/09/2026","account_currency":"BRL","spend":"1.00"}]}`},
		{name: "bad impressions", body: `{"data":[{"ad_id":"A1","date_start":"2026-09-02","account_currency":"BRL","spend":"1.00","impressions":"many"}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, _ := gatewayWith(t, ok(tt.body))
			if _, err := g.DailyInsights(context.Background(), "tok", "9", septemberRange(t)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
