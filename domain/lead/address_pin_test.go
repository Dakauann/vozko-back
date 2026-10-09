package lead

import (
	"errors"
	"testing"

	"vozko/domain/address"
	"vozko/domain/geo"
)

func TestANewLeadStoresThePinChosenBeforeSaving(t *testing.T) {
	point := geo.Point{Lat: -23.5620, Lng: -46.6570}
	l, err := New("ws-1", Draft{Name: "Maria", Addresses: []AddressInput{{Label: AddressHome, Postal: homePostal, Pin: &point}}}, pinnedAt)
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	got := l.Addresses[0]
	want := geo.Fix{Point: point, Precision: geo.PrecisionExact, Source: geo.SourceManual, FixedAt: pinnedAt}
	if got.Fix == nil || *got.Fix != want || got.GeoStatus != GeoLocated {
		t.Fatalf("address = %+v, want the manual pin %+v located", got, want)
	}
}

func TestAnEditCarriesThePinThroughTheManualRules(t *testing.T) {
	point := geo.Point{Lat: -23.5620, Lng: -46.6570}
	moved := geo.Fix{Point: point, Precision: geo.PrecisionExact, Source: geo.SourceManual, FixedAt: pinnedAt}
	sameAsStored := manualFix.Point
	cases := []struct {
		name    string
		stored  Address
		input   AddressInput
		wantFix geo.Fix
	}{
		{"a pin replaces the reference point of an unchanged text", storedHome(&cepFix, GeoApproximate),
			AddressInput{ID: "a-1", Label: AddressHome, Primary: true, Postal: homePostal, Pin: &point}, moved},
		{"a pin chosen with a new text is kept", storedHome(&cepFix, GeoApproximate),
			AddressInput{ID: "a-1", Label: AddressHome, Primary: true, Postal: workPostal, Pin: &point}, moved},
		{"a pin replaces an older pin", storedHome(&manualFix, GeoLocated),
			AddressInput{ID: "a-1", Label: AddressHome, Primary: true, Postal: homePostal, Pin: &point}, moved},
		{"the stored pin sent again keeps its time", storedHome(&manualFix, GeoLocated),
			AddressInput{ID: "a-1", Label: AddressHome, Primary: true, Postal: homePostal, Pin: &sameAsStored}, manualFix},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := leadWith(tc.stored)
			inputs := []AddressInput{tc.input}
			if err := l.ApplyEdit(Edit{Addresses: &inputs}, pinnedAt); err != nil {
				t.Fatalf("ApplyEdit() = %v", err)
			}
			got := l.Addresses[0]
			if got.Fix == nil || *got.Fix != tc.wantFix || got.GeoStatus != GeoLocated {
				t.Fatalf("address = %+v, want %+v located", got, tc.wantFix)
			}
		})
	}
}

func TestAPinWithoutAddressTextMakesAPositionOnlyAddress(t *testing.T) {
	point := geo.Point{Lat: -8.05, Lng: -34.9}
	l, err := New("ws-1", Draft{Name: "Maria", Addresses: []AddressInput{{Label: AddressHome, Pin: &point}}}, pinnedAt)
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	if got := l.Addresses[0]; !got.positionOnly() || got.GeoStatus != GeoLocated {
		t.Fatalf("address = %+v, want a located address that holds only the pin", got)
	}
}

func TestABadPinRefusesTheWholeEdit(t *testing.T) {
	lisbon := geo.Point{Lat: 38.7, Lng: -9.1}
	broken := geo.Point{Lat: 123, Lng: -46}
	for name, point := range map[string]geo.Point{"outside Brazil": lisbon, "not a coordinate": broken} {
		t.Run(name, func(t *testing.T) {
			l := leadWith(storedHome(&cepFix, GeoApproximate))
			inputs := []AddressInput{{ID: "a-1", Label: AddressHome, Primary: true, Postal: homePostal, Pin: &point}}
			err := l.ApplyEdit(Edit{Addresses: &inputs}, pinnedAt)
			var item *ItemError
			if !errors.Is(err, ErrLocationInvalid) || !errors.As(err, &item) || item.Index != 0 || item.Field != FieldAddresses {
				t.Fatalf("ApplyEdit() = %v, want ErrLocationInvalid on address 0", err)
			}
			if *l.Addresses[0].Fix != cepFix {
				t.Fatalf("a refused edit changed the stored address: %+v", l.Addresses[0])
			}
		})
	}
}

func TestSetAddressesWithoutAClockRefusesAPin(t *testing.T) {
	point := geo.Point{Lat: -8.05, Lng: -34.9}
	l := leadWith()
	if err := l.SetAddresses([]AddressInput{{Label: AddressHome, Postal: homePostal, Pin: &point}}); !errors.Is(err, ErrLocationInvalid) {
		t.Fatalf("SetAddresses() = %v, want ErrLocationInvalid", err)
	}
}

func TestANewBlankAddressWithoutAPinIsStillRefused(t *testing.T) {
	l := leadWith()
	inputs := []AddressInput{{Label: AddressHome}}
	if err := l.ApplyEdit(Edit{Addresses: &inputs}, pinnedAt); !errors.Is(err, address.ErrInvalidAddress) {
		t.Fatalf("ApplyEdit() = %v, want an invalid address", err)
	}
}

func TestEditPinsTellWhetherAnEditSetsAPosition(t *testing.T) {
	point := geo.Point{Lat: -8.05, Lng: -34.9}
	if (Edit{}).SetsPins() {
		t.Fatal("an edit without addresses sets no pin")
	}
	plain := []AddressInput{{Label: AddressHome, Postal: homePostal}}
	if (Edit{Addresses: &plain}).SetsPins() {
		t.Fatal("addresses without a pin set no pin")
	}
	pinned := []AddressInput{{Label: AddressHome, Postal: homePostal}, {Label: AddressWork, Postal: workPostal, Pin: &point}}
	if !(Edit{Addresses: &pinned}).SetsPins() || !(Draft{Addresses: pinned}).SetsPins() {
		t.Fatal("an address with a pin sets a position")
	}
}
