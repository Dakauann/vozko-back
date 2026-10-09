package customfield

import (
	"encoding/json"
	"errors"
	"math"
	"testing"
)

func TestANumberValueIsAFiniteDecimalTheStoreCanRead(t *testing.T) {
	tests := []struct {
		name  string
		value any
		ok    bool
	}{
		{"float", 12.5, true},
		{"integer", 7, true},
		{"json number", json.Number("42"), true},
		{"decimal text", "12.5", true},
		{"padded negative text", " -3 ", true},
		{"exponent text", "1e3", true},
		{"not a number", "abc", false},
		{"nan text", "NaN", false},
		{"infinity text", "Infinity", false},
		{"short infinity text", "-inf", false},
		{"decimal comma text", "1,5", false},
		{"hexadecimal text", "0x1p4", false},
		{"nan float", math.NaN(), false},
		{"infinite float", math.Inf(1), false},
		{"infinite float32", float32(math.Inf(-1)), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := def(TypeNumber).ValidateValue(tc.value)
			if tc.ok && err != nil {
				t.Fatalf("ValidateValue(%#v) = %v, want accepted", tc.value, err)
			}
			if !tc.ok && !errors.Is(err, ErrValueType) {
				t.Fatalf("ValidateValue(%#v) = %v, want ErrValueType", tc.value, err)
			}
		})
	}
}
