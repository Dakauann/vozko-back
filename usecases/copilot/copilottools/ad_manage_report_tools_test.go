package copilottools

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"vozko/domain/advertising"
	"vozko/domain/copilot"
	"vozko/domain/workspace"
	adsuc "vozko/usecases/advertising"
)

const savedReportUUID = "9c8b7a6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d"

func cell(v float64) *float64 { return &v }

func genderBarsRun(rows int) *adsuc.ReportRun {
	table := advertising.ReportTable{
		View: advertising.ViewBars, Breakdowns: []advertising.Breakdown{advertising.BreakdownGender},
		Metrics: []advertising.ReportMetric{advertising.ReportSpend, advertising.ReportCTR},
		Totals:  advertising.ReportCells{advertising.ReportSpend: cell(500_000_000), advertising.ReportCTR: cell(1.5)},
	}
	for i := range rows {
		table.Rows = append(table.Rows, advertising.ReportTableRow{
			Key: strconv.Itoa(i), Dimensions: []string{"male"},
			Values: advertising.ReportCells{advertising.ReportSpend: cell(12_500_000), advertising.ReportCTR: cell(1.23456)},
		})
	}
	return &adsuc.ReportRun{Currency: "BRL", Table: table}
}

func reportContext() copilot.Context {
	cc := adContext
	cc.Datasets = copilot.NewDatasetStore()
	return cc
}

func TestRunAdReportSendsTheDefinitionAndCapsTheRows(t *testing.T) {
	f := newManageFixture()
	f.runs.run = genderBarsRun(40)
	args := map[string]interface{}{
		"ad_account_id": adAccountUUID, "view": "bars", "level": "ad",
		"breakdowns": []interface{}{"gender"}, "metrics": []interface{}{"spend", "ctr"},
	}
	cc := reportContext()
	result := f.tool(t, "run_ad_report").Execute(context.Background(), cc, args)
	if result.Status != copilot.StatusOK || len(f.runs.ran) != 1 {
		t.Fatalf("result %+v", result)
	}
	in := f.runs.ran[0]
	def := in.Definition
	if def.View != advertising.ViewBars || def.Level != advertising.LevelAd || len(def.Breakdowns) != 1 || def.Metrics[1] != advertising.ReportCTR {
		t.Fatalf("definition %+v", def)
	}
	if in.Range.Since.Format(advertising.DayLayout) != "2026-09-02" || in.Range.Until.Format(advertising.DayLayout) != "2026-10-01" || in.AccountID != adAccountUUID {
		t.Fatalf("without a period the report covers the last 30 days of the account: %+v", in.Range)
	}
	data := result.Data.(map[string]interface{})
	rows := data["rows"].([]map[string]interface{})
	if len(rows) != maxAdRowsShown || data["rows_total"] != 40 || rows[0]["spend"] != 12.5 || rows[0]["ctr"] != 1.23 || rows[0]["segment"] != "Masculino" {
		t.Fatalf("rows %+v", rows[0])
	}
	if totals := data["totals"].(map[string]interface{}); totals["spend"] != 500.0 {
		t.Fatalf("totals %+v", totals)
	}
	dataset := data["dataset"].(copilot.DatasetPreview)
	if dataset.RowCount != 40 {
		t.Fatalf("the whole table goes to the dataset: %+v", dataset)
	}
	if _, err := cc.Datasets.Get(dataset.DatasetID); err != nil {
		t.Fatalf("render_chart must find the dataset: %v", err)
	}
}

func TestRunAdReportRefusesAnInvalidDefinitionBeforeCallingMeta(t *testing.T) {
	cases := map[string]map[string]interface{}{
		"unknown metric":        {"ad_account_id": adAccountUUID, "metrics": []interface{}{"roas"}},
		"breakdowns in a trend": {"ad_account_id": adAccountUUID, "view": "trend", "breakdowns": []interface{}{"age"}},
		"reversed period":       {"ad_account_id": adAccountUUID, "since": "2026-09-30", "until": "2026-09-01"},
		"invented account":      {"ad_account_id": "act_123"},
		"invented object":       {"ad_account_id": adAccountUUID, "object_ids": []interface{}{"campanha"}},
	}
	for name, args := range cases {
		f := newManageFixture()
		if result := f.tool(t, "run_ad_report").Execute(context.Background(), reportContext(), args); result.Status != copilot.StatusError || len(f.runs.ran) != 0 {
			t.Errorf("%s: result %+v", name, result)
		}
	}
}

func TestTrendReportReturnsOneRowPerDay(t *testing.T) {
	f := newManageFixture()
	day := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	f.runs.run = &adsuc.ReportRun{
		Currency: "BRL",
		Table:    advertising.ReportTable{View: advertising.ViewTrend, Metrics: advertising.TrendMetrics()},
		Series:   []advertising.ReportDay{{Day: day, Values: advertising.ReportCells{advertising.ReportSpend: cell(3_000_000)}}},
	}
	result := f.tool(t, "run_ad_report").Execute(context.Background(), reportContext(), map[string]interface{}{"ad_account_id": adAccountUUID, "view": "trend"})
	data := result.Data.(map[string]interface{})
	rows := data["rows"].([]map[string]interface{})
	if result.Status != copilot.StatusOK || len(rows) != 1 || rows[0]["day"] != "2026-09-30" || rows[0]["spend"] != 3.0 {
		t.Fatalf("result %+v", result)
	}
	if len(f.runs.ran[0].Definition.Metrics) != len(advertising.TrendMetrics()) {
		t.Fatalf("a trend without metrics uses the daily ones: %+v", f.runs.ran[0].Definition)
	}
}

func savedReport() *advertising.SavedReport {
	return &advertising.SavedReport{
		ID: savedReportUUID, AdAccountID: adAccountUUID, Name: "Idade e gênero",
		Definition: advertising.ReportDefinition{
			View: advertising.ViewBars, Level: advertising.LevelCampaign, Breakdowns: []advertising.Breakdown{advertising.BreakdownGender},
			Metrics: []advertising.ReportMetric{advertising.ReportSpend, advertising.ReportCTR}, DatePreset: advertising.PresetCustom,
			Since: "2026-09-01", Until: "2026-09-15",
		},
	}
}

func TestSavedReportsAreListedAndRunWithTheirDefinition(t *testing.T) {
	f := newManageFixture()
	f.runs.run = genderBarsRun(2)
	f.reports.reports = []*advertising.SavedReport{savedReport()}
	listed := f.tool(t, "list_ad_reports").Execute(context.Background(), adContext, map[string]interface{}{})
	reports := listed.Data.(map[string]interface{})["reports"].([]map[string]interface{})
	if len(reports) != 1 || reports[0]["report_id"] != savedReportUUID || reports[0]["since"] != "2026-09-01" {
		t.Fatalf("listed %+v", listed)
	}
	result := f.tool(t, "run_saved_ad_report").Execute(context.Background(), reportContext(), map[string]interface{}{"report_id": savedReportUUID})
	if result.Status != copilot.StatusOK || result.Data.(map[string]interface{})["report"] != "Idade e gênero" {
		t.Fatalf("result %+v", result)
	}
	in := f.runs.ran[0]
	if in.Definition.View != advertising.ViewBars || in.Range.Since.Format(advertising.DayLayout) != "2026-09-01" || in.Range.Until.Format(advertising.DayLayout) != "2026-09-15" {
		t.Fatalf("input %+v", in)
	}
	missing := f.tool(t, "run_saved_ad_report").Execute(context.Background(), reportContext(), map[string]interface{}{"report_id": otherAdAccountUUID})
	if missing.Status != copilot.StatusError || len(f.runs.ran) != 1 {
		t.Fatalf("an unknown report must be refused: %+v", missing)
	}
}

func TestExportAdReportUsesPortugueseLabelsAndOpensTheReportsScreen(t *testing.T) {
	f := newManageFixture()
	f.runs.run = genderBarsRun(2)
	f.reports.reports = []*advertising.SavedReport{savedReport()}
	tool := f.tool(t, "export_ad_report")
	args := map[string]interface{}{"report_id": savedReportUUID}
	if err := validateTool(tool, args); err != nil {
		t.Fatal(err)
	}
	fields := describeTool(tool, args)
	if fields["name"] != "Idade e gênero" || fields["period"] != "01/09/2026 a 15/09/2026" || fields["breakdowns"] != "Gênero" || fields["metrics"] != "Valor gasto, CTR (taxa de cliques no link)" {
		t.Fatalf("fields %+v", fields)
	}
	if len(f.runs.ran) != 0 {
		t.Fatal("nothing runs at Meta before approval")
	}
	result := tool.Execute(context.Background(), adContext, args)
	if result.Status != copilot.StatusOK || result.Card == nil || result.Card.Destination.Screen != workspace.ScreenAdsReports {
		t.Fatalf("result %+v", result)
	}
	in := f.runs.exported
	labels := in.Labels
	if in.ReportID != savedReportUUID || in.UserID != adContext.UserID || labels.Object != "Nome da campanha" || labels.Metrics[advertising.ReportSpend] != "Valor gasto" ||
		labels.Values[advertising.BreakdownGender]["male"] != "Masculino" {
		t.Fatalf("export %+v", in)
	}
}

func TestExportAdReportRefusesABadReportBeforeApproval(t *testing.T) {
	f := newManageFixture()
	tool := f.tool(t, "export_ad_report")
	if err := validateTool(tool, map[string]interface{}{"ad_account_id": adAccountUUID, "metrics": []interface{}{"roas"}}); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("err %v", err)
	}
	if err := validateTool(tool, map[string]interface{}{"metrics": []interface{}{"spend"}}); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("an export without a report or an account must be refused: %v", err)
	}
	if err := validateTool(tool, map[string]interface{}{"report_id": savedReportUUID}); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("an unknown report must be refused: %v", err)
	}
	if f.runs.exported != nil {
		t.Fatal("nothing exported before approval")
	}
}
