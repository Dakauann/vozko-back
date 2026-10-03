package advertisinghttp

import (
	"bytes"
	"strings"
	"testing"

	"vozko/domain/report"
)

func int64Ptr(v int64) *int64       { return &v }
func float64Ptr(v float64) *float64 { return &v }

func TestReportCSVHasOneRowPerReportRowWithMoneyInMajorUnits(t *testing.T) {
	rep := ReportResponse{Rows: []RowResponse{
		{
			MetaID: "c-1", Name: "Campanha, com vírgula", Level: "campaign", Status: "ACTIVE", Delivery: "active",
			Metrics: MetricsResponse{
				Currency: "BRL", Spend: 12_345_678, Impressions: 1000, Clicks: 50, LinkClicks: 40, Results: 4,
				ResultAction: "lead", CostPerResult: int64Ptr(3_086_420), CTR: float64Ptr(4), CPC: int64Ptr(308_642), CPM: int64Ptr(12_345_678),
				Conversations: 2, CostPerConversation: int64Ptr(6_172_839),
			},
			Outcome: OutcomeResponse{Leads: 3, CostPerLead: int64Ptr(4_115_226), WonDeals: 1, Revenue: 100_000_000, ROAS: float64Ptr(8.1)},
		},
		{MetaID: "c-2", Name: "Sem gasto", Level: "campaign", Metrics: MetricsResponse{Currency: "BRL"}},
	}}

	var out bytes.Buffer
	if err := writeReportCSV(&out, rep); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), report.UTF8BOM) {
		t.Fatal("missing the UTF-8 byte order mark spreadsheets need for accents")
	}
	var records [][]string
	for _, line := range strings.Split(strings.TrimSuffix(strings.TrimPrefix(out.String(), report.UTF8BOM), report.CSVNewline), report.CSVNewline) {
		records = append(records, strings.Split(line, report.CSVDelimiter))
	}
	if len(records) != 3 {
		t.Fatalf("got %d records, want header plus 2 rows", len(records))
	}
	header, first, empty := records[0], records[1], records[2]
	column := func(row []string, name string) string {
		for i, h := range header {
			if h == name {
				return row[i]
			}
		}
		t.Fatalf("no column %q", name)
		return ""
	}
	want := map[string]string{
		"Nome": "Campanha, com vírgula", "Moeda": "BRL", "Gasto": "12,35", "Impressões": "1000", "Resultados": "4",
		"Custo por resultado": "3,09", "CTR (%)": "4", "CPC": "0,31", "Receita": "100", "ROAS": "8,1", "Custo por lead": "4,12",
	}
	for name, value := range want {
		if got := column(first, name); got != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}
	for _, name := range []string{"Custo por resultado", "CTR (%)", "CPC", "CPM", "Custo por conversa", "Custo por lead", "ROAS"} {
		if got := column(empty, name); got != "" {
			t.Errorf("%s without data = %q, want blank", name, got)
		}
	}
	if got := column(empty, "Gasto"); got != "0" {
		t.Errorf("zero spend = %q", got)
	}
}
