package advertising

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	ads "vozko/domain/advertising"
)

type scriptedLive struct {
	rows  []ads.LiveRow
	query ads.LiveQuery
}

func (s *scriptedLive) Insights(_ context.Context, q ads.LiveQuery) (*LiveReport, error) {
	s.query = q
	return &LiveReport{Account: &ads.AdAccount{ID: "acc-1", Currency: "BRL"}, Query: q, Rows: s.rows}, nil
}

type scriptedTrend struct{ points []TrendPoint }

func (s *scriptedTrend) Trend(context.Context, ReportQuery) (*Trend, error) {
	return &Trend{Account: &ads.AdAccount{ID: "acc-1", Currency: "BRL"}, Points: s.points}, nil
}

type memoryExports struct {
	created []*ads.ReportExport
	kept    int
}

func (m *memoryExports) Create(_ context.Context, e *ads.ReportExport) error {
	e.ID = "e-1"
	m.created = append(m.created, e)
	return nil
}
func (m *memoryExports) Find(context.Context, string, string) (*ads.ReportExport, error) {
	return nil, ads.ErrReportExportNotFound
}
func (m *memoryExports) List(context.Context, string, int) ([]*ads.ReportExport, error) {
	return nil, nil
}
func (m *memoryExports) Delete(context.Context, string, string) error { return nil }
func (m *memoryExports) KeepNewest(_ context.Context, _ string, keep int) error {
	m.kept = keep
	return nil
}

func runsFixture(rows []ads.LiveRow) (*ReportRunsUseCase, *scriptedLive, *memoryExports, *memoryReports) {
	live := &scriptedLive{rows: rows}
	objects := &fakeObjects{byID: map[string]*ads.Object{"c-1": {MetaID: "c-1", WorkspaceID: "ws", AdAccountID: "acc-1", Level: ads.LevelCampaign, Name: "Aurora"}}}
	exports := &memoryExports{}
	reports := &memoryReports{byID: map[string]*ads.SavedReport{
		"r-1": {ID: "r-1", WorkspaceID: "ws", AdAccountID: "acc-1"},
		"r-2": {ID: "r-2", WorkspaceID: "ws", AdAccountID: "acc-2"},
	}, opened: map[string]time.Time{}}
	trend := &scriptedTrend{points: []TrendPoint{{Day: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), Metrics: ads.Metrics{SpendMicros: 2_000_000}}}}
	return NewReportRunsUseCase(live, trend, objects, reports, exports), live, exports, reports
}

func september() ads.DateRange {
	return ads.DateRange{Since: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), Until: time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)}
}

func campaignRun() ReportRunInput {
	return ReportRunInput{
		WorkspaceID: "ws", AccountID: "acc-1", Range: september(), Windows: []ads.AttributionWindow{ads.Window7DayClick},
		Definition: ads.ReportDefinition{View: ads.ViewPivot, Level: ads.LevelCampaign, Metrics: []ads.ReportMetric{ads.ReportSpend}, DatePreset: "last30"},
	}
}

func TestARunNamesTheObjectsAndPassesTheWindows(t *testing.T) {
	uc, live, _, _ := runsFixture([]ads.LiveRow{{ObjectID: "c-1", Values: ads.LiveMetrics{Metrics: ads.Metrics{Currency: "BRL", SpendMicros: 1_000_000}}}})
	run, err := uc.Run(context.Background(), campaignRun())
	if err != nil {
		t.Fatal(err)
	}
	if run.Currency != "BRL" || len(run.Table.Rows) != 1 || run.Table.Rows[0].Name != "Aurora" || live.query.Windows[0] != ads.Window7DayClick {
		t.Fatalf("got %+v query %+v", run, live.query)
	}
}

func TestARunRefusesAnInvalidDefinitionOrRange(t *testing.T) {
	uc, _, _, _ := runsFixture(nil)
	in := campaignRun()
	in.Definition.Metrics = nil
	if _, err := uc.Run(context.Background(), in); err == nil {
		t.Fatal("a definition without metrics must be refused")
	}
	in = campaignRun()
	in.Range = ads.DateRange{}
	if _, err := uc.Run(context.Background(), in); !errors.Is(err, ads.ErrInvalidRange) {
		t.Fatalf("got %v", err)
	}
}

func TestATrendRunReturnsTheDays(t *testing.T) {
	uc, _, _, _ := runsFixture(nil)
	in := campaignRun()
	in.Definition.View = ads.ViewTrend
	run, err := uc.Run(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if run.Rows() != 1 || *run.Series[0].Values[ads.ReportSpend] != 2_000_000 {
		t.Fatalf("got %+v", run)
	}
}

func TestAnExportStoresTheCSVAndKeepsTheNewest(t *testing.T) {
	uc, _, exports, _ := runsFixture([]ads.LiveRow{{ObjectID: "c-1", Values: ads.LiveMetrics{Metrics: ads.Metrics{Currency: "BRL", SpendMicros: 1_000_000}}}})
	export, err := uc.Export(context.Background(), ReportExportInput{
		ReportRunInput: campaignRun(), UserID: "u-1", Name: "Mensal", ReportID: "r-1",
		Labels: ads.ExportLabels{Object: "Campanha", Total: "Total", Metrics: map[ads.ReportMetric]string{ads.ReportSpend: "Valor gasto"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if export.Rows != 1 || !strings.Contains(string(export.Content), "Campanha;Valor gasto") || exports.kept != ads.KeptReportExports || len(exports.created) != 1 {
		t.Fatalf("got %+v kept %d", export, exports.kept)
	}
}

func TestAnExportOfAnotherAccountsReportIsRefused(t *testing.T) {
	uc, _, exports, _ := runsFixture(nil)
	_, err := uc.Export(context.Background(), ReportExportInput{ReportRunInput: campaignRun(), UserID: "u-1", Name: "Mensal", ReportID: "r-2"})
	if !errors.Is(err, ads.ErrReportNotFound) || len(exports.created) != 0 {
		t.Fatalf("got %v", err)
	}
}
