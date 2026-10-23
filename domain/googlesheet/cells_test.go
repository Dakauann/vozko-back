package googlesheet

import (
	"testing"
)

func TestEncodeRowWritesTheReferenceThenEveryColumnInOrder(t *testing.T) {
	s := typedSheet(t)
	cells, err := s.Accept(map[string]any{
		"nome": "Ana", "quantidade": "2", "data": "2026-10-21", "visita": "2026-10-21 18:00", "aceitou": "sim", "status": "Novo",
	}, system, acceptNow)
	if err != nil {
		t.Fatal(err)
	}
	row, err := s.EncodeRow("row-1", cells)
	if err != nil {
		t.Fatalf("EncodeRow: %v", err)
	}
	if len(row) != len(s.Columns)+1 || row[0] != "row-1" {
		t.Fatalf("row = %#v", row)
	}
	byKey := map[string]any{}
	for i, col := range s.Columns {
		byKey[col.Key] = row[i+1]
	}
	if byKey["nome"] != "Ana" || byKey["quantidade"] != 2.0 || byKey["aceitou"] != true || byKey["status"] != "Novo" {
		t.Fatalf("encoded = %#v", byKey)
	}
	if byKey["data"] != 46316.0 {
		t.Fatalf("data serial = %#v, want 46316", byKey["data"])
	}
	if byKey["visita"] != 46316.75 {
		t.Fatalf("visita serial = %#v, want 46316.75 (18:00 on the sheet clock)", byKey["visita"])
	}
	if byKey["detalhes"] != "" {
		t.Fatalf("empty cell = %#v, want empty string", byKey["detalhes"])
	}
}

func TestEncodeRowUsesCurrentColumnsForOlderRows(t *testing.T) {
	s := typedSheet(t)
	cells, _ := s.Accept(map[string]any{"nome": "Ana"}, system, acceptNow)
	if err := s.EditColumns(ColumnsEdit{Add: []ColumnInput{{Header: "Origem", Type: TypeText}}}); err != nil {
		t.Fatal(err)
	}
	row, err := s.EncodeRow("row-1", cells)
	if err != nil {
		t.Fatal(err)
	}
	if len(row) != len(s.Columns)+1 || row[len(row)-1] != "" {
		t.Fatalf("row = %#v", row)
	}
}

func TestEncodeRowRefusesAStoredValueThatNoLongerFits(t *testing.T) {
	s := typedSheet(t)
	if _, err := s.EncodeRow("row-1", Cells{"quantidade": "not a number"}); err == nil {
		t.Fatal("a corrupt stored value was encoded")
	}
}

func TestSerialDateMatchesSpreadsheetEpoch(t *testing.T) {
	cases := map[string]float64{"1899-12-30": 0, "1900-03-01": 61, "2026-10-21": 46316}
	for date, want := range cases {
		got, err := serialOfDate(date)
		if err != nil || got != want {
			t.Fatalf("serialOfDate(%s) = %v, %v; want %v", date, got, err, want)
		}
	}
}
