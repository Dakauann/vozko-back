package advertising

import (
	"errors"
	"testing"
)

func TestLocationsTakeTheirNamesFromMetaNotFromWhoeverAskedForThem(t *testing.T) {
	wanted := []GeoLocation{{Kind: LocationRegion, Key: "455", Name: "BR-RN"}, {Kind: LocationCity, Key: "1", RadiusKm: 10}}
	known := []RemoteLocation{{Kind: LocationRegion, Key: "455", Name: "Rio Grande do Norte"}, {Kind: LocationCity, Key: "1", Name: "Natal"}}
	got, err := NameLocations(wanted, known)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Name != "Rio Grande do Norte" || got[1].Name != "Natal" || got[1].RadiusKm != 10 {
		t.Fatalf("%+v", got)
	}
}

func TestALocationMetaDoesNotKnowIsRefused(t *testing.T) {
	_, err := NameLocations([]GeoLocation{{Kind: LocationRegion, Key: "BR-RN"}}, []RemoteLocation{{Kind: LocationCity, Key: "BR-RN", Name: "?"}})
	var invalid *ValidationError
	if !errors.As(err, &invalid) || invalid.Issues[0].Code != CodeUnknownLocation {
		t.Fatalf("err %v", err)
	}
}
