package shared

import "testing"

func TestParseDecimalReadsBothDecimalMarks(t *testing.T) {
	cases := []struct {
		raw  string
		want float64
	}{
		{"8,5", 8.5},
		{" 12.5 ", 12.5},
		{"1.234,56", 1234.56},
		{"-23,5505", -23.5505},
		{"-46.6333", -46.6333},
		{"7", 7},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			got, err := ParseDecimal(tc.raw)
			if err != nil || got != tc.want {
				t.Fatalf("ParseDecimal(%q) = %v, %v; want %v", tc.raw, got, err, tc.want)
			}
		})
	}
}

func TestParseDecimalRefusesText(t *testing.T) {
	for _, raw := range []string{"", "doze", "1,2,3", "12a", "NaN", "nan", "inf", "+Inf", "-Infinity", "1e999"} {
		if _, err := ParseDecimal(raw); err == nil {
			t.Fatalf("ParseDecimal(%q) was accepted", raw)
		}
	}
}
