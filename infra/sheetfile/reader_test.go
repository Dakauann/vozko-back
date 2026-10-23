package sheetfile

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/xuri/excelize/v2"

	"vozko/domain/sheet"
)

func xlsxOf(t *testing.T, rows [][]any) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	for i, row := range rows {
		cell, err := excelize.CoordinatesToCellName(1, i+1)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.SetSheetRow("Sheet1", cell, &row); err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestReadKeepsDelimitedTextExactlyAsTheSheetParserReadsIt(t *testing.T) {
	data := []byte("\xEF\xBB\xBFtelefone;nome\n\n5511900010142;Maria\n")
	got, err := Reader{}.Read(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, sheet.Parse(data)) {
		t.Fatalf("rows = %+v", got)
	}
}

func TestReadTakesTheFirstSheetOfAWorkbookWithRawNumbers(t *testing.T) {
	data := xlsxOf(t, [][]any{
		{"telefone", "nome", "var1"},
		{5511900010142, "Maria", "1º ano"},
		{},
		{"(11) 90002-3301", "João", 7},
	})
	got, err := Reader{}.Read(data)
	if err != nil {
		t.Fatal(err)
	}
	want := []sheet.Row{
		{Line: 1, Cells: []string{"telefone", "nome", "var1"}},
		{Line: 2, Cells: []string{"5511900010142", "Maria", "1º ano"}},
		{Line: 4, Cells: []string{"(11) 90002-3301", "João", "7"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %+v\nwant %+v", got, want)
	}
}

func TestReadKeepsColumnsInPlaceWhenACellIsEmpty(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()
	for cell, value := range map[string]string{"A1": "telefone", "C1": "var2", "A2": "5511900010142", "C2": "Centro"} {
		if err := f.SetCellValue("Sheet1", cell, value); err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	got, err := Reader{}.Read(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got[1].Cells, []string{"5511900010142", "", "Centro"}) {
		t.Fatalf("cells = %q", got[1].Cells)
	}
}

func TestReadRefusesABrokenWorkbook(t *testing.T) {
	if _, err := (Reader{}).Read([]byte("PK\x03\x04not really a zip")); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("err = %v", err)
	}
}
