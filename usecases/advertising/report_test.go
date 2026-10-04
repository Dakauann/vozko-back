package advertising

import (
	"context"
	"testing"

	ads "vozko/domain/advertising"
)

func seedStructure(w *world) {
	for _, o := range []*ads.Object{
		{MetaID: "c-1", Level: ads.LevelCampaign, Name: "Leads"},
		{MetaID: "c-2", Level: ads.LevelCampaign, Name: "Tráfego"},
		{MetaID: "s-1", Level: ads.LevelAdSet, CampaignMetaID: "c-1", OptimizationGoal: "CONVERSATIONS"},
		{MetaID: "s-2", Level: ads.LevelAdSet, CampaignMetaID: "c-2", OptimizationGoal: "LINK_CLICKS"},
		{MetaID: "a-1", Level: ads.LevelAd, CampaignMetaID: "c-1", AdSetMetaID: "s-1"},
		{MetaID: "a-2", Level: ads.LevelAd, CampaignMetaID: "c-1", AdSetMetaID: "s-1"},
		{MetaID: "a-3", Level: ads.LevelAd, CampaignMetaID: "c-2", AdSetMetaID: "s-2"},
	} {
		o.WorkspaceID, o.AdAccountID = "ws-1", "acc-1"
		w.objects.byID[o.MetaID] = o
	}
	day, _ := ads.ParseDay("2026-09-30")
	w.insights.rows = []ads.DailyInsight{
		{AdMetaID: "a-1", Day: day, Currency: "BRL", SpendMicros: 30_000_000, Impressions: 1000, LinkClicks: 10, Actions: map[string]int64{ads.ActionConversationStarted: 3}},
		{AdMetaID: "a-2", Day: day, Currency: "BRL", SpendMicros: 10_000_000, Impressions: 500, LinkClicks: 5, Actions: map[string]int64{ads.ActionConversationStarted: 1}},
		{AdMetaID: "a-3", Day: day, Currency: "BRL", SpendMicros: 20_000_000, Impressions: 4000, LinkClicks: 40, Actions: map[string]int64{ads.ActionLinkClick: 40}},
	}
	w.attrib.rows = []ads.Attribution{
		{Key: "a-1", Conversations: 4, Leads: 2, WonDeals: 1, Revenue: 20_000, RevenueCurrency: "BRL"},
		{Key: "a-3", Conversations: 1},
	}
}

func reporter(w *world) *ReportUseCase {
	return NewReportUseCase(w.accounts, w.objects, w.insights, w.attrib)
}

func septemberLast() ads.DateRange {
	r, _ := ads.NewDateRange("2026-09-24", "2026-09-30")
	return r
}

func TestCampaignRowsAddTheirAdsResultsByTheirAdSetGoal(t *testing.T) {
	w := newWorld()
	seedStructure(w)
	report, err := reporter(w).Report(context.Background(), ReportQuery{WorkspaceID: "ws-1", AccountID: "acc-1", Level: ads.LevelCampaign, Range: septemberLast()})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Rows) != 2 {
		t.Fatalf("rows %d", len(report.Rows))
	}
	leads := report.Rows[0]
	if leads.Object.MetaID != "c-1" || leads.Metrics.SpendMicros != 40_000_000 || leads.Metrics.Results != 4 || *leads.Metrics.CostPerResult() != 10_000_000 {
		t.Fatalf("leads row %+v", leads.Metrics)
	}
	if leads.Outcome.Conversations != 4 || *leads.Outcome.CostPerLead != 20_000_000 || *leads.Outcome.ROAS != 5 {
		t.Fatalf("leads outcome %+v", leads.Outcome)
	}
	if report.Totals.SpendMicros != 60_000_000 || !report.Totals.MixedResults || report.Outcome.Conversations != 5 {
		t.Fatalf("totals %+v outcome %+v", report.Totals, report.Outcome)
	}
}

func TestSelectingACampaignNarrowsTheAdsTabLikeMeta(t *testing.T) {
	w := newWorld()
	seedStructure(w)
	report, err := reporter(w).Report(context.Background(), ReportQuery{WorkspaceID: "ws-1", AccountID: "acc-1", Level: ads.LevelAd, Range: septemberLast(), CampaignIDs: []string{"c-2"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Rows) != 1 || report.Rows[0].Object.MetaID != "a-3" || report.Totals.SpendMicros != 20_000_000 {
		t.Fatalf("rows %+v totals %+v", report.Rows, report.Totals)
	}
}

func TestRowsWithoutDeliveryShowNoCostsInTheAccountCurrency(t *testing.T) {
	w := newWorld()
	seedStructure(w)
	w.insights.rows = nil
	report, err := reporter(w).Report(context.Background(), ReportQuery{WorkspaceID: "ws-1", AccountID: "acc-1", Level: ads.LevelAd, Range: septemberLast()})
	if err != nil {
		t.Fatal(err)
	}
	row := report.Rows[0]
	if !row.Metrics.Empty() || row.Metrics.CostPerResult() != nil || row.Outcome.CostPerConversation != nil {
		t.Fatalf("row %+v %+v", row.Metrics, row.Outcome)
	}
}

func TestReportOfAnotherWorkspacesAccountIsRefused(t *testing.T) {
	w := newWorld()
	if _, err := reporter(w).Report(context.Background(), ReportQuery{WorkspaceID: "ws-2", AccountID: "acc-1", Level: ads.LevelAd, Range: septemberLast()}); err == nil {
		t.Fatal("foreign account reported")
	}
}

func TestTrendHasOnePointPerDayIncludingEmptyDays(t *testing.T) {
	w := newWorld()
	seedStructure(w)
	trend, err := reporter(w).Trend(context.Background(), ReportQuery{WorkspaceID: "ws-1", AccountID: "acc-1", Range: septemberLast()})
	if err != nil {
		t.Fatal(err)
	}
	if len(trend.Points) != 7 || trend.Points[6].Metrics.SpendMicros != 60_000_000 || trend.Points[0].Metrics.SpendMicros != 0 {
		t.Fatalf("points %+v", trend.Points)
	}
}

func TestTrendOfOneAdCountsOnlyThatAd(t *testing.T) {
	w := newWorld()
	seedStructure(w)
	trend, err := reporter(w).Trend(context.Background(), ReportQuery{WorkspaceID: "ws-1", AccountID: "acc-1", Range: septemberLast(), AdIDs: []string{"a-2"}})
	if err != nil {
		t.Fatal(err)
	}
	if trend.Points[6].Metrics.SpendMicros != 10_000_000 {
		t.Fatalf("points %+v", trend.Points[6])
	}
}

func TestComparingReadsTheSameLengthRightBefore(t *testing.T) {
	w := newWorld()
	seedStructure(w)
	report, err := reporter(w).Report(context.Background(), ReportQuery{WorkspaceID: "ws-1", AccountID: "acc-1", Level: ads.LevelCampaign, Range: septemberLast(), Compare: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.Previous == nil || report.Previous.Range.Until.Format(ads.DayLayout) != "2026-09-23" || report.Previous.Totals.SpendMicros != 0 {
		t.Fatalf("previous %+v", report.Previous)
	}
}

func TestObjectRowMatchesItsRowInTheFullReport(t *testing.T) {
	w := newWorld()
	seedStructure(w)
	uc := reporter(w)
	full, err := uc.Report(context.Background(), ReportQuery{WorkspaceID: "ws-1", AccountID: "acc-1", Level: ads.LevelAdSet, Range: septemberLast()})
	if err != nil {
		t.Fatal(err)
	}
	got, err := uc.ObjectRow(context.Background(), "ws-1", "s-1", septemberLast())
	if err != nil {
		t.Fatal(err)
	}
	row, want := got.Row, full.Rows[0]
	if row.Object.MetaID != "s-1" || row.Metrics != want.Metrics || row.Outcome.Conversations != want.Outcome.Conversations || got.Range != septemberLast() || got.Account.ID != "acc-1" {
		t.Fatalf("row %+v want %+v", row, want)
	}
	if row.Metrics.SpendMicros != 40_000_000 {
		t.Fatalf("spend %d", row.Metrics.SpendMicros)
	}
}

func TestObjectRowOfAnotherWorkspaceIsNotFound(t *testing.T) {
	w := newWorld()
	seedStructure(w)
	if _, err := reporter(w).ObjectRow(context.Background(), "ws-2", "s-1", septemberLast()); err == nil {
		t.Fatal("foreign object reported")
	}
}

func TestADealReachedThroughTwoCampaignsCountsOnceInTheTotal(t *testing.T) {
	w := newWorld()
	seedStructure(w)
	w.attrib.byGroup = func(groups []ads.AdGroup) []ads.Attribution {
		keys := map[string]bool{}
		for _, g := range groups {
			keys[g.Key] = true
		}
		var out []ads.Attribution
		for key := range keys {
			out = append(out, ads.Attribution{Key: key, Conversations: 1, Leads: 1, WonDeals: 1, Revenue: 20_000, RevenueCurrency: "BRL"})
		}
		return out
	}
	report, err := reporter(w).Report(context.Background(), ReportQuery{WorkspaceID: "ws-1", AccountID: "acc-1", Level: ads.LevelCampaign, Range: septemberLast()})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Rows) != 2 || report.Rows[0].Outcome.WonDeals != 1 || report.Rows[1].Outcome.WonDeals != 1 {
		t.Fatalf("rows %+v", report.Rows)
	}
	if report.Outcome.WonDeals != 1 || report.Outcome.RevenueMicros != 20_000*10_000 {
		t.Fatalf("the shared deal was counted %d times in the total", report.Outcome.WonDeals)
	}
	last := w.attrib.calls[len(w.attrib.calls)-1]
	for _, g := range last {
		if g.Key != last[0].Key {
			t.Fatalf("the total grouped ads apart: %+v", last)
		}
	}
}

func TestTrendGroupsDaysIntoWeeksLabelledInsideTheRange(t *testing.T) {
	w := newWorld()
	seedStructure(w)
	trend, err := reporter(w).Trend(context.Background(), ReportQuery{WorkspaceID: "ws-1", AccountID: "acc-1", Range: septemberLast(), Granularity: ads.GranularityWeek})
	if err != nil {
		t.Fatal(err)
	}
	if len(trend.Points) != 2 || trend.Points[0].Day.Format(ads.DayLayout) != "2026-09-24" || trend.Points[1].Day.Format(ads.DayLayout) != "2026-09-28" {
		t.Fatalf("points %+v", trend.Points)
	}
	if trend.Points[1].Metrics.SpendMicros != 60_000_000 || trend.Points[0].Metrics.SpendMicros != 0 {
		t.Fatalf("spend %+v", trend.Points)
	}
}
