package lead

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"vozko/domain/geo"
	leaddomain "vozko/domain/lead"
)

func TestAddressResponseCarriesTheStatusAndTheSourceOfThePosition(t *testing.T) {
	at := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	fix := geo.Fix{Point: geo.Point{Lat: -23.56, Lng: -46.65}, Precision: geo.PrecisionAddress, Source: geo.SourceProvider, Provider: "opencage", FixedAt: at}
	got := addressResponses([]leaddomain.Address{
		{ID: "a-1", Label: leaddomain.AddressHome, Primary: true, Fix: &fix, GeoStatus: leaddomain.GeoLocated},
		{ID: "a-2", Label: leaddomain.AddressWork, GeoStatus: leaddomain.GeoUnavailable},
	})
	located := got[0]
	if located.GeoStatus != "located" || located.Precision != "address" || located.PositionSource != "provider" || located.PositionProvider != "opencage" {
		t.Fatalf("located address = %+v, want the provider kept for attribution", located)
	}
	if located.GeocodedAt != "2026-10-08T12:00:00Z" {
		t.Fatalf("geocodedAt = %q, want when the position was found", located.GeocodedAt)
	}
	waiting := got[1]
	if waiting.GeoStatus != "unavailable" || waiting.Precision != "" || waiting.PositionProvider != "" || waiting.GeocodedAt != "" {
		t.Fatalf("waiting address = %+v, want only the status", waiting)
	}
}

func TestAddressResponseSaysWhetherTheAddressIsStillQueued(t *testing.T) {
	for _, status := range leaddomain.GeoStatuses() {
		got := addressResponses([]leaddomain.Address{{ID: "a-1", GeoStatus: status}})[0]
		if got.GeoQueued != status.Queued() {
			t.Fatalf("%s: geoQueued = %v, want %v", status, got.GeoQueued, status.Queued())
		}
	}
}

func TestAddressGeoStatusEnumListsEveryDomainStatus(t *testing.T) {
	field, ok := reflect.TypeOf(AddressResponse{}).FieldByName("GeoStatus")
	if !ok {
		t.Fatal("AddressResponse has no GeoStatus")
	}
	documented := strings.Split(field.Tag.Get("enums"), ",")
	if len(documented) != len(leaddomain.GeoStatuses()) {
		t.Fatalf("documented statuses = %v, want %v", documented, leaddomain.GeoStatuses())
	}
	for i, status := range leaddomain.GeoStatuses() {
		if documented[i] != string(status) {
			t.Fatalf("documented statuses = %v, want %v", documented, leaddomain.GeoStatuses())
		}
	}
}
