package advertising

import (
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/report"
)

func TestPivotCSVUsesThePersonsLabelsAndCurrencyUnits(t *testing.T) {
	def := ReportDefinition{View: ViewPivot, Level: LevelCampaign, Breakdowns: []Breakdown{BreakdownGender}, Metrics: []ReportMetric{ReportSpend, ReportImpressions, ReportCTR}}
	table, err := BuildReportTable(def, []LiveRow{liveRow("c-1", map[Breakdown]string{BreakdownGender: "female"}, 12_500_000, 1000, 0, "", 1)}, map[string]string{"c-1": "=Promo"})
	if err != nil {
		t.Fatal(err)
	}
	labels := ExportLabels{
		Object: "Campanha", Total: "Total",
		Breakdowns: map[Breakdown]string{BreakdownGender: "Gênero"},
		Values:     map[Breakdown]map[string]string{BreakdownGender: {"female": "Mulheres"}},
		Metrics:    map[ReportMetric]string{ReportSpend: "Valor gasto", ReportImpressions: "Impressões"},
	}
	doc := report.BuildCSVDocument([]report.CSVSection{table.CSVSection(labels)}, true)
	want := report.UTF8BOM + "Campanha;Gênero;Valor gasto;Impressões;ctr\r\n'=Promo;Mulheres;12,5;1000;10\r\nTotal;;12,5;1000;10\r\n"
	if doc != want {
		t.Fatalf("got %q", doc)
	}
}

func TestSeriesCSVHasOneLinePerDay(t *testing.T) {
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	series := BuildReportSeries([]ReportMetric{ReportSpend}, []DayMetrics{{Day: day, Metrics: Metrics{SpendMicros: 2_000_000}}})
	doc := report.BuildCSVDocument([]report.CSVSection{SeriesCSVSection([]ReportMetric{ReportSpend}, series, ExportLabels{Day: "Dia"})}, false)
	if doc != "Dia;spend\r\n2026-10-01;2\r\n" {
		t.Fatalf("got %q", doc)
	}
}

func TestReportExportBounds(t *testing.T) {
	r := DateRange{}
	if _, err := NewReportExport("ws", "acc", "", "u", " ", r, 0, nil); err == nil || !strings.Contains(err.Error(), "name:required") {
		t.Fatalf("got %v", err)
	}
	if _, err := NewReportExport("ws", "acc", "", "u", "Mensal", r, 0, make([]byte, MaxReportExportBytes+1)); !errors.Is(err, ErrReportExportTooLarge) {
		t.Fatalf("got %v", err)
	}
	e, err := NewReportExport("ws", "acc", "r-1", "u", " Mensal ", r, 3, []byte("x"))
	if err != nil || e.Name != "Mensal" || e.Rows != 3 || e.ReportID != "r-1" {
		t.Fatalf("got %+v %v", e, err)
	}
}
