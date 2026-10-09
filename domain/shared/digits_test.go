package shared

import "testing"

func TestDigitsOfKeepsOnlyTheDigits(t *testing.T) {
	cases := map[string]string{"(84) 99999-1234": "84999991234", "+55 11": "5511", "abc": "", "": ""}
	for raw, want := range cases {
		if got := DigitsOf(raw); got != want {
			t.Errorf("DigitsOf(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestAllDigitsNeedsAtLeastOneDigit(t *testing.T) {
	cases := map[string]bool{"1234": true, "12a4": false, "": false, "+12": false}
	for raw, want := range cases {
		if got := AllDigits(raw); got != want {
			t.Errorf("AllDigits(%q) = %v, want %v", raw, got, want)
		}
	}
}
