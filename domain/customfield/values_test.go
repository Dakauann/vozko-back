package customfield

import (
	"errors"
	"reflect"
	"testing"
)

func opportunityDefs() []*Definition {
	return []*Definition{
		{Key: "segmento", Type: TypeSelect, Options: []string{"enterprise", "smb"}, Required: true},
		{Key: "score", Type: TypeNumber},
		{Key: "tags", Type: TypeMultiSelect, Options: []string{"a", "b"}},
	}
}

func TestValidateValues(t *testing.T) {
	cases := []struct {
		name   string
		defs   []*Definition
		values map[string]any
		want   error
	}{
		{"all valid", opportunityDefs(), map[string]any{"segmento": "smb", "score": float64(3), "tags": []any{"a"}}, nil},
		{"unknown key", opportunityDefs(), map[string]any{"segmento": "smb", "foo": "bar"}, ErrUnknownKey},
		{"wrong type", opportunityDefs(), map[string]any{"segmento": "smb", "score": "abc"}, ErrValueType},
		{"option not allowed", opportunityDefs(), map[string]any{"segmento": "startup"}, ErrValueNotInOptions},
		{"required missing", opportunityDefs(), map[string]any{"score": float64(1)}, ErrValueRequired},
		{"required missing from no values", opportunityDefs(), nil, ErrValueRequired},
		{"no definitions and no values", nil, nil, nil},
		{"no definitions refuse every value", nil, map[string]any{"segmento": "smb"}, ErrUnknownKey},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateValues(tc.defs, tc.values)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("ValidateValues() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("ValidateValues() = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestCoerceTurnsSpreadsheetTextIntoTypedValues(t *testing.T) {
	cases := []struct {
		name string
		def  *Definition
		raw  string
		want any
	}{
		{"text stays text", &Definition{Key: "k", Type: TypeText}, "acme", "acme"},
		{"select stays text", &Definition{Key: "k", Type: TypeSelect, Options: []string{"x"}}, "x", "x"},
		{"date stays text", &Definition{Key: "k", Type: TypeDate}, "2026-07-12", "2026-07-12"},
		{"number", &Definition{Key: "k", Type: TypeNumber}, "12.5", 12.5},
		{"boolean in capitals", &Definition{Key: "k", Type: TypeBoolean}, "TRUE", true},
		{"multiselect split and trimmed", &Definition{Key: "k", Type: TypeMultiSelect}, "a| b ||c ", []string{"a", "b", "c"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.def.Coerce(tc.raw)
			if err != nil {
				t.Fatalf("Coerce(%q) error = %v", tc.raw, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Coerce(%q) = %#v, want %#v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestCoerceRefusesWhatItCannotType(t *testing.T) {
	cases := []struct {
		name string
		def  *Definition
		raw  string
		want error
	}{
		{"not a number", &Definition{Key: "k", Type: TypeNumber}, "doze", ErrValueType},
		{"nan text", &Definition{Key: "k", Type: TypeNumber}, "NaN", ErrValueType},
		{"infinity text", &Definition{Key: "k", Type: TypeNumber}, "Infinity", ErrValueType},
		{"hexadecimal text", &Definition{Key: "k", Type: TypeNumber}, "0x1p4", ErrValueType},
		{"not a boolean", &Definition{Key: "k", Type: TypeBoolean}, "talvez", ErrValueType},
		{"no definition", nil, "x", ErrUnknownKey},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.def.Coerce(tc.raw); !errors.Is(err, tc.want) {
				t.Fatalf("Coerce(%q) error = %v, want %v", tc.raw, err, tc.want)
			}
		})
	}
}

func TestFormatValueWritesSpreadsheetText(t *testing.T) {
	cases := []struct {
		value any
		want  string
	}{
		{nil, ""},
		{"acme", "acme"},
		{true, "true"},
		{12.5, "12.5"},
		{float32(2.5), "2.5"},
		{7, "7"},
		{int64(9), "9"},
		{[]string{"a", "b"}, "a|b"},
		{[]any{"a", 3}, "a|3"},
	}
	for _, tc := range cases {
		if got := FormatValue(tc.value); got != tc.want {
			t.Errorf("FormatValue(%#v) = %q, want %q", tc.value, got, tc.want)
		}
	}
}

func TestCoerceAndFormatRoundTripAMultiselect(t *testing.T) {
	def := &Definition{Key: "tags", Type: TypeMultiSelect}
	value, err := def.Coerce(FormatValue([]string{"a", "b"}))
	if err != nil {
		t.Fatalf("Coerce: %v", err)
	}
	if !reflect.DeepEqual(value, []string{"a", "b"}) {
		t.Fatalf("round trip = %#v", value)
	}
}
