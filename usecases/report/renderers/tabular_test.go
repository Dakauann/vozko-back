package report_renderers

import (
	"strings"
	"testing"

	"vozko/domain/report"
)

func TestParseCSVTableStripsTheBOMAndKeepsTheHeader(t *testing.T) {
	data := []byte(report.UTF8BOM + "Date,Type,Amount\r\n2026-09-01,debit,-0.026500\r\n")

	table, err := parseCSVTable(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(table.Columns) != 3 || table.Columns[0] != "Date" {
		t.Fatalf("columns = %#v; the BOM leaked into the first header", table.Columns)
	}
	if table.Total != 1 || len(table.Rows) != 1 {
		t.Fatalf("rows = %#v", table.Rows)
	}
	if table.Rows[0][2] != "-0.026500" {
		t.Fatalf("cell = %q", table.Rows[0][2])
	}
	if table.Truncated {
		t.Fatal("a one-row table is not truncated")
	}
}

func TestParseCSVTableCapsRowsButReportsTheTrueTotal(t *testing.T) {
	var builder strings.Builder
	builder.WriteString("a,b\n")
	for i := 0; i < maxPrintRows+25; i++ {
		builder.WriteString("1,2\n")
	}

	table, err := parseCSVTable([]byte(builder.String()))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(table.Rows) != maxPrintRows {
		t.Fatalf("kept %d rows, want the %d cap", len(table.Rows), maxPrintRows)
	}
	if table.Total != maxPrintRows+25 {
		t.Fatalf("total = %d, want the real count", table.Total)
	}
	if !table.Truncated {
		t.Fatal("a capped table must say so, never imply the period was that small")
	}
}

func TestParseCSVTableHandlesAnEmptyExport(t *testing.T) {
	table, err := parseCSVTable(nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(table.Columns) != 0 || len(table.Rows) != 0 {
		t.Fatalf("table = %#v", table)
	}
}

func TestParseCSVTableKeepsQuotedCommas(t *testing.T) {
	table, err := parseCSVTable([]byte("name,note\n\"Ana, Silva\",\"said: a,b\"\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if table.Rows[0][0] != "Ana, Silva" {
		t.Fatalf("name = %q", table.Rows[0][0])
	}
	if table.Rows[0][1] != "said: a,b" {
		t.Fatalf("note = %q", table.Rows[0][1])
	}
}
