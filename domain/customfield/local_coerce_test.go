package customfield

import (
	"errors"
	"reflect"
	"testing"
)

func TestCoerceLocalReadsWhatPeopleTypeInBrazil(t *testing.T) {
	cases := []struct {
		name string
		def  *Definition
		raw  string
		want any
	}{
		{"decimal comma", &Definition{Key: "k", Type: TypeNumber}, "8,5", 8.5},
		{"thousands dot and decimal comma", &Definition{Key: "k", Type: TypeNumber}, "1.234,5", 1234.5},
		{"decimal point", &Definition{Key: "k", Type: TypeNumber}, " 12.5 ", 12.5},
		{"day first date", &Definition{Key: "k", Type: TypeDate}, "05/02/2020", "2020-02-05"},
		{"iso date", &Definition{Key: "k", Type: TypeDate}, "2020-02-05", "2020-02-05"},
		{"sim", &Definition{Key: "k", Type: TypeBoolean}, "Sim", true},
		{"não", &Definition{Key: "k", Type: TypeBoolean}, "NÃO", false},
		{"true", &Definition{Key: "k", Type: TypeBoolean}, "true", true},
		{"a marked box", &Definition{Key: "k", Type: TypeBoolean}, "x", true},
		{"select option", &Definition{Key: "k", Type: TypeSelect, Options: []string{"Matrícula", "Visita"}}, " Matrícula ", "Matrícula"},
		{"text", &Definition{Key: "k", Type: TypeText}, "alto", "alto"},
		{"multiselect", &Definition{Key: "k", Type: TypeMultiSelect, Options: []string{"a", "b"}}, "a|b", []string{"a", "b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.def.CoerceLocal(tc.raw)
			if err != nil {
				t.Fatalf("CoerceLocal(%q) error = %v", tc.raw, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("CoerceLocal(%q) = %#v, want %#v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestCoerceLocalRefusesWhatItCannotType(t *testing.T) {
	cases := []struct {
		name string
		def  *Definition
		raw  string
		want error
	}{
		{"not a number", &Definition{Key: "k", Type: TypeNumber}, "muito boa", ErrValueType},
		{"NaN", &Definition{Key: "k", Type: TypeNumber}, "NaN", ErrValueType},
		{"infinity", &Definition{Key: "k", Type: TypeNumber}, "Infinity", ErrValueType},
		{"short infinity", &Definition{Key: "k", Type: TypeNumber}, "-inf", ErrValueType},
		{"not a date", &Definition{Key: "k", Type: TypeDate}, "ontem", ErrValueType},
		{"not a yes or no", &Definition{Key: "k", Type: TypeBoolean}, "talvez", ErrValueType},
		{"option the field does not offer", &Definition{Key: "k", Type: TypeSelect, Options: []string{"Visita"}}, "Outro", ErrValueNotInOptions},
		{"no definition", nil, "x", ErrUnknownKey},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.def.CoerceLocal(tc.raw); !errors.Is(err, tc.want) {
				t.Fatalf("CoerceLocal(%q) error = %v, want %v", tc.raw, err, tc.want)
			}
		})
	}
}

func TestMissingRequiredListsTheRequiredFieldsAViewerLeftEmpty(t *testing.T) {
	defs := []*Definition{
		{Key: "turma", Type: TypeText, Required: true},
		{Key: "renda", Type: TypeNumber, Required: true, Sensitive: true, LegalBasis: "consentimento"},
		{Key: "interesse", Type: TypeText, Required: true},
		{Key: "cor", Type: TypeText},
		nil,
	}
	cases := []struct {
		name   string
		viewer Viewer
		values map[string]any
		want   []string
	}{
		{"nothing filled", Viewer{}, nil, []string{"interesse", "turma"}},
		{"a blank value is missing", Viewer{}, map[string]any{"interesse": "", "turma": "3A"}, []string{"interesse"}},
		{"sensitive fields count for a viewer who reads them", Viewer{ReadsSensitive: true}, map[string]any{"interesse": "x"}, []string{"renda", "turma"}},
		{"everything filled", Viewer{ReadsSensitive: true}, map[string]any{"interesse": "x", "turma": "3A", "renda": 10.0}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MissingRequired(defs, tc.viewer, tc.values); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("MissingRequired = %v, want %v", got, tc.want)
			}
		})
	}
}
