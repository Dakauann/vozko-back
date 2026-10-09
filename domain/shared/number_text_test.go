package shared

import (
	"errors"
	"testing"
)

func TestParseNumberTextReadsDotDecimals(t *testing.T) {
	cases := []struct {
		raw  string
		want float64
	}{
		{"12.5", 12.5},
		{" -3 ", -3},
		{"1e3", 1000},
		{"+7", 7},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			got, err := ParseNumberText(tc.raw)
			if err != nil || got != tc.want {
				t.Fatalf("ParseNumberText(%q) = %v, %v; want %v", tc.raw, got, err, tc.want)
			}
		})
	}
}

func TestParseNumberTextRefusesWhatPostgresNumericCannotRead(t *testing.T) {
	cases := []struct {
		raw  string
		want error
	}{
		{"1,5", ErrNumberTextForm},
		{"1.234,56", ErrNumberTextForm},
		{"0x1p4", ErrNumberTextForm},
		{"0X10", ErrNumberTextForm},
		{"1_000", ErrNumberTextForm},
		{"NaN", ErrDecimalNotFinite},
		{"Infinity", ErrDecimalNotFinite},
		{"-inf", ErrDecimalNotFinite},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			if _, err := ParseNumberText(tc.raw); !errors.Is(err, tc.want) {
				t.Fatalf("ParseNumberText(%q) = %v, want %v", tc.raw, err, tc.want)
			}
		})
	}
	for _, raw := range []string{"", "doze", "1e999"} {
		if _, err := ParseNumberText(raw); err == nil {
			t.Fatalf("ParseNumberText(%q) was accepted", raw)
		}
	}
}
