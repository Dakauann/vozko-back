package customfield

import (
	"errors"
	"reflect"
	"testing"
)

func leadDefs() []*Definition {
	return []*Definition{
		{Key: "interesse", Type: TypeSelect, Options: []string{"alto", "baixo"}, Required: true},
		{Key: "classificacao", Type: TypeSelect, Options: []string{"positivo", "negativo"}, Sensitive: true, LegalBasis: "consentimento"},
		{Key: "notas", Type: TypeText},
	}
}

var (
	reader      = Viewer{ReadsSensitive: true}
	plainViewer = Viewer{}
)

func TestApplyValues(t *testing.T) {
	stored := map[string]any{"interesse": "alto", "classificacao": "positivo", "removida": "x"}
	cases := []struct {
		name    string
		viewer  Viewer
		current map[string]any
		patch   map[string]any
		want    map[string]any
		err     error
		key     string
	}{
		{"a patch keeps every key it does not name", plainViewer, stored, map[string]any{"notas": "ligar de manhã"},
			map[string]any{"interesse": "alto", "classificacao": "positivo", "removida": "x", "notas": "ligar de manhã"}, nil, ""},
		{"a null value clears the key", reader, stored, map[string]any{"classificacao": nil},
			map[string]any{"interesse": "alto", "removida": "x"}, nil, ""},
		{"a sensitive key needs permission to read it", plainViewer, stored, map[string]any{"classificacao": "negativo"},
			nil, ErrValueForbidden, "classificacao"},
		{"clearing a sensitive key needs the same permission", plainViewer, stored, map[string]any{"classificacao": nil},
			nil, ErrValueForbidden, "classificacao"},
		{"a reader of sensitive data writes it", reader, stored, map[string]any{"classificacao": "negativo"},
			map[string]any{"interesse": "alto", "classificacao": "negativo", "removida": "x"}, nil, ""},
		{"an unknown key is refused", reader, stored, map[string]any{"cpf": "123"}, nil, ErrUnknownKey, "cpf"},
		{"a stored key whose field was removed cannot be written", reader, stored, map[string]any{"removida": "y"}, nil, ErrUnknownKey, "removida"},
		{"a value of the wrong type is refused", reader, stored, map[string]any{"notas": 3}, nil, ErrValueType, "notas"},
		{"an option the field does not have is refused", reader, stored, map[string]any{"interesse": "medio"}, nil, ErrValueNotInOptions, "interesse"},
		{"a required field cannot be cleared", reader, stored, map[string]any{"interesse": nil}, nil, ErrValueRequired, "interesse"},
		{"a required field is asked on a new record", plainViewer, nil, map[string]any{"notas": "oi"}, nil, ErrValueRequired, "interesse"},
		{"a new record with no values still needs the required field", plainViewer, nil, nil, nil, ErrValueRequired, "interesse"},
		{"an empty result is stored as nothing", reader, map[string]any{"notas": "x", "interesse": "alto"}, map[string]any{"notas": nil},
			map[string]any{"interesse": "alto"}, nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ApplyValues(leadDefs(), tc.viewer, tc.current, tc.patch)
			if tc.err != nil {
				var valueErr *ValueError
				if !errors.Is(err, tc.err) || !errors.As(err, &valueErr) || valueErr.Key != tc.key {
					t.Fatalf("err = %v, want %v on key %q", err, tc.err, tc.key)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("values = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestApplyValuesNeverChangesTheStoredMap(t *testing.T) {
	stored := map[string]any{"interesse": "alto"}
	if _, err := ApplyValues(leadDefs(), reader, stored, map[string]any{"interesse": "baixo"}); err != nil {
		t.Fatal(err)
	}
	if stored["interesse"] != "alto" {
		t.Fatalf("the stored values changed: %v", stored)
	}
}

func TestApplyValuesLeavesNothingWhenEverythingIsCleared(t *testing.T) {
	defs := []*Definition{{Key: "notas", Type: TypeText}}
	got, err := ApplyValues(defs, reader, map[string]any{"notas": "x"}, map[string]any{"notas": nil})
	if err != nil || got != nil {
		t.Fatalf("values = %v, %v; want nil", got, err)
	}
}

func TestVisibleValues(t *testing.T) {
	stored := map[string]any{"interesse": "alto", "classificacao": "positivo", "removida": "x"}
	cases := []struct {
		name   string
		viewer Viewer
		want   map[string]any
	}{
		{"sensitive keys and keys without a field are hidden", plainViewer, map[string]any{"interesse": "alto"}},
		{"a reader of sensitive data sees them", reader, map[string]any{"interesse": "alto", "classificacao": "positivo"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := VisibleValues(leadDefs(), tc.viewer, stored); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("visible = %v, want %v", got, tc.want)
			}
		})
	}
	if got := VisibleValues(leadDefs(), reader, nil); got != nil {
		t.Fatalf("no values stay nil, got %v", got)
	}
}

func TestRecordableKnowsOnlyNonSensitiveFields(t *testing.T) {
	cases := []struct {
		key  string
		want bool
	}{
		{"interesse", true},
		{"classificacao", false},
		{"removida", false},
	}
	for _, tc := range cases {
		if got := Recordable(leadDefs(), tc.key); got != tc.want {
			t.Errorf("Recordable(%q) = %v, want %v", tc.key, got, tc.want)
		}
	}
}

func TestValidateValuesNamesTheKeyThatFailed(t *testing.T) {
	err := ValidateValues(leadDefs(), map[string]any{"notas": "x"})
	var valueErr *ValueError
	if !errors.Is(err, ErrValueRequired) || !errors.As(err, &valueErr) || valueErr.Key != "interesse" {
		t.Fatalf("err = %v, want the required key named", err)
	}
}
