package googlesheet

import (
	"errors"
	"strings"
	"testing"
)

func validInput() NewSheetInput {
	return NewSheetInput{
		WorkspaceID: "ws-1",
		AccountID:   "acct-1",
		Title:       "Pedidos",
		TabName:     "Pedidos",
		TimeZone:    "America/Sao_Paulo",
		CreatedBy:   "user-1",
		Columns: []ColumnInput{
			{Header: "Nome do cliente", Type: TypeText, Required: true, Instruction: "Nome completo"},
			{Header: "Valor", Type: TypeMoney},
			{Header: "Status", Type: TypeChoice, Options: []string{"Novo", "Pago"}},
			{Header: "Telefone", Type: TypePhone, Source: SourceLeadPhone},
			{Header: "Registrado em", Type: TypeDateTime, Source: SourceWrittenAt},
		},
	}
}

func TestNewSheetDerivesStableKeysFromHeaders(t *testing.T) {
	s, err := NewSheet(validInput())
	if err != nil {
		t.Fatalf("NewSheet: %v", err)
	}
	want := []string{"nome_do_cliente", "valor", "status", "telefone", "registrado_em"}
	for i, col := range s.Columns {
		if col.Key != want[i] {
			t.Fatalf("column %d key = %q, want %q", i, col.Key, want[i])
		}
	}
	if s.Status != StatusActive || s.Version != 1 || s.Locale != DefaultLocale {
		t.Fatalf("sheet = %+v", s)
	}
}

func TestKeyFromHeaderIsUniqueAndSafe(t *testing.T) {
	cases := []struct {
		header string
		taken  []string
		want   string
	}{
		{header: "Ação tomada", want: "acao_tomada"},
		{header: "  2º Contato!! ", want: "c_2o_contato"},
		{header: "Valor", taken: []string{"valor"}, want: "valor_2"},
		{header: "Valor", taken: []string{"valor", "valor_2"}, want: "valor_3"},
		{header: "###", want: "coluna"},
		{header: strings.Repeat("a", 90), want: strings.Repeat("a", MaxColumnKeyLength)},
	}
	for _, c := range cases {
		if got := KeyFromHeader(c.header, c.taken); got != c.want {
			t.Fatalf("KeyFromHeader(%q, %v) = %q, want %q", c.header, c.taken, got, c.want)
		}
	}
}

func TestNewSheetRefusesInvalidDefinitions(t *testing.T) {
	cases := map[string]func(*NewSheetInput){
		"no title":                 func(in *NewSheetInput) { in.Title = " " },
		"no account":               func(in *NewSheetInput) { in.AccountID = "" },
		"no columns":               func(in *NewSheetInput) { in.Columns = nil },
		"only system columns":      func(in *NewSheetInput) { in.Columns = in.Columns[3:] },
		"duplicate header":         func(in *NewSheetInput) { in.Columns[1].Header = "nome do cliente" },
		"blank header":             func(in *NewSheetInput) { in.Columns[1].Header = "  " },
		"choice without options":   func(in *NewSheetInput) { in.Columns[2].Options = nil },
		"repeated option":          func(in *NewSheetInput) { in.Columns[2].Options = []string{"Novo", "novo"} },
		"options on a text":        func(in *NewSheetInput) { in.Columns[0].Options = []string{"a"} },
		"unknown type":             func(in *NewSheetInput) { in.Columns[0].Type = "currency" },
		"unknown source":           func(in *NewSheetInput) { in.Columns[3].Source = "lead_cpf" },
		"source of the wrong type": func(in *NewSheetInput) { in.Columns[4].Type = TypeNumber },
		"instruction too long":     func(in *NewSheetInput) { in.Columns[0].Instruction = strings.Repeat("a", MaxInstructionRunes+1) },
		"unknown time zone":        func(in *NewSheetInput) { in.TimeZone = "Mars/Olympus" },
		"tab name with quote":      func(in *NewSheetInput) { in.TabName = "Pedidos'" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			in := validInput()
			mutate(&in)
			_, err := NewSheet(in)
			if !errors.Is(err, ErrInvalidSheet) {
				t.Fatalf("err = %v, want ErrInvalidSheet", err)
			}
		})
	}
}

func TestHeaderRowStartsWithTheHiddenReference(t *testing.T) {
	s, _ := NewSheet(validInput())
	header := s.HeaderRow()
	if header[0] != RefHeader || header[1] != "Nome do cliente" || len(header) != len(s.Columns)+1 {
		t.Fatalf("header = %v", header)
	}
}

func TestEditColumnsAddsRenamesAndArchives(t *testing.T) {
	s, _ := NewSheet(validInput())
	header := "Valor total"
	instruction := "Some os itens"
	archived := true
	err := s.EditColumns(ColumnsEdit{
		Add:    []ColumnInput{{Header: "Observação", Type: TypeLongText}},
		Update: []ColumnUpdate{{Key: "valor", Header: &header, Instruction: &instruction}, {Key: "status", Archived: &archived}},
	})
	if err != nil {
		t.Fatalf("EditColumns: %v", err)
	}
	if s.Version != 2 {
		t.Fatalf("version = %d, want 2", s.Version)
	}
	valor, _ := s.Column("valor")
	if valor.Header != "Valor total" || valor.Instruction != "Some os itens" {
		t.Fatalf("valor = %+v", valor)
	}
	if status, _ := s.Column("status"); !status.Archived {
		t.Fatal("status not archived")
	}
	if last := s.Columns[len(s.Columns)-1]; last.Key != "observacao" {
		t.Fatalf("new column key = %q, want it appended at the end", last.Key)
	}
}

func TestEditColumnsRefusesBreakingChanges(t *testing.T) {
	cases := map[string]ColumnsEdit{
		"unknown column":          {Update: []ColumnUpdate{{Key: "nope", Header: ptr("x")}}},
		"duplicate header":        {Update: []ColumnUpdate{{Key: "valor", Header: ptr("Status")}}},
		"archive every input":     {Update: []ColumnUpdate{{Key: "nome_do_cliente", Archived: ptrBool(true)}, {Key: "valor", Archived: ptrBool(true)}, {Key: "status", Archived: ptrBool(true)}}},
		"drop an option in use":   {Update: []ColumnUpdate{{Key: "status", Options: []string{"Novo"}}}},
		"options on a non choice": {Update: []ColumnUpdate{{Key: "valor", Options: []string{"1"}}}},
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			s, _ := NewSheet(validInput())
			if err := s.EditColumns(edit); err == nil {
				t.Fatal("edit accepted")
			}
			if s.Version != 1 {
				t.Fatal("a refused edit changed the sheet")
			}
		})
	}
}

func TestEditColumnsCanAddChoiceOptions(t *testing.T) {
	s, _ := NewSheet(validInput())
	if err := s.EditColumns(ColumnsEdit{Update: []ColumnUpdate{{Key: "status", Options: []string{"Novo", "Pago", "Cancelado"}}}}); err != nil {
		t.Fatalf("EditColumns: %v", err)
	}
	status, _ := s.Column("status")
	if len(status.Options) != 3 {
		t.Fatalf("options = %v", status.Options)
	}
}

func TestWritableReflectsStatus(t *testing.T) {
	s, _ := NewSheet(validInput())
	if err := s.Writable(); err != nil {
		t.Fatalf("active sheet not writable: %v", err)
	}
	s.NeedsAttention(ReasonHeaderMismatch, "coluna B renomeada")
	if err := s.Writable(); !errors.Is(err, ErrSheetNotActive) {
		t.Fatalf("Writable = %v, want ErrSheetNotActive", err)
	}
	s.Activate()
	if s.Writable() != nil || s.Reason != "" {
		t.Fatalf("sheet not reactivated: %+v", s)
	}
}

func TestURLPointsAtTheTab(t *testing.T) {
	s, _ := NewSheet(validInput())
	s.SpreadsheetID, s.TabID = "abc123", 42
	if got := s.URL(); got != "https://docs.google.com/spreadsheets/d/abc123/edit#gid=42" {
		t.Fatalf("URL = %q", got)
	}
}

func ptr(s string) *string { return &s }
func ptrBool(b bool) *bool { return &b }

func TestRenameValidatesAndBumpsTheVersion(t *testing.T) {
	s, _ := NewSheet(validInput())
	if err := s.Rename(ptr("Pedidos 2026"), ptr("Pedidos do site")); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if s.Title != "Pedidos 2026" || s.Description != "Pedidos do site" || s.Version != 2 {
		t.Fatalf("sheet = %+v", s)
	}
	if err := s.Rename(ptr("  "), nil); !errors.Is(err, ErrInvalidSheet) {
		t.Fatalf("blank title err = %v", err)
	}
	if s.Title != "Pedidos 2026" || s.Version != 2 {
		t.Fatal("a refused rename changed the sheet")
	}
}

func TestHeaderDifference(t *testing.T) {
	s, _ := NewSheet(validInput())
	expected := s.HeaderRow()
	asRow := func(cells ...string) [][]any {
		row := make([]any, len(cells))
		for i, c := range cells {
			row[i] = c
		}
		return [][]any{row}
	}
	if got := HeaderDifference(expected, asRow(expected...)); got != "" {
		t.Fatalf("identical header reported %q", got)
	}
	if got := HeaderDifference(expected, asRow(append(expected, "Coluna extra")...)); got != "" {
		t.Fatalf("a column added after ours reported %q", got)
	}
	renamed := append([]string{}, expected...)
	renamed[2] = "Preço"
	if got := HeaderDifference(expected, asRow(renamed...)); !strings.Contains(got, "Valor") || !strings.Contains(got, "Preço") {
		t.Fatalf("rename reported %q", got)
	}
	if got := HeaderDifference(expected, asRow(expected[:3]...)); got == "" {
		t.Fatal("a removed column was accepted")
	}
	if got := HeaderDifference(expected, nil); got == "" {
		t.Fatal("an empty first row was accepted")
	}
}

func TestCheckMapping(t *testing.T) {
	s, _ := NewSheet(validInput())
	if err := s.CheckMapping(map[string]string{"nome_do_cliente": "{{var.nome}}", "valor": ""}); err != nil {
		t.Fatalf("a valid mapping was refused: %v", err)
	}
	cases := map[string]struct {
		mapping map[string]string
		err     error
	}{
		"required left blank":  {map[string]string{"nome_do_cliente": "  "}, ErrRequiredColumnMissing},
		"required not mapped":  {map[string]string{"valor": "1"}, ErrRequiredColumnMissing},
		"unknown column":       {map[string]string{"nome_do_cliente": "a", "cpf": "1"}, ErrUnknownColumn},
		"system column mapped": {map[string]string{"nome_do_cliente": "a", "telefone": "1"}, ErrSystemColumn},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if err := s.CheckMapping(c.mapping); !errors.Is(err, c.err) {
				t.Fatalf("err = %v, want %v", err, c.err)
			}
		})
	}
}

func TestCheckDefinitionValidatesTheShapeWithoutAnAccount(t *testing.T) {
	in := validInput()
	in.WorkspaceID, in.AccountID = "", ""
	if err := CheckDefinition(in); err != nil {
		t.Fatalf("a valid draft was refused: %v", err)
	}
	in.Columns[2].Options = nil
	if err := CheckDefinition(in); !errors.Is(err, ErrInvalidSheet) {
		t.Fatalf("err = %v", err)
	}
	if _, err := NewSheet(NewSheetInput{Title: "x", Columns: validInput().Columns}); !errors.Is(err, ErrInvalidSheet) {
		t.Fatal("NewSheet accepted a sheet without workspace or account")
	}
}
