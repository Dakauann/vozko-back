package advertising

import (
	"errors"
	"testing"
)

func TestMinorUnitsFollowMetaCurrencyOffsets(t *testing.T) {
	cases := []struct {
		currency string
		minor    int64
		micros   int64
	}{
		{"BRL", 2550, 25_500_000},
		{"USD", 100, 1_000_000},
		{"JPY", 500, 500_000_000},
		{"CLP", 1000, 1_000_000_000},
	}
	for _, c := range cases {
		got, err := MinorToMicros(c.currency, c.minor)
		if err != nil || got != c.micros {
			t.Fatalf("MinorToMicros(%s, %d) = %d, %v; want %d", c.currency, c.minor, got, err, c.micros)
		}
		back, err := MicrosToMinor(c.currency, c.micros)
		if err != nil || back != c.minor {
			t.Fatalf("MicrosToMinor(%s, %d) = %d, %v; want %d", c.currency, c.micros, back, err, c.minor)
		}
	}
}

func TestMicrosToMinorRoundsDownSoBudgetsNeverGrow(t *testing.T) {
	got, err := MicrosToMinor("BRL", 10_009_999)
	if err != nil || got != 1000 {
		t.Fatalf("got %d, %v; want 1000", got, err)
	}
}

func TestUnknownCurrencyIsRefusedInsteadOfGuessed(t *testing.T) {
	for _, code := range []string{"", "BR", "brl1", "XXXX"} {
		if _, err := MinorToMicros(code, 100); !errors.Is(err, ErrUnknownCurrency) {
			t.Fatalf("currency %q accepted: %v", code, err)
		}
	}
}

func TestDecimalAmountsFromMetaParseExactly(t *testing.T) {
	cases := map[string]int64{
		"0":         0,
		"12.34":     12_340_000,
		"1000":      1_000_000_000,
		"0.000001":  1,
		"7.5":       7_500_000,
		"355.44":    355_440_000,
		"0.0000019": 1,
	}
	for raw, want := range cases {
		got, err := DecimalToMicros(raw)
		if err != nil || got != want {
			t.Fatalf("DecimalToMicros(%q) = %d, %v; want %d", raw, got, err, want)
		}
	}
	for _, bad := range []string{"", "abc", "1.2.3", "-1", "1e5"} {
		if _, err := DecimalToMicros(bad); err == nil {
			t.Fatalf("DecimalToMicros(%q) accepted", bad)
		}
	}
}

func TestAmountInCurrencyUnitsBecomesMetaMinorUnits(t *testing.T) {
	cases := []struct {
		currency string
		amount   float64
		minor    int64
	}{{"BRL", 30, 3000}, {"BRL", 25.5, 2550}, {"BRL", 0.1, 10}, {"JPY", 500, 500}}
	for _, c := range cases {
		got, err := AmountToMinor(c.currency, c.amount)
		if err != nil || got != c.minor {
			t.Fatalf("AmountToMinor(%s, %v) = %d, %v", c.currency, c.amount, got, err)
		}
	}
	for _, bad := range []float64{0, -1} {
		if _, err := AmountToMinor("BRL", bad); err == nil {
			t.Fatalf("amount %v accepted", bad)
		}
	}
}

func TestMicrosRenderAsPlainCurrencyUnits(t *testing.T) {
	if got := MicrosToAmount(19_746_666); got != 19.75 {
		t.Fatalf("got %v", got)
	}
}
