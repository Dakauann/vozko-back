package lead

import (
	"reflect"
	"testing"

	"vozko/domain/address"
	"vozko/domain/geo"
)

func TestAutomatedAddressesFillOnlyWhatIsEmpty(t *testing.T) {
	importFix := geo.Fix{Point: geo.Point{Lat: -23.5613, Lng: -46.6565}, Precision: geo.PrecisionExact, Source: geo.SourceImport, FixedAt: fixedAt}
	cityOnly := Address{ID: "a-1", Label: AddressHome, Primary: true, Postal: address.Postal{City: "São Paulo", State: "SP"}, GeoStatus: GeoPending}
	cases := []struct {
		name      string
		stored    []Address
		incoming  Address
		changed   bool
		conflict  bool
		added     bool
		filled    bool
		wantFix   *geo.Fix
		wantState GeoStatus
	}{
		{name: "no address yet adds a primary one", incoming: Address{Postal: homePostal}, changed: true, added: true, wantState: GeoPending},
		{name: "no address yet keeps the coordinates that came with it", incoming: Address{Postal: homePostal, Fix: &importFix, GeoStatus: GeoLocated}, changed: true, added: true, wantFix: &importFix, wantState: GeoLocated},
		{name: "empty parts of the primary address are filled", stored: []Address{cityOnly}, incoming: Address{Postal: homePostal}, changed: true, filled: true, wantState: GeoPending},
		{name: "filled parts take the coordinates that came with them", stored: []Address{cityOnly}, incoming: Address{Postal: homePostal, Fix: &importFix}, changed: true, filled: true, wantFix: &importFix, wantState: GeoLocated},
		{name: "a manual pin is kept when parts are filled", stored: []Address{{ID: "a-1", Label: AddressHome, Primary: true, Postal: cityOnly.Postal, Fix: &manualFix, GeoStatus: GeoLocated}}, incoming: Address{Postal: homePostal, Fix: &importFix}, changed: true, filled: true, wantFix: &manualFix, wantState: GeoLocated},
		{name: "an address the stored one already covers changes nothing", stored: []Address{storedHome(&cepFix, GeoApproximate)}, incoming: Address{Postal: address.Postal{City: "sao paulo", State: "São Paulo"}}, wantFix: &cepFix, wantState: GeoApproximate},
		{name: "a differing part is a conflict and nothing changes", stored: []Address{storedHome(&manualFix, GeoLocated)}, incoming: Address{Postal: workPostal}, conflict: true, wantFix: &manualFix, wantState: GeoLocated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := profileLead(tc.stored...)
			f := newGapFill(l, 1)
			if err := f.address(tc.incoming); err != nil {
				t.Fatalf("address: %v", err)
			}
			if got := len(f.changed) > 0; got != tc.changed {
				t.Fatalf("changed = %v, want %v", f.changed, tc.changed)
			}
			if got := len(f.conflicts) > 0; got != tc.conflict {
				t.Fatalf("conflicts = %v, want %v", f.conflicts, tc.conflict)
			}
			if (f.newAddress != nil) != tc.added || (f.filledAddress != nil) != tc.filled {
				t.Fatalf("added %+v filled %+v", f.newAddress, f.filledAddress)
			}
			primary := f.next.PrimaryAddress()
			if primary == nil || !reflect.DeepEqual(primary.Fix, tc.wantFix) || primary.GeoStatus != tc.wantState {
				t.Fatalf("primary = %+v, want fix %+v status %s", primary, tc.wantFix, tc.wantState)
			}
			if !reflect.DeepEqual(l.Addresses, profileLead(tc.stored...).Addresses) {
				t.Fatalf("the stored lead was changed in place: %+v", l.Addresses)
			}
		})
	}
}
