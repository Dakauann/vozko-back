package googlesheet

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var acceptNow = time.Date(2026, 10, 21, 15, 30, 0, 0, time.UTC)

func typedSheet(t *testing.T) *Sheet {
	t.Helper()
	s, err := NewSheet(NewSheetInput{
		WorkspaceID: "ws-1", AccountID: "acct-1", Title: "Leads", TabName: "Leads", TimeZone: "America/Sao_Paulo",
		Columns: []ColumnInput{
			{Header: "Nome", Type: TypeText, Required: true},
			{Header: "Detalhes", Type: TypeLongText},
			{Header: "Quantidade", Type: TypeNumber},
			{Header: "Valor", Type: TypeMoney},
			{Header: "Data", Type: TypeDate},
			{Header: "Visita", Type: TypeDateTime},
			{Header: "Aceitou", Type: TypeBoolean},
			{Header: "Status", Type: TypeChoice, Options: []string{"Novo", "Em negociação"}},
			{Header: "Email", Type: TypeEmail},
			{Header: "Celular", Type: TypePhone},
			{Header: "Site", Type: TypeURL},
			{Header: "Telefone do lead", Type: TypePhone, Source: SourceLeadPhone, Required: true},
			{Header: "Gravado em", Type: TypeDateTime, Source: SourceWrittenAt},
		},
	})
	if err != nil {
		t.Fatalf("NewSheet: %v", err)
	}
	return s
}

var system = SystemValues{SourceLeadPhone: "+5511999990000"}

func TestAcceptNormalizesEveryType(t *testing.T) {
	s := typedSheet(t)
	cells, err := s.Accept(map[string]any{
		"nome":       "  Ana Souza ",
		"quantidade": "3",
		"valor":      "R$ 1.234,50",
		"data":       "21/10/2026",
		"visita":     "2026-10-22 14:00",
		"aceitou":    "sim",
		"status":     "em negociacao",
		"email":      "Ana@Example.COM",
		"celular":    "(11) 98888-7777",
		"site":       "https://example.com/a",
	}, system, acceptNow)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	want := map[string]any{
		"nome":             "Ana Souza",
		"quantidade":       3.0,
		"valor":            1234.5,
		"data":             "2026-10-21",
		"visita":           "2026-10-22T14:00:00-03:00",
		"aceitou":          true,
		"status":           "Em negociação",
		"email":            "ana@example.com",
		"celular":          "+5511988887777",
		"site":             "https://example.com/a",
		"telefone_do_lead": "+5511999990000",
		"gravado_em":       "2026-10-21T12:30:00-03:00",
	}
	for key, value := range want {
		if cells[key] != value {
			t.Fatalf("%s = %#v, want %#v", key, cells[key], value)
		}
	}
	if _, ok := cells["detalhes"]; ok {
		t.Fatal("an omitted optional column must stay empty, not be invented")
	}
}

func TestAcceptTakesTypedJSONValues(t *testing.T) {
	s := typedSheet(t)
	cells, err := s.Accept(map[string]any{"nome": "Ana", "quantidade": 2.5, "aceitou": false, "valor": 10}, system, acceptNow)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if cells["quantidade"] != 2.5 || cells["aceitou"] != false || cells["valor"] != 10.0 {
		t.Fatalf("cells = %#v", cells)
	}
}

func TestAcceptKeepsFormulaLookingTextAsText(t *testing.T) {
	s := typedSheet(t)
	cells, err := s.Accept(map[string]any{"nome": "=IMPORTXML(\"http://x\")"}, system, acceptNow)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if cells["nome"] != "=IMPORTXML(\"http://x\")" {
		t.Fatalf("nome = %#v", cells["nome"])
	}
}

func TestAcceptRefusesWithTheColumnNamed(t *testing.T) {
	cases := []struct {
		name   string
		values map[string]any
		system SystemValues
		err    error
		column string
	}{
		{name: "unknown column", values: map[string]any{"nome": "a", "cpf": "1"}, system: system, err: ErrUnknownColumn, column: "cpf"},
		{name: "required missing", values: map[string]any{"quantidade": 1}, system: system, err: ErrRequiredColumnMissing, column: "nome"},
		{name: "required blank", values: map[string]any{"nome": "   "}, system: system, err: ErrRequiredColumnMissing, column: "nome"},
		{name: "number text", values: map[string]any{"nome": "a", "quantidade": "três"}, system: system, err: ErrInvalidValue, column: "quantidade"},
		{name: "bad date", values: map[string]any{"nome": "a", "data": "amanhã"}, system: system, err: ErrInvalidValue, column: "data"},
		{name: "bad boolean", values: map[string]any{"nome": "a", "aceitou": "talvez"}, system: system, err: ErrInvalidValue, column: "aceitou"},
		{name: "option outside the list", values: map[string]any{"nome": "a", "status": "Perdido"}, system: system, err: ErrValueNotInOptions, column: "status"},
		{name: "bad email", values: map[string]any{"nome": "a", "email": "ana@"}, system: system, err: ErrInvalidValue, column: "email"},
		{name: "bad phone", values: map[string]any{"nome": "a", "celular": "123"}, system: system, err: ErrInvalidValue, column: "celular"},
		{name: "url without scheme", values: map[string]any{"nome": "a", "site": "javascript:alert(1)"}, system: system, err: ErrInvalidValue, column: "site"},
		{name: "object value", values: map[string]any{"nome": map[string]any{"a": 1}}, system: system, err: ErrInvalidValue, column: "nome"},
		{name: "too long", values: map[string]any{"nome": strings.Repeat("a", MaxCellRunes+1)}, system: system, err: ErrValueTooLong, column: "nome"},
		{name: "system column overridden", values: map[string]any{"nome": "a", "telefone_do_lead": "+5511000000000"}, system: system, err: ErrSystemColumn, column: "telefone_do_lead"},
		{name: "required system value unresolved", values: map[string]any{"nome": "a"}, system: SystemValues{}, err: ErrRequiredColumnMissing, column: "telefone_do_lead"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := typedSheet(t).Accept(c.values, c.system, acceptNow)
			if !errors.Is(err, c.err) {
				t.Fatalf("err = %v, want %v", err, c.err)
			}
			var valueErr *ValueError
			if !errors.As(err, &valueErr) || valueErr.Column != c.column {
				t.Fatalf("err %v does not name column %q", err, c.column)
			}
		})
	}
}

func TestAcceptRefusesArchivedColumnsAndInactiveSheets(t *testing.T) {
	s := typedSheet(t)
	if err := s.EditColumns(ColumnsEdit{Update: []ColumnUpdate{{Key: "detalhes", Archived: ptrBool(true)}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Accept(map[string]any{"nome": "a", "detalhes": "x"}, system, acceptNow); !errors.Is(err, ErrColumnArchived) {
		t.Fatalf("archived err = %v", err)
	}
	s.NeedsAttention(ReasonAccessLost, "")
	if _, err := s.Accept(map[string]any{"nome": "a"}, system, acceptNow); !errors.Is(err, ErrSheetNotActive) {
		t.Fatalf("inactive err = %v", err)
	}
}

func TestAcceptRefusesARowTooLargeForOneRequest(t *testing.T) {
	cols := make([]ColumnInput, 0, 40)
	values := map[string]any{}
	for i := 0; i < 40; i++ {
		header := "Coluna " + string(rune('A'+i%26)) + strings.Repeat("x", i/26)
		cols = append(cols, ColumnInput{Header: header, Type: TypeLongText})
		values[KeyFromHeader(header, nil)] = strings.Repeat("a", MaxCellRunes)
	}
	s, err := NewSheet(NewSheetInput{WorkspaceID: "w", AccountID: "a", Title: "Big", TabName: "Big", TimeZone: "UTC", Columns: cols})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Accept(values, nil, acceptNow); !errors.Is(err, ErrRowTooLarge) {
		t.Fatalf("err = %v, want ErrRowTooLarge", err)
	}
}
