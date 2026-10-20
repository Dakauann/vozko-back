package report_xlsx

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/xuri/excelize/v2"
)

func readBack(t *testing.T, data []byte, sheet string) [][]string {
	t.Helper()
	file, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("the workbook does not open: %v", err)
	}
	defer file.Close()
	if got := file.GetSheetList(); !reflect.DeepEqual(got, []string{sheet}) {
		t.Fatalf("sheets = %v, want only %q", got, sheet)
	}
	rows, err := file.GetRows(sheet)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestASheetHoldsTheHeaderAndEveryRowAsText(t *testing.T) {
	sheet, err := NewSheet("Respostas", []string{"Nome", "Valor"})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range [][]string{{"Ana", "=1+1"}, {"Caio", "007"}} {
		if err := sheet.Append(row); err != nil {
			t.Fatal(err)
		}
	}
	var buffer bytes.Buffer
	if err := sheet.Write(&buffer); err != nil {
		t.Fatal(err)
	}
	if sheet.Rows() != 2 {
		t.Fatalf("rows = %d", sheet.Rows())
	}
	want := [][]string{{"Nome", "Valor"}, {"Ana", "=1+1"}, {"Caio", "007"}}
	if got := readBack(t, buffer.Bytes(), "Respostas"); !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	file, err := excelize.OpenReader(bytes.NewReader(buffer.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if formula, _ := file.GetCellFormula("Respostas", "B2"); formula != "" {
		t.Fatalf("a text cell became a formula: %q", formula)
	}
}

func TestWriteTableWritesAWholeTable(t *testing.T) {
	var buffer bytes.Buffer
	if err := WriteTable(&buffer, "Transactions", []string{"Date"}, [][]string{{"2026-04-09"}}); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"Date"}, {"2026-04-09"}}
	if got := readBack(t, buffer.Bytes(), "Transactions"); !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %v", got)
	}
}

func TestASheetNeedsAName(t *testing.T) {
	if _, err := NewSheet("", []string{"A"}); err == nil {
		t.Fatal("a sheet without a name must be refused")
	}
}
