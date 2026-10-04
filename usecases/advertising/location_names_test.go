package advertising

import (
	"context"
	"errors"
	"testing"

	ads "vozko/domain/advertising"
)

func TestLocationsAreNamedByMetaBeforeAnyChangeIsShown(t *testing.T) {
	w := newWorld()
	w.gateway.knownLocations = []ads.RemoteLocation{{Kind: ads.LocationRegion, Key: "455", Name: "Rio Grande do Norte"}}
	uc := NewAssetsUseCase(w.sync, w.gateway, w.numbers)
	named, err := uc.NameLocations(context.Background(), "ws-1", "acc-1", []ads.GeoLocation{{Kind: ads.LocationRegion, Key: "455", Name: "BR-RN"}})
	if err != nil || named[0].Name != "Rio Grande do Norte" {
		t.Fatalf("named %+v err %v", named, err)
	}
	_, err = uc.NameLocations(context.Background(), "ws-1", "acc-1", []ads.GeoLocation{{Kind: ads.LocationRegion, Key: "BR-RN"}})
	var invalid *ads.ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("an invented key passed: %v", err)
	}
}

func TestNamingNoLocationsAsksMetaNothing(t *testing.T) {
	w := newWorld()
	uc := NewAssetsUseCase(w.sync, w.gateway, w.numbers)
	if named, err := uc.NameLocations(context.Background(), "ws-1", "acc-1", nil); err != nil || len(named) != 0 || len(w.gateway.calls) != 0 {
		t.Fatalf("named %v err %v calls %v", named, err, w.gateway.calls)
	}
}
