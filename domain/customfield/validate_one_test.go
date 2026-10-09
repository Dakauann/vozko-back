package customfield

import (
	"errors"
	"testing"
)

func TestValidateOne(t *testing.T) {
	defs := []*Definition{
		{Key: "classificacao", ObjectType: ObjectLead, Type: TypeSelect, Options: []string{"Positivo"}, Sensitive: true},
		{Key: "origem", ObjectType: ObjectLead, Type: TypeText, Required: true},
	}
	cases := []struct {
		name   string
		key    string
		value  any
		viewer Viewer
		want   error
	}{
		{"a valid option", "classificacao", "Positivo", Viewer{ReadsSensitive: true}, nil},
		{"a sensitive field for a viewer who cannot read it", "classificacao", "Positivo", Viewer{}, ErrValueForbidden},
		{"an option that does not exist", "classificacao", "Talvez", Viewer{ReadsSensitive: true}, ErrValueNotInOptions},
		{"an unknown key", "cor", "azul", Viewer{}, ErrUnknownKey},
		{"clearing a required field", "origem", nil, Viewer{}, ErrValueRequired},
		{"only the named field is checked", "classificacao", nil, Viewer{ReadsSensitive: true}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			def, err := ValidateOne(defs, tc.viewer, tc.key, tc.value)
			if !errors.Is(err, tc.want) {
				t.Fatalf("ValidateOne = %v, want %v", err, tc.want)
			}
			if tc.want == nil && (def == nil || def.Key != tc.key) {
				t.Fatalf("ValidateOne returned %+v", def)
			}
		})
	}
}
