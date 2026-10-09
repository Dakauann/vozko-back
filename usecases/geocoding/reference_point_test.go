package geocoding_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/address"
	"vozko/domain/geo"
)

func TestReferencePointsNeedTheReferenceData(t *testing.T) {
	if _, err := NewReferencePoints(nil, nil); err == nil {
		t.Fatal("reference points were built without the reference data")
	}
}

func TestReferencePointsLocateACEP(t *testing.T) {
	reference := loadedReference()
	points, err := NewReferencePoints(reference, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	spot, err := points.Locate(context.Background(), address.Postal{ZipCode: "01310-100"})
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	want := geo.Fix{Point: cepPoint, Precision: geo.PrecisionStreet, Source: geo.SourceReference, FixedAt: now}
	if spot.Fix != want || spot.WholeCity {
		t.Fatalf("spot = %+v, want %+v on a street", spot, want)
	}
	if len(reference.looked) != 1 || reference.looked[0].ZipCode != "01310100" {
		t.Fatalf("looked up %+v, want the normalized CEP once", reference.looked)
	}
}

func TestReferencePointsRefuseWithACode(t *testing.T) {
	tests := []struct {
		name      string
		reference *fakeReference
		query     address.Postal
		want      error
		lookups   int
	}{
		{"an invalid query never reads the data", loadedReference(), address.Postal{City: "Campinas"}, geo.ErrReferenceQueryInvalid, 0},
		{"unreadable coverage", &fakeReference{coverageErr: errDown}, address.Postal{ZipCode: "01310100"}, geo.ErrReferenceUnavailable, 0},
		{"a failed lookup", &fakeReference{coverage: everyState(), lookupErr: errDown}, address.Postal{ZipCode: "01310100"}, geo.ErrReferenceUnavailable, 1},
		{"no reference data loaded", &fakeReference{}, address.Postal{ZipCode: "01310100"}, geo.ErrReferenceNotLoaded, 1},
		{"a loaded place without a point", &fakeReference{coverage: everyState()}, address.Postal{ZipCode: "01310100"}, geo.ErrReferencePointNotFound, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			points, err := NewReferencePoints(tt.reference, func() time.Time { return now })
			if err != nil {
				t.Fatal(err)
			}
			_, err = points.Locate(context.Background(), tt.query)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if tt.reference.lookups != tt.lookups {
				t.Fatalf("lookups = %d, want %d", tt.reference.lookups, tt.lookups)
			}
		})
	}
}
