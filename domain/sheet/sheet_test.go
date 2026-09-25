package sheet

import (
	"reflect"
	"testing"
)

func TestParseDetectsTheDelimiterAndKeepsLineNumbers(t *testing.T) {
	rows := Parse([]byte("\xEF\xBB\xBFnumero;nome\n\n5584994409624;Maria\n"))
	want := []Row{{Line: 1, Cells: []string{"numero", "nome"}}, {Line: 3, Cells: []string{"5584994409624", "Maria"}}}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestParseHonoursQuotes(t *testing.T) {
	rows := Parse([]byte("nome,obs\n\"Silva, Maria\",\"disse \"\"oi\"\"\"\n"))
	if got := rows[1].Cells; got[0] != "Silva, Maria" || got[1] != `disse "oi"` {
		t.Fatalf("cells = %q", got)
	}
}

func TestParsePrefersTabThenSemicolon(t *testing.T) {
	if got := Parse([]byte("a\tb;c\n"))[0].Cells; len(got) != 2 {
		t.Fatalf("tab: %q", got)
	}
	if got := Parse([]byte("a;b,c\n"))[0].Cells; len(got) != 2 || got[1] != "b,c" {
		t.Fatalf("semicolon: %q", got)
	}
}

func TestParseFallsBackToWindows1252(t *testing.T) {
	rows := Parse([]byte("nome\nJo\xe3o\n"))
	if rows[1].Cells[0] != "João" {
		t.Fatalf("cell = %q", rows[1].Cells[0])
	}
}

func TestParseOfNothingIsEmpty(t *testing.T) {
	if rows := Parse([]byte("\n  \n")); len(rows) != 0 {
		t.Fatalf("rows = %+v", rows)
	}
}
