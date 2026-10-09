package geo

import (
	"errors"
	"math"
	"testing"
	"time"
)

func TestPinned(t *testing.T) {
	at := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.FixedZone("BRT", -3*3600))
	saoPaulo := Point{Lat: -23.55052, Lng: -46.633308}
	cases := []struct {
		name    string
		point   Point
		source  FixSource
		wantErr error
	}{
		{"a person's pin", saoPaulo, SourceManual, nil},
		{"a location the lead sent", saoPaulo, SourceLeadPin, nil},
		{"a reference point is not a pin", saoPaulo, SourceReference, ErrInvalidFix},
		{"a provider point is not a pin", saoPaulo, SourceProvider, ErrInvalidFix},
		{"an import is not a pin", saoPaulo, SourceImport, ErrInvalidFix},
		{"an unknown source", saoPaulo, FixSource("gps"), ErrInvalidFix},
		{"the zero point", Point{}, SourceManual, ErrInvalidPoint},
		{"not a number", Point{Lat: math.NaN(), Lng: -46.6}, SourceManual, ErrInvalidPoint},
		{"outside Brazil", Point{Lat: 38.7223, Lng: -9.1393}, SourceManual, ErrPointOutsideBrazil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Pinned(tc.point, tc.source, at)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Pinned() error = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				return
			}
			want := Fix{Point: tc.point, Precision: PrecisionExact, Source: tc.source, FixedAt: at.UTC()}
			if got != want || got.Validate() != nil {
				t.Fatalf("Pinned() = %+v, want %+v", got, want)
			}
		})
	}
}
