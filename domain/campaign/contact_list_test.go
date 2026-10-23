package campaign

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"vozko/domain/customfield"
	"vozko/domain/sheet"
)

func contactRows(t *testing.T, text string) []sheet.Row {
	t.Helper()
	return sheet.Parse([]byte(text))
}

func readPhones(t *testing.T, text string) ContactList {
	t.Helper()
	list, err := ReadContactList(contactRows(t, text), ContactPhone)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func freeVariables(n int) []BindingSource {
	return make([]BindingSource, n)
}

func TestReadContactListReadsTheHeadersOfEverySpreadsheetWeAccept(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{name: "semicolon", text: "telefone;nome;var1\n(11) 90001-0142;Maria;Maria\n"},
		{name: "comma", text: "number,name,var1\n5511900010142,Maria,Maria\n"},
		{name: "tab", text: "phone\tname\tvar1\n11 90001 0142\tMaria\tMaria\n"},
		{name: "byte order mark", text: "\xEF\xBB\xBFnumero;nome;var1\n+55 11 90001-0142;Maria;Maria\n"},
		{name: "accents and case", text: "Número;Nome;VAR1\n11900010142;Maria;Maria\n"},
		{name: "the unofficial sample header", text: "numero;nome;variavel1\n5511900010142;Maria;Maria\n"},
		{name: "spelled variable", text: "celular;nome;variável 1\n5511900010142;Maria;Maria\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			list := readPhones(t, tc.text)
			if !list.Layout.Header || list.Lines != 1 || len(list.Issues) != 0 || len(list.Rows) != 1 {
				t.Fatalf("list = %+v, want one data line under a header and nothing skipped", list)
			}
			row := list.Rows[0]
			if row.Line != 2 || row.Contact != "5511900010142" || row.Name != "Maria" {
				t.Fatalf("row = %+v", row)
			}
			mapping := MapList(list.Layout, ContactPhone, freeVariables(1), nil)
			if got := mapping.Cells(row); !reflect.DeepEqual(got, []string{"Maria"}) {
				t.Fatalf("variables = %q", got)
			}
			if got := mapping.Metadata(row); got != nil {
				t.Fatalf("metadata = %v", got)
			}
		})
	}
}

func TestReadContactListReadsAListWithoutHeaderByPosition(t *testing.T) {
	list := readPhones(t, "5511900010142;Maria Aparecida;Maria;Centro\n5511900023301;João;João\n")
	if list.Layout.Header || list.Lines != 2 || len(list.Rows) != 2 {
		t.Fatalf("list = %+v, want two positional rows", list)
	}
	if list.Rows[0].Line != 1 || list.Rows[0].Contact != "5511900010142" || list.Rows[0].Name != "Maria Aparecida" {
		t.Fatalf("first row = %+v", list.Rows[0])
	}
	if list.Layout.VariableColumns() != 2 {
		t.Fatalf("variable columns = %d", list.Layout.VariableColumns())
	}
	mapping := MapList(list.Layout, ContactPhone, freeVariables(2), nil)
	if got := mapping.Cells(list.Rows[1]); !reflect.DeepEqual(got, []string{"João", ""}) {
		t.Fatalf("second row variables = %q", got)
	}
	if mapping.Variables[1].Column != "var2" || mapping.Variables[1].Fill != FillColumn {
		t.Fatalf("mapping = %+v", mapping.Variables)
	}
}

func TestReadContactListSkipsBadLinesWithTheirReason(t *testing.T) {
	list := readPhones(t, "telefone;nome\n5511900010142;Maria\n1199;Bruna\n;Marta\nabc;Rita\n+55 (11) 90001-0142;Maria de novo\n5511900023301;João\n")
	if list.Lines != 6 || len(list.Rows) != 2 {
		t.Fatalf("list = %+v, want six data lines and two valid rows", list)
	}
	want := []ListIssue{
		{Line: 3, Reason: ListInvalidNumber, Raw: "1199;Bruna"},
		{Line: 4, Reason: ListMissingNumber, Raw: ";Marta"},
		{Line: 5, Reason: ListMissingNumber, Raw: "abc;Rita"},
		{Line: 6, Reason: ListDuplicate, Raw: "+55 (11) 90001-0142;Maria de novo", DuplicateOf: 2},
	}
	if !reflect.DeepEqual(list.Issues, want) {
		t.Fatalf("issues = %+v\nwant %+v", list.Issues, want)
	}
	if list.IssueCounts[ListInvalidNumber] != 1 || list.IssueCounts[ListMissingNumber] != 2 || list.IssueCounts[ListDuplicate] != 1 {
		t.Fatalf("counts = %+v", list.IssueCounts)
	}
}

func TestMapListWithoutSuggestionsKeepsTodaysVarColumnsAndMetadata(t *testing.T) {
	list := readPhones(t, "telefone;nome;var2;var1;Série de interesse;Bolsista;Idade;Unidade;Bairro\n5511900010142;Maria;Centro;Maria;1º ano;TRUE;7;;Centro\n")
	mapping := MapList(list.Layout, ContactPhone, freeVariables(2), nil)
	row := list.Rows[0]
	if got := mapping.Cells(row); !reflect.DeepEqual(got, []string{"Maria", "Centro"}) {
		t.Fatalf("variables = %v, want them ordered by number", got)
	}
	wantMeta := map[string]any{"serie_de_interesse": "1º ano", "bolsista": true, "idade": float64(7), "bairro": "Centro"}
	if got := mapping.Metadata(row); !reflect.DeepEqual(got, wantMeta) {
		t.Fatalf("metadata = %#v, want %#v", got, wantMeta)
	}
	wantColumns := []MetadataColumn{{Key: "serie_de_interesse", Label: "Série de interesse"}, {Key: "bolsista", Label: "Bolsista"}, {Key: "idade", Label: "Idade"}, {Key: "unidade", Label: "Unidade"}, {Key: "bairro", Label: "Bairro"}}
	if !reflect.DeepEqual(mapping.MetadataColumns, wantColumns) {
		t.Fatalf("metadata columns = %+v", mapping.MetadataColumns)
	}
	wantVariables := []VariableColumnMapping{{Slot: 1, Column: "var1", Fill: FillColumn}, {Slot: 2, Column: "var2", Fill: FillColumn}}
	if !reflect.DeepEqual(mapping.Variables, wantVariables) {
		t.Fatalf("variables mapping = %+v", mapping.Variables)
	}
	if list.Layout.Contact != 0 || list.Layout.ContactLabel() != "telefone" || list.Layout.NameLabel() != "nome" {
		t.Fatalf("layout = %+v", list.Layout)
	}
}

func TestMapListLeavesAGapForAMissingVariableColumn(t *testing.T) {
	list := readPhones(t, "telefone;var1;var3\n5511900010142;a;c\n")
	mapping := MapList(list.Layout, ContactPhone, freeVariables(3), nil)
	if got := mapping.Cells(list.Rows[0]); !reflect.DeepEqual(got, []string{"a", "", "c"}) {
		t.Fatalf("variables = %q, want var2 empty rather than var3 moved into its place", got)
	}
	if mapping.Variables[1].Fill != FillNone || mapping.Variables[1].Column != "" {
		t.Fatalf("var2 mapping = %+v", mapping.Variables[1])
	}
}

func TestMapListNamesSuggestedVariablesAfterTheirLeadField(t *testing.T) {
	defs := []*customfield.Definition{{Key: "serie_interesse", Label: "Série de interesse", ObjectType: customfield.ObjectLead, Type: customfield.TypeText}}
	list := readPhones(t, "telefone;nome;primeiro_nome;Bairro;var3;Série de interesse;Turma\n5511900010142;Maria Souza;Mari;Centro;manhã;1º ano;A\n")
	fields := []BindingSource{BindFirstName, BindDistrict, "", CustomBinding("serie_interesse")}
	mapping := MapList(list.Layout, ContactPhone, fields, defs)
	want := []VariableColumnMapping{
		{Slot: 1, Column: "primeiro_nome", Field: BindFirstName, Fill: FillColumn},
		{Slot: 2, Column: "Bairro", Field: BindDistrict, Fill: FillColumn},
		{Slot: 3, Column: "var3", Fill: FillColumn},
		{Slot: 4, Column: "Série de interesse", Field: CustomBinding("serie_interesse"), Fill: FillColumn},
	}
	if !reflect.DeepEqual(mapping.Variables, want) {
		t.Fatalf("mapping = %+v\nwant %+v", mapping.Variables, want)
	}
	if got := mapping.Cells(list.Rows[0]); !reflect.DeepEqual(got, []string{"Mari", "Centro", "manhã", "1º ano"}) {
		t.Fatalf("variables = %q", got)
	}
	if got := mapping.Metadata(list.Rows[0]); !reflect.DeepEqual(got, map[string]any{"turma": "A"}) {
		t.Fatalf("a column that fills a variable is not metadata: %v", got)
	}
}

func TestMapListStillAcceptsVarNForASuggestedVariable(t *testing.T) {
	list := readPhones(t, "telefone;var1\n5511900010142;Mari\n")
	mapping := MapList(list.Layout, ContactPhone, []BindingSource{BindFirstName}, nil)
	if mapping.Variables[0].Column != "var1" || mapping.Variables[0].Fill != FillColumn {
		t.Fatalf("mapping = %+v", mapping.Variables)
	}
	if got := mapping.Cells(list.Rows[0]); !reflect.DeepEqual(got, []string{"Mari"}) {
		t.Fatalf("variables = %q", got)
	}
}

func TestMapListSaysHowAVariableWithoutColumnIsFilled(t *testing.T) {
	list := readPhones(t, "telefone;nome\n5511900010142;Maria\n")
	fields := []BindingSource{BindFirstName, BindCity, BindLiteral, ""}
	mapping := MapList(list.Layout, ContactPhone, fields, nil)
	fills := []FillKind{}
	for _, v := range mapping.Variables {
		fills = append(fills, v.Fill)
	}
	if !reflect.DeepEqual(fills, []FillKind{FillName, FillLead, FillFixed, FillNone}) {
		t.Fatalf("fills = %v", fills)
	}
	if got := mapping.Cells(list.Rows[0]); !reflect.DeepEqual(got, []string{"", "", "", ""}) {
		t.Fatalf("cells = %q, filling from the lead happens when the lead is known", got)
	}
	headerless := readPhones(t, "5511900010142\n")
	if got := MapList(headerless.Layout, ContactPhone, []BindingSource{BindFirstName}, nil).Variables[0].Fill; got != FillLead {
		t.Fatalf("without a name column the first name comes from the lead: %s", got)
	}
}

func TestMapListUsesTheRowsOwnValuesOnceTheyWereTypedByHand(t *testing.T) {
	list := readPhones(t, "telefone;var1\n5511900010142;Mari\n")
	mapping := MapList(list.Layout, ContactPhone, freeVariables(2), nil)
	typed := ListRow{Contact: "5511900010199", Variables: []string{"Ana"}, Metadata: map[string]any{"turma": "B"}}
	if got := mapping.Cells(typed); !reflect.DeepEqual(got, []string{"Ana", ""}) {
		t.Fatalf("typed variables = %q", got)
	}
	if got := mapping.Metadata(typed); !reflect.DeepEqual(got, map[string]any{"turma": "B"}) {
		t.Fatalf("typed metadata = %v", got)
	}
}

func TestReadContactListReadsEmailLists(t *testing.T) {
	list, err := ReadContactList(contactRows(t, "Nome;E-mail;Cidade\nMaria;Maria@Exemplo.com.br;Barueri\nAna;não é email;Centro\nBia;;Centro\nOutra Maria;maria@exemplo.com.br;Osasco\n"), ContactEmail)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Rows) != 1 || list.Rows[0].Contact != "maria@exemplo.com.br" || list.Rows[0].Name != "Maria" {
		t.Fatalf("rows = %+v, want one normalized address", list.Rows)
	}
	mapping := MapList(list.Layout, ContactEmail, nil, nil)
	if mapping.Metadata(list.Rows[0]) != nil || len(mapping.Variables) != 0 {
		t.Fatalf("an email list carries no metadata: %+v", mapping)
	}
	reasons := []ListIssueReason{}
	for _, issue := range list.Issues {
		reasons = append(reasons, issue.Reason)
	}
	if !reflect.DeepEqual(reasons, []ListIssueReason{ListInvalidEmail, ListMissingEmail, ListDuplicate}) || list.Issues[2].DuplicateOf != 2 {
		t.Fatalf("issues = %+v", list.Issues)
	}
}

func TestReadContactListRefusesWhatItCannotRead(t *testing.T) {
	if _, err := ReadContactList(nil, ContactPhone); !errors.Is(err, ErrListEmpty) {
		t.Fatalf("empty = %v", err)
	}
	if _, err := ReadContactList(contactRows(t, "telefone;nome\n"), ContactPhone); !errors.Is(err, ErrListEmpty) {
		t.Fatalf("header only = %v", err)
	}
	if _, err := ReadContactList(contactRows(t, "nome;cidade\nMaria;Barueri\n"), ContactPhone); !errors.Is(err, ErrListContactColumn) {
		t.Fatalf("no phone column = %v", err)
	}
	if _, err := ReadContactList(contactRows(t, "bairro;primeiro_nome\nCentro;Maria\n"), ContactPhone); !errors.Is(err, ErrListContactColumn) {
		t.Fatalf("field columns make a header too: %v", err)
	}
	if _, err := ReadContactList(contactRows(t, "nome;telefone\nMaria;5511900010142\n"), ContactEmail); !errors.Is(err, ErrListContactColumn) {
		t.Fatalf("no email column = %v", err)
	}
	wide := strings.Repeat("c;", MaxListColumns) + "telefone\n"
	if _, err := ReadContactList(contactRows(t, wide+"1\n"), ContactPhone); !errors.Is(err, ErrListTooWide) {
		t.Fatalf("wide = %v", err)
	}
	if _, err := ReadContactList(contactRows(t, "telefone\n1\n"), ContactKind("fax")); !errors.Is(err, ErrListContactKind) {
		t.Fatalf("kind = %v", err)
	}
}

func TestReadContactListRefusesMoreRowsThanOneCampaignTakes(t *testing.T) {
	rows := make([]sheet.Row, 0, MaxEntries+2)
	rows = append(rows, sheet.Row{Line: 1, Cells: []string{"telefone"}})
	for i := 0; i <= MaxEntries; i++ {
		rows = append(rows, sheet.Row{Line: i + 2, Cells: []string{fmt.Sprintf("55119%08d", i)}})
	}
	if _, err := ReadContactList(rows, ContactPhone); !errors.Is(err, ErrListTooLarge) {
		t.Fatalf("too large = %v", err)
	}
}

func TestReadContactListKeepsOnlyTheFirstSkippedLinesButCountsThemAll(t *testing.T) {
	var b strings.Builder
	b.WriteString("telefone\n")
	for i := 0; i < MaxImportIssues+10; i++ {
		b.WriteString("123\n")
	}
	list := readPhones(t, b.String())
	if len(list.Issues) != MaxImportIssues || list.IssueCounts[ListInvalidNumber] != MaxImportIssues+10 || list.SkippedTotal() != MaxImportIssues+10 {
		t.Fatalf("issues %d counts %+v", len(list.Issues), list.IssueCounts)
	}
}

func TestFieldColumnIsTheStableSampleHeaderOfALeadField(t *testing.T) {
	defs := []*customfield.Definition{{Key: "serie_interesse", Label: "Série de interesse", ObjectType: customfield.ObjectLead}}
	cases := map[BindingSource]string{
		BindFirstName: "primeiro_nome", BindName: "nome_completo", BindNickname: "apelido", BindDistrict: "bairro",
		BindCity: "cidade", BindOwnerName: "responsavel", CustomBinding("serie_interesse"): "serie_interesse",
		BindLiteral: "", "": "", CustomBinding("sumiu"): "",
	}
	for source, want := range cases {
		if got := FieldColumn(source, defs); got != want {
			t.Fatalf("FieldColumn(%q) = %q, want %q", source, got, want)
		}
	}
	for _, source := range BindableSources() {
		if FieldColumn(source, nil) == "" {
			t.Fatalf("%s has no sample column", source)
		}
	}
}

func TestMetadataKeyFoldsLabelsLikeTheFormDid(t *testing.T) {
	cases := map[string]string{
		"Série de interesse": "serie_de_interesse",
		"  CPF  ":            "cpf",
		"Ação/Promoção 2027": "acao_promocao_2027",
		"___":                "",
		"":                   "",
	}
	for label, want := range cases {
		if got := MetadataKey(label); got != want {
			t.Fatalf("MetadataKey(%q) = %q, want %q", label, got, want)
		}
	}
}

func TestMetadataValueTypesBooleansAndNumbers(t *testing.T) {
	cases := []struct {
		raw  string
		want any
	}{
		{"true", true}, {"FALSE", false}, {"42", float64(42)}, {"-3.5", -3.5}, {"1e3", float64(1000)},
		{"0123", float64(123)}, {"1º ano", "1º ano"}, {"NaN", "NaN"}, {"Infinity", "Infinity"}, {" sim ", "sim"},
	}
	for _, tc := range cases {
		if got := MetadataValue(tc.raw); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("MetadataValue(%q) = %#v, want %#v", tc.raw, got, tc.want)
		}
	}
}

func TestVariableColumnNamesEveryWayASheetSpellsIt(t *testing.T) {
	cases := map[string]int{"var1": 1, "VAR12": 12, "variavel3": 3, "Variável 2": 2, "variable 4": 4, "var0": 0, "vars": 0, "var": 0, "nome": 0}
	for header, want := range cases {
		got, ok := VariableColumn(header)
		if (want > 0) != ok || got != want {
			t.Fatalf("VariableColumn(%q) = %d, %v; want %d", header, got, ok, want)
		}
	}
}

func TestMissingVariableSlotsNamesTheEmptyRequiredValues(t *testing.T) {
	if got := MissingSlots([]string{"a", " ", "c", ""}); !reflect.DeepEqual(got, []int{2, 4}) {
		t.Fatalf("missing = %v", got)
	}
	if got := MissingSlots(nil); got != nil {
		t.Fatalf("missing without variables = %v", got)
	}
}
