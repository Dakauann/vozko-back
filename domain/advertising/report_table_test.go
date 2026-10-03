package advertising

import (
	"errors"
	"testing"
	"time"
)

func liveRow(objectID string, dims map[Breakdown]string, spend, impressions, results int64, action string, reach int64) LiveRow {
	return LiveRow{
		ObjectID:   objectID,
		Dimensions: dims,
		Values: LiveMetrics{
			Metrics: Metrics{Currency: "BRL", SpendMicros: spend, Impressions: impressions, LinkClicks: impressions / 10, Results: results, ResultAction: action},
			Reach:   reach, Frequency: 1.5,
		},
	}
}

func cell(t *testing.T, cells ReportCells, metric ReportMetric) float64 {
	t.Helper()
	v := cells[metric]
	if v == nil {
		t.Fatalf("%s is empty", metric)
	}
	return *v
}

func TestPivotGroupsByObjectAndBreakdownAndSkipsTotalRows(t *testing.T) {
	def := ReportDefinition{View: ViewPivot, Level: LevelCampaign, Breakdowns: []Breakdown{BreakdownAge}, Metrics: []ReportMetric{ReportSpend, ReportReach, ReportCostPerResult, ReportCPM}}
	rows := []LiveRow{
		liveRow("c-2", map[Breakdown]string{BreakdownAge: "18-24"}, 3_000_000, 1000, 3, "lead", 800),
		liveRow("c-1", map[Breakdown]string{BreakdownAge: "25-34"}, 1_000_000, 500, 1, "lead", 400),
		liveRow("c-1", nil, 9_000_000, 9000, 9, "lead", 9000),
	}
	table, err := BuildReportTable(def, rows, map[string]string{"c-1": "Aurora", "c-2": "Brisa"})
	if err != nil {
		t.Fatal(err)
	}
	if len(table.Rows) != 2 || table.Rows[0].Name != "Aurora" || table.Rows[0].Dimensions[0] != "25-34" || table.Rows[1].ObjectID != "c-2" {
		t.Fatalf("rows %+v", table.Rows)
	}
	if cell(t, table.Rows[1].Values, ReportCostPerResult) != 1_000_000 || cell(t, table.Rows[1].Values, ReportCPM) != 3_000_000 {
		t.Fatalf("values %+v", table.Rows[1].Values)
	}
	if table.Rows[1].Share != 0.75 || cell(t, table.Totals, ReportSpend) != 4_000_000 {
		t.Fatalf("share %v totals %+v", table.Rows[1].Share, table.Totals)
	}
	if table.Totals[ReportReach] != nil {
		t.Fatal("unique reach cannot be added across rows")
	}
	if cell(t, table.Rows[0].Values, ReportReach) != 400 {
		t.Fatal("a single row keeps its reach")
	}
}

func TestBarsGroupByBreakdownAcrossObjectsHighestFirst(t *testing.T) {
	def := ReportDefinition{View: ViewBars, Level: LevelAd, Breakdowns: []Breakdown{BreakdownGender}, Metrics: []ReportMetric{ReportSpend}}
	rows := []LiveRow{
		liveRow("a-1", map[Breakdown]string{BreakdownGender: "female"}, 1_000_000, 10, 0, "", 5),
		liveRow("a-2", map[Breakdown]string{BreakdownGender: "female"}, 2_000_000, 10, 0, "", 5),
		liveRow("a-1", map[Breakdown]string{BreakdownGender: "male"}, 5_000_000, 10, 0, "", 5),
	}
	table, err := BuildReportTable(def, rows, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(table.Rows) != 2 || table.Rows[0].Dimensions[0] != "male" || table.Rows[0].ObjectID != "" || cell(t, table.Rows[1].Values, ReportSpend) != 3_000_000 {
		t.Fatalf("rows %+v", table.Rows)
	}
}

func TestMixedResultKindsHaveNoResultsOrCost(t *testing.T) {
	def := ReportDefinition{View: ViewBars, Level: LevelAd, Metrics: []ReportMetric{ReportResults, ReportCostPerResult}}
	table, err := BuildReportTable(def, []LiveRow{
		liveRow("a-1", nil, 1_000_000, 10, 2, "lead", 5),
		liveRow("a-2", nil, 1_000_000, 10, 3, "link_click", 5),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if table.Totals[ReportResults] != nil || table.Totals[ReportCostPerResult] != nil {
		t.Fatalf("totals %+v", table.Totals)
	}
}

func TestMixedCurrenciesAreRefused(t *testing.T) {
	usd := liveRow("a-2", nil, 1, 1, 0, "", 1)
	usd.Values.Currency = "USD"
	_, err := BuildReportTable(ReportDefinition{View: ViewPivot, Level: LevelAd, Metrics: []ReportMetric{ReportSpend}}, []LiveRow{liveRow("a-1", nil, 1, 1, 0, "", 1), usd}, nil)
	if !errors.Is(err, ErrMixedCurrencies) {
		t.Fatalf("got %v", err)
	}
}

func TestSeriesCarriesTheDailyMetrics(t *testing.T) {
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	series := BuildReportSeries([]ReportMetric{ReportSpend, ReportResults}, []DayMetrics{{Day: day, Metrics: Metrics{SpendMicros: 5, Results: 2, ResultAction: "lead"}}})
	if len(series) != 1 || !series[0].Day.Equal(day) || *series[0].Values[ReportSpend] != 5 || *series[0].Values[ReportResults] != 2 {
		t.Fatalf("series %+v", series)
	}
}

func TestALiveQueryNamesAtMostAHundredObjects(t *testing.T) {
	r := DateRange{Since: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), Until: time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)}
	q := LiveQuery{Level: LevelAd, Range: r, ObjectIDs: make([]string, MaxLiveObjects)}
	if err := q.Validate(); err != nil {
		t.Fatal(err)
	}
	q.ObjectIDs = append(q.ObjectIDs, "one-more")
	if err := q.Validate(); err == nil {
		t.Fatal("more than a hundred objects must be refused")
	}
}
