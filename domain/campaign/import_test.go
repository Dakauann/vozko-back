package campaign

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"vozko/domain/sheet"
)

func digitsOnly(raw string) string {
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, raw)
	if len(digits) < 8 {
		return ""
	}
	return digits
}

func sheetOf(csv string) []sheet.Row { return sheet.Parse([]byte(csv)) }

func TestReadImportReportsEveryProblemWithItsLine(t *testing.T) {
	rows := sheetOf("numero;nome;var1\n5584994409624;Maria;BF10\n123;Ana;BF10\n+55 84 99440-9624;Maria;BF10\n5584994409625;Pedro;\n")
	got, err := ReadImport(rows, ColumnMapping{}, 1, digitsOnly)
	if err != nil {
		t.Fatal(err)
	}
	if got.TotalRows != 4 || got.ValidRows != 1 || got.Rows[0].Number != "5584994409624" || got.Rows[0].Name != "Maria" {
		t.Fatalf("result = %+v", got)
	}
	want := map[int]string{3: IssueInvalidNumber, 4: IssueDuplicate, 5: IssueMissingVariable}
	for _, issue := range got.Issues {
		if want[issue.Line] != issue.Reason {
			t.Fatalf("issues = %+v", got.Issues)
		}
	}
}

func TestReadImportRefusesAnUnusableSheetOrMapping(t *testing.T) {
	if _, err := ReadImport(sheetOf("numero\n"), ColumnMapping{}, 0, digitsOnly); !errors.Is(err, ErrImportEmpty) {
		t.Fatalf("empty = %v", err)
	}
	if _, err := ReadImport(sheetOf("cliente\nMaria\n"), ColumnMapping{}, 0, digitsOnly); !errors.Is(err, ErrImportNumberColumn) {
		t.Fatalf("no number column = %v", err)
	}
	if _, err := ReadImport(sheetOf("fone,cupom\n5584994409624,X\n"), ColumnMapping{Number: "fone", Variables: []string{"nope"}}, 1, digitsOnly); !errors.Is(err, ErrImportVariableCount) {
		t.Fatalf("bad mapping = %v", err)
	}
}

func TestDefaultMappingDetectsCommonHeaders(t *testing.T) {
	got := DefaultMapping([]string{"Nome", " Telefone ", "VAR2", "var1", "cidade"}, 2)
	want := ColumnMapping{Number: " Telefone ", Name: "Nome", Variables: []string{"var1", "VAR2"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mapping = %+v", got)
	}
}
