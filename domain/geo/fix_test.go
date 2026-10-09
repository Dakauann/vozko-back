package geo

import (
	"errors"
	"math"
	"testing"
	"time"
)

func TestPrecisionPinsAHouse(t *testing.T) {
	tests := []struct {
		precision Precision
		pins      bool
	}{
		{PrecisionExact, true},
		{PrecisionAddress, true},
		{PrecisionStreet, true},
		{PrecisionPostalCode, false},
		{PrecisionDistrict, false},
		{PrecisionCity, false},
		{"", false},
		{"rooftop", false},
	}
	for _, tt := range tests {
		t.Run(string(tt.precision), func(t *testing.T) {
			if got := tt.precision.PinsAHouse(); got != tt.pins {
				t.Fatalf("PinsAHouse() = %v, expected %v", got, tt.pins)
			}
		})
	}
}

func TestPrecisionBetter(t *testing.T) {
	tests := []struct {
		name     string
		p, than  Precision
		expected bool
	}{
		{"exact beats address", PrecisionExact, PrecisionAddress, true},
		{"address beats street", PrecisionAddress, PrecisionStreet, true},
		{"street beats postal code", PrecisionStreet, PrecisionPostalCode, true},
		{"postal code beats district", PrecisionPostalCode, PrecisionDistrict, true},
		{"district beats city", PrecisionDistrict, PrecisionCity, true},
		{"city beats nothing known", PrecisionCity, "", true},
		{"equal is not better", PrecisionStreet, PrecisionStreet, false},
		{"worse is not better", PrecisionCity, PrecisionExact, false},
		{"an unknown precision is never better", "rooftop", PrecisionCity, false},
		{"two unknown precisions are not better than each other", "rooftop", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.Better(tt.than); got != tt.expected {
				t.Fatalf("%q.Better(%q) = %v, expected %v", tt.p, tt.than, got, tt.expected)
			}
		})
	}
}

func TestCEPPrecision(t *testing.T) {
	tests := []struct {
		name     string
		zip      string
		spreadM  float64
		expected Precision
	}{
		{"a tight CEP pins the street", "01310100", 120, PrecisionStreet},
		{"just under 300 m is still street", "01310-100", 299.9, PrecisionStreet},
		{"300 m is a postal code", "01310100", 300, PrecisionPostalCode},
		{"1,500 m is still a postal code", "30140071", 1500, PrecisionPostalCode},
		{"above 1,500 m is a city", "30140071", 1500.1, PrecisionCity},
		{"a generic CEP ending in 000 is a city even when tight", "69300000", 80, PrecisionCity},
		{"a generic CEP with a mask is a city", "39.270-000", 900, PrecisionCity},
		{"an unreadable CEP is a city", "abc", 50, PrecisionCity},
		{"a negative spread is a city", "01310100", -1, PrecisionCity},
		{"a NaN spread is a city", "01310100", math.NaN(), PrecisionCity},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CEPPrecision(tt.zip, tt.spreadM); got != tt.expected {
				t.Fatalf("CEPPrecision(%q, %v) = %q, expected %q", tt.zip, tt.spreadM, got, tt.expected)
			}
		})
	}
}

func TestFixValidate(t *testing.T) {
	point := Point{Lat: -23.55, Lng: -46.63}
	tests := []struct {
		name     string
		fix      Fix
		expected error
	}{
		{"accepts a reference fix", Fix{Point: point, Precision: PrecisionStreet, Source: SourceReference}, nil},
		{"accepts a provider fix that names the provider", Fix{Point: point, Precision: PrecisionAddress, Source: SourceProvider, Provider: "opencage"}, nil},
		{"refuses a provider fix without the provider, so attribution is never lost", Fix{Point: point, Precision: PrecisionAddress, Source: SourceProvider}, ErrInvalidFix},
		{"refuses an unknown precision", Fix{Point: point, Precision: "rooftop", Source: SourceReference}, ErrInvalidFix},
		{"refuses an unknown source", Fix{Point: point, Precision: PrecisionCity, Source: "guess"}, ErrInvalidFix},
		{"refuses an invalid point", Fix{Precision: PrecisionCity, Source: SourceReference}, ErrInvalidPoint},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fix.Validate()
			if tt.expected == nil && err != nil {
				t.Fatalf("expected valid, got %v", err)
			}
			if tt.expected != nil && !errors.Is(err, tt.expected) {
				t.Fatalf("expected %v, got %v", tt.expected, err)
			}
		})
	}
}

func TestChoose(t *testing.T) {
	at := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	fix := func(precision Precision, source FixSource) Fix {
		f := Fix{Point: Point{Lat: -23.55, Lng: -46.63}, Precision: precision, Source: source, FixedAt: at}
		if source == SourceProvider {
			f.Provider = "opencage"
		}
		return f
	}
	ptr := func(f Fix) *Fix { return &f }
	invalid := Fix{Precision: PrecisionExact, Source: SourceProvider, Provider: "opencage"}

	tests := []struct {
		name       string
		current    *Fix
		candidates []Fix
		expected   Fix
		changed    bool
	}{
		{"no current takes the best candidate", nil, []Fix{fix(PrecisionCity, SourceReference), fix(PrecisionStreet, SourceReference), fix(PrecisionDistrict, SourceReference)}, fix(PrecisionStreet, SourceReference), true},
		{"ties keep the first candidate in chain order", nil, []Fix{fix(PrecisionStreet, SourceReference), fix(PrecisionStreet, SourceProvider)}, fix(PrecisionStreet, SourceReference), true},
		{"no current and no candidates changes nothing", nil, nil, Fix{}, false},
		{"invalid candidates are never chosen", nil, []Fix{invalid}, Fix{}, false},
		{"an invalid candidate does not hide a valid one", nil, []Fix{invalid, fix(PrecisionCity, SourceReference)}, fix(PrecisionCity, SourceReference), true},
		{"a strictly better candidate replaces the current fix", ptr(fix(PrecisionDistrict, SourceReference)), []Fix{fix(PrecisionAddress, SourceProvider)}, fix(PrecisionAddress, SourceProvider), true},
		{"an equal candidate keeps the current fix", ptr(fix(PrecisionStreet, SourceReference)), []Fix{fix(PrecisionStreet, SourceProvider)}, fix(PrecisionStreet, SourceReference), false},
		{"a worse candidate keeps the current fix", ptr(fix(PrecisionStreet, SourceReference)), []Fix{fix(PrecisionCity, SourceReference)}, fix(PrecisionStreet, SourceReference), false},
		{"a manual pin is never replaced", ptr(fix(PrecisionCity, SourceManual)), []Fix{fix(PrecisionAddress, SourceProvider)}, fix(PrecisionCity, SourceManual), false},
		{"a location the lead sent is never replaced", ptr(fix(PrecisionDistrict, SourceLeadPin)), []Fix{fix(PrecisionExact, SourceImport)}, fix(PrecisionDistrict, SourceLeadPin), false},
		{"an imported exact fix is not replaced by a provider address", ptr(fix(PrecisionExact, SourceImport)), []Fix{fix(PrecisionAddress, SourceProvider)}, fix(PrecisionExact, SourceImport), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed := Choose(tt.current, tt.candidates)
			if changed != tt.changed {
				t.Fatalf("changed = %v, expected %v", changed, tt.changed)
			}
			if got != tt.expected {
				t.Fatalf("Choose() = %+v, expected %+v", got, tt.expected)
			}
		})
	}
}

func TestSameFix(t *testing.T) {
	at := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)
	pin := Fix{Point: Point{Lat: -23.55, Lng: -46.63}, Precision: PrecisionExact, Source: SourceManual, FixedAt: at}
	moved := pin
	moved.Point.Lat = -23.56
	resourced := pin
	resourced.Source = SourceLeadPin
	tests := []struct {
		name string
		a, b *Fix
		want bool
	}{
		{"both absent", nil, nil, true},
		{"one absent", &pin, nil, false},
		{"the other absent", nil, &pin, false},
		{"equal values at different addresses", &pin, &Fix{Point: pin.Point, Precision: pin.Precision, Source: pin.Source, FixedAt: at}, true},
		{"a moved point", &pin, &moved, false},
		{"another source", &pin, &resourced, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SameFix(tt.a, tt.b); got != tt.want {
				t.Fatalf("SameFix() = %v, expected %v", got, tt.want)
			}
		})
	}
}
