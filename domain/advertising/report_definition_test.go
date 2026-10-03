package advertising

import (
	"errors"
	"slices"
	"testing"
)

func pivotOf(metrics ...ReportMetric) ReportDefinition {
	return ReportDefinition{View: ViewPivot, Level: LevelCampaign, Metrics: metrics, DatePreset: "last30"}
}

func issueCodes(err error) []string {
	var invalid *ValidationError
	if !errors.As(err, &invalid) {
		return nil
	}
	out := make([]string, 0, len(invalid.Issues))
	for _, issue := range invalid.Issues {
		out = append(out, issue.Field+":"+issue.Code)
	}
	return out
}

func TestEveryTemplateIsAValidDefinition(t *testing.T) {
	keys := map[string]bool{}
	for _, template := range ReportTemplates() {
		if err := template.Definition.Validate(); err != nil {
			t.Errorf("%s: %v", template.Key, err)
		}
		keys[template.Key] = true
	}
	for _, key := range []string{"roi_snapshot", "reach_frequency", "overall_performance", "signups_summary", "age_gender", "engagement"} {
		if !keys[key] {
			t.Errorf("missing template %s", key)
		}
	}
}

func TestReportDefinitionRefusesUnknownValues(t *testing.T) {
	cases := map[string]ReportDefinition{
		"definition.view:invalid":                   {View: "table", Level: LevelAd, Metrics: []ReportMetric{ReportSpend}, DatePreset: "last7"},
		"definition.level:invalid":                  {View: ViewPivot, Level: "account", Metrics: []ReportMetric{ReportSpend}, DatePreset: "last7"},
		"definition.metrics:required":               pivotOf(),
		"definition.metrics:invalid":                pivotOf(ReportSpend, ReportSpend),
		"definition.datePreset:invalid":             {View: ViewPivot, Level: LevelAd, Metrics: []ReportMetric{ReportSpend}, DatePreset: "forever"},
		"definition.breakdowns:invalid_combination": {View: ViewPivot, Level: LevelAd, Breakdowns: []Breakdown{BreakdownAge, BreakdownCountry}, Metrics: []ReportMetric{ReportSpend}, DatePreset: "last7"},
	}
	for want, definition := range cases {
		if got := issueCodes(definition.Validate()); !slices.Contains(got, want) {
			t.Errorf("want %s got %v", want, got)
		}
	}
}

func TestTrendUsesDailyMetricsWithoutBreakdowns(t *testing.T) {
	trend := ReportDefinition{View: ViewTrend, Level: LevelCampaign, Metrics: []ReportMetric{ReportSpend, ReportResults}, DatePreset: "last7"}
	if err := trend.Validate(); err != nil {
		t.Fatal(err)
	}
	trend.Metrics = []ReportMetric{ReportReach}
	if got := issueCodes(trend.Validate()); !slices.Contains(got, "definition.metrics:invalid") {
		t.Fatalf("reach is not daily, got %v", got)
	}
	trend.Metrics = []ReportMetric{ReportSpend}
	trend.Breakdowns = []Breakdown{BreakdownAge}
	if got := issueCodes(trend.Validate()); !slices.Contains(got, "definition.breakdowns:not_for_trend") {
		t.Fatalf("got %v", got)
	}
}

func TestCustomDatesOnlyForTheCustomPreset(t *testing.T) {
	custom := pivotOf(ReportSpend)
	custom.DatePreset = PresetCustom
	if got := issueCodes(custom.Validate()); !slices.Contains(got, "definition.since:invalid_range") {
		t.Fatalf("got %v", got)
	}
	custom.Since, custom.Until = "2026-09-01", "2026-09-30"
	if err := custom.Validate(); err != nil {
		t.Fatal(err)
	}
	preset := pivotOf(ReportSpend)
	preset.Since = "2026-09-01"
	if got := issueCodes(preset.Validate()); !slices.Contains(got, "definition.since:only_for_custom") {
		t.Fatalf("got %v", got)
	}
}

func TestSavedReportNeedsANameAndAValidDefinition(t *testing.T) {
	r := &SavedReport{}
	if got := issueCodes(r.Set("acc-1", "  ", pivotOf(ReportSpend))); !slices.Contains(got, "name:required") {
		t.Fatalf("got %v", got)
	}
	if err := r.Set("acc-1", "Mensal", pivotOf()); err == nil {
		t.Fatal("an invalid definition must be refused")
	}
	if err := r.Set("acc-1", " Mensal ", pivotOf(ReportSpend)); err != nil || r.Name != "Mensal" || r.AdAccountID != "acc-1" {
		t.Fatalf("got %+v %v", r, err)
	}
}

func TestReportOptionsListEveryAllowedBreakdownOnce(t *testing.T) {
	got := ReportBreakdowns()
	if len(got) != 8 || got[0] != BreakdownAge || !slices.Contains(got, BreakdownHourOfDay) {
		t.Fatalf("got %v", got)
	}
	for _, metric := range TrendMetrics() {
		if !slices.Contains(ReportMetrics(), metric) {
			t.Fatalf("trend metric %s is not a report metric", metric)
		}
	}
}

func TestEveryReportMetricHasAKind(t *testing.T) {
	want := map[ReportMetric]MetricKind{ReportSpend: KindMoney, ReportCTR: KindPercent, ReportFrequency: KindDecimal, ReportReach: KindCount, ReportCostPerThruPlay: KindMoney}
	for metric, kind := range want {
		if metric.Kind() != kind {
			t.Errorf("%s: got %s", metric, metric.Kind())
		}
	}
}
