package geo

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/address"
	"vozko/domain/cep"
)

func TestReferenceQueryOfNeedsACEPOrACityWithItsState(t *testing.T) {
	tests := []struct {
		name  string
		raw   address.Postal
		valid bool
	}{
		{"a masked CEP alone", address.Postal{ZipCode: "01310-100"}, true},
		{"a city with its state", address.Postal{City: "São Paulo", State: "sp"}, true},
		{"a bairro with city and state", address.Postal{District: "Bela Vista", City: "São Paulo", State: "SP"}, true},
		{"nothing", address.Postal{}, false},
		{"a city without state", address.Postal{City: "Campinas"}, false},
		{"a malformed CEP", address.Postal{ZipCode: "0131"}, false},
		{"a malformed CEP next to a valid city", address.Postal{ZipCode: "abc", City: "Campinas", State: "SP"}, false},
		{"an unknown state", address.Postal{City: "Campinas", State: "XX"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ReferenceQueryOf(tt.raw)
			if tt.valid {
				if err != nil {
					t.Fatalf("refused: %v", err)
				}
				if got != tt.raw.Normalize() {
					t.Fatalf("query = %+v, want the normalized postal", got)
				}
				return
			}
			if !errors.Is(err, ErrReferenceQueryInvalid) {
				t.Fatalf("err = %v, want ErrReferenceQueryInvalid", err)
			}
		})
	}
}

func TestReferencePointOfPicksTheBestLoadedPoint(t *testing.T) {
	at := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	cepPoint := Point{Lat: -23.5614, Lng: -46.6559}
	districtPoint := Point{Lat: -23.56, Lng: -46.65}
	cityPoint := Point{Lat: -23.55, Lng: -46.63}
	place := address.Place{CityCode: "3550308", DistrictKey: address.DistrictKey("Bela Vista")}
	loaded := ReferenceIndex{
		CEPs:      map[string]ReferencePoint{"01310100": {Point: cepPoint, SpreadM: 120, CityCode: "3550308"}},
		Districts: map[address.Place]ReferencePoint{place: {Point: districtPoint}},
		Cities:    map[string]ReferencePoint{"3550308": {Point: cityPoint}},
		CityCodes: map[CityName]string{{State: "SP", NameKey: address.CityNameKey("São Paulo")}: "3550308"},
	}
	saoPaulo := Coverage{States: map[string]bool{"SP": true}}
	reference := func(p Point, precision Precision) Fix {
		return Fix{Point: p, Precision: precision, Source: SourceReference, FixedAt: at}
	}
	tests := []struct {
		name     string
		coverage Coverage
		index    ReferenceIndex
		query    address.Postal
		want     Fix
		err      error
	}{
		{"a tight CEP answers at street precision", saoPaulo, loaded, address.Postal{ZipCode: "01310100"}, reference(cepPoint, PrecisionStreet), nil},
		{"a CEP beats the bairro of the same address", saoPaulo, loaded, address.Postal{ZipCode: "01310100", District: "Bela Vista", City: "São Paulo", State: "SP"}, reference(cepPoint, PrecisionStreet), nil},
		{"a bairro with its city answers the bairro point", saoPaulo, loaded, address.Postal{District: "Bela Vista", City: "São Paulo", State: "SP"}, reference(districtPoint, PrecisionDistrict), nil},
		{"a city alone answers the city point", saoPaulo, loaded, address.Postal{City: "São Paulo", State: "SP"}, reference(cityPoint, PrecisionCity), nil},
		{"a CEP without state is covered through its city code", saoPaulo, loaded, address.Postal{ZipCode: "01310100"}, reference(cepPoint, PrecisionStreet), nil},
		{"an unknown CEP in a partly loaded country is not loaded", saoPaulo, loaded, address.Postal{ZipCode: "20040002"}, Fix{}, ErrReferenceNotLoaded},
		{"a state that was never loaded", saoPaulo, loaded, address.Postal{City: "Rio de Janeiro", State: "RJ"}, Fix{}, ErrReferenceNotLoaded},
		{"nothing loaded at all", Coverage{}, ReferenceIndex{}, address.Postal{ZipCode: "01310100"}, Fix{}, ErrReferenceNotLoaded},
		{"a covered place without a point", saoPaulo, ReferenceIndex{}, address.Postal{City: "Campinas", State: "SP"}, Fix{}, ErrReferencePointNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ReferencePointOf(tt.coverage, tt.index, tt.query.Normalize(), at)
			if !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if got != tt.want {
				t.Fatalf("fix = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestEveryReferenceRefusalHasACode(t *testing.T) {
	codes := map[error]string{
		ErrReferenceQueryInvalid:  "reference_query_invalid",
		ErrReferenceNotLoaded:     string(ReasonReferenceNotLoaded),
		ErrReferencePointNotFound: "reference_point_not_found",
		ErrReferenceUnavailable:   string(ReasonReferenceDown),
	}
	for err, want := range codes {
		if got := ReferenceErrorCode(errors.Join(errors.New("context"), err)); got != want {
			t.Errorf("code of %v = %q, want %q", err, got, want)
		}
	}
	if got := ReferenceErrorCode(cep.ErrInvalidCEP); got != "" {
		t.Fatalf("an unrelated error got code %q", got)
	}
}
