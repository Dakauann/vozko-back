package lead

import (
	"reflect"
	"testing"
)

func TestEmailFactsNames(t *testing.T) {
	cases := []struct {
		name, first, full string
	}{
		{name: "Ana Maria Souza", first: "Ana", full: "Ana Maria Souza"},
		{name: "  ana  ", first: "ana", full: "ana"},
		{name: "", first: "", full: ""},
	}
	for _, tc := range cases {
		f := EmailFacts{Name: tc.name}
		if f.FirstName() != tc.first || f.FullName() != tc.full {
			t.Errorf("%q: first=%q full=%q", tc.name, f.FirstName(), f.FullName())
		}
	}
}

func TestEmailFactsFieldValuesKeepPrintableScalars(t *testing.T) {
	f := EmailFacts{CustomFields: map[string]any{
		"plano": "Ouro", "pontos": float64(120), "vip": true, "vazio": " ", "lista": []any{"a"}, "nada": nil,
	}}
	want := map[string]string{"plano": "Ouro", "pontos": "120", "vip": "true"}
	if got := f.FieldValues(); !reflect.DeepEqual(got, want) {
		t.Fatalf("FieldValues() = %v, want %v", got, want)
	}
}
