package lead

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"vozko/domain/address"
	"vozko/domain/geo"
	"vozko/domain/recordevent"
)

var (
	pinnedAt  = time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)
	pinPoint  = geo.Point{Lat: -23.5505, Lng: -46.6333}
	personPin = geo.Fix{Point: pinPoint, Precision: geo.PrecisionExact, Source: geo.SourceManual, FixedAt: pinnedAt}
	leadPin   = geo.Fix{Point: pinPoint, Precision: geo.PrecisionExact, Source: geo.SourceLeadPin, FixedAt: pinnedAt}
)

func storedWork(fix *geo.Fix, status GeoStatus) Address {
	return Address{ID: "a-2", Label: AddressWork, Postal: workPostal.Normalize(), Fix: fix, GeoStatus: status}
}

func leadWith(addresses ...Address) *Lead {
	l := &Lead{ID: "lead-1", WorkspaceID: "ws-1", Name: "Maria", Version: 3, Phones: []ContactPhone{}, Addresses: append([]Address{}, addresses...)}
	return l
}

func TestPinLocation(t *testing.T) {
	cases := []struct {
		name      string
		lead      *Lead
		addressID string
		fix       geo.Fix
		changed   bool
		wantErr   error
	}{
		{"an address without a position gets the pin", leadWith(storedHome(nil, GeoPending)), "a-1", personPin, true, nil},
		{"a pin replaces a reference point", leadWith(storedHome(&cepFix, GeoApproximate)), "a-1", personPin, true, nil},
		{"a new pin replaces an older one", leadWith(storedHome(&manualFix, GeoLocated)), "a-1", personPin, true, nil},
		{"the same pin again changes nothing", leadWith(storedHome(&personPin, GeoLocated)), "a-1", personPin, false, nil},
		{"another address of the lead", leadWith(storedHome(nil, GeoPending), storedWork(nil, GeoPending)), "a-2", personPin, true, nil},
		{"an address of someone else", leadWith(storedHome(nil, GeoPending)), "a-9", personPin, false, ErrAddressNotFound},
		{"a blank address id", leadWith(storedHome(nil, GeoPending)), " ", personPin, false, ErrAddressNotFound},
		{"a reference point is not a pin", leadWith(storedHome(nil, GeoPending)), "a-1", cepFix, false, ErrLocationInvalid},
		{"a pin outside Brazil", leadWith(storedHome(nil, GeoPending)), "a-1",
			geo.Fix{Point: geo.Point{Lat: 38.7, Lng: -9.1}, Precision: geo.PrecisionExact, Source: geo.SourceManual, FixedAt: pinnedAt}, false, ErrLocationInvalid},
		{"a pin that is not exact", leadWith(storedHome(nil, GeoPending)), "a-1",
			geo.Fix{Point: pinPoint, Precision: geo.PrecisionStreet, Source: geo.SourceManual, FixedAt: pinnedAt}, false, ErrLocationInvalid},
		{"a lead read without its addresses", &Lead{ID: "lead-1"}, "a-1", personPin, false, ErrAggregateNotLoaded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := append([]Address{}, tc.lead.Addresses...)
			shared := *tc.lead
			changed, err := shared.PinLocation(tc.addressID, tc.fix)
			if !errors.Is(err, tc.wantErr) || changed != tc.changed {
				t.Fatalf("PinLocation() = %v, %v; want %v, %v", changed, err, tc.changed, tc.wantErr)
			}
			if len(tc.lead.Addresses)+len(before) > 0 && !reflect.DeepEqual(tc.lead.Addresses, before) {
				t.Fatalf("the lead it was copied from changed: %+v", tc.lead.Addresses)
			}
			if !changed {
				return
			}
			for _, a := range shared.Addresses {
				original := addressByID(before, a.ID)
				if a.ID != tc.addressID {
					if !reflect.DeepEqual(a, original) {
						t.Fatalf("address %s changed: %+v", a.ID, a)
					}
					continue
				}
				if a.Fix == nil || *a.Fix != tc.fix || a.GeoStatus != GeoLocated || a.Postal != original.Postal {
					t.Fatalf("pinned address = %+v", a)
				}
			}
		})
	}
}

func TestAcceptLocation(t *testing.T) {
	cases := []struct {
		name    string
		lead    *Lead
		changed bool
		wantErr error
		check   func(t *testing.T, got []Address)
	}{
		{
			name: "the primary address gets the location the lead sent", changed: true,
			lead: leadWith(storedWork(nil, GeoPending), storedHome(&cepFix, GeoApproximate)),
			check: func(t *testing.T, got []Address) {
				if got[1].Fix == nil || *got[1].Fix != leadPin || got[1].GeoStatus != GeoLocated || got[0].Fix != nil {
					t.Fatalf("addresses = %+v", got)
				}
			},
		},
		{
			name: "a lead without an address gets a primary home address at that position", changed: true,
			lead: leadWith(),
			check: func(t *testing.T, got []Address) {
				if len(got) != 1 || got[0].ID != "" || !got[0].Primary || got[0].Label != AddressHome || got[0].GeoStatus != GeoLocated {
					t.Fatalf("addresses = %+v", got)
				}
				if got[0].Fix == nil || *got[0].Fix != leadPin || got[0].Postal.Fingerprint() == "" || got[0].Postal.City != "" {
					t.Fatalf("address = %+v", got[0])
				}
			},
		},
		{
			name: "a person accepting it replaces an older pin", changed: true,
			lead: leadWith(storedHome(&manualFix, GeoLocated)),
			check: func(t *testing.T, got []Address) {
				if *got[0].Fix != leadPin {
					t.Fatalf("addresses = %+v", got)
				}
			},
		},
		{name: "accepting the same location again changes nothing", lead: leadWith(storedHome(&leadPin, GeoLocated))},
		{name: "a lead read without its addresses", lead: &Lead{ID: "lead-1"}, wantErr: ErrAggregateNotLoaded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := append([]Address{}, tc.lead.Addresses...)
			shared := *tc.lead
			changed, err := shared.AcceptLocation(leadPin)
			if !errors.Is(err, tc.wantErr) || changed != tc.changed {
				t.Fatalf("AcceptLocation() = %v, %v; want %v, %v", changed, err, tc.changed, tc.wantErr)
			}
			if len(tc.lead.Addresses)+len(before) > 0 && !reflect.DeepEqual(tc.lead.Addresses, before) {
				t.Fatalf("the lead it was copied from changed: %+v", tc.lead.Addresses)
			}
			if tc.check != nil {
				tc.check(t, shared.Addresses)
			}
			if changed && shared.validateAddresses() != nil {
				t.Fatalf("addresses invalid: %v", shared.validateAddresses())
			}
		})
	}
	if _, err := leadWith().AcceptLocation(personPin); !errors.Is(err, ErrLocationInvalid) {
		t.Fatalf("accepting a person's pin as a sent location: %v", err)
	}
	if _, err := leadWith().AcceptLocation(cepFix); !errors.Is(err, ErrLocationInvalid) {
		t.Fatalf("accepting a reference point: %v", err)
	}
}

func TestPinEventRecordsWhatMovedWithoutTheCoordinates(t *testing.T) {
	before := leadWith(storedHome(&cepFix, GeoApproximate), storedWork(nil, GeoPending))
	after := *before
	if _, err := after.PinLocation("a-1", personPin); err != nil {
		t.Fatal(err)
	}
	event := PinEvent(EventLocationPinned, "user-1", before, &after)
	want := recordevent.Event{Actor: "user-1", Kind: EventLocationPinned, Changes: []recordevent.Change{{
		Field:  FieldAddresses,
		Before: map[string]any{"label": "home", "precision": "postal_code", "source": "reference"},
		After:  map[string]any{"label": "home", "precision": "exact", "source": "manual"},
	}}}
	if !reflect.DeepEqual(event, want) {
		t.Fatalf("PinEvent() = %+v, want %+v", event, want)
	}

	empty := leadWith()
	accepted := *empty
	if _, err := accepted.AcceptLocation(leadPin); err != nil {
		t.Fatal(err)
	}
	event = PinEvent(EventLocationAccepted, "user-1", empty, &accepted)
	if len(event.Changes) != 1 || event.Changes[0].Before != nil ||
		!reflect.DeepEqual(event.Changes[0].After, map[string]any{"label": "home", "precision": "exact", "source": "lead_pin"}) {
		t.Fatalf("PinEvent() for a new address = %+v", event)
	}
	if PinEvent(EventLocationPinned, "user-1", before, before).Empty() == false {
		t.Fatal("nothing moved, so the event is empty")
	}
}

func addressByID(addresses []Address, id string) Address {
	for _, a := range addresses {
		if a.ID == id {
			return a
		}
	}
	return Address{}
}

func TestAPositionOnlyAddressSurvivesAnEditThatSendsItBack(t *testing.T) {
	pinnedOnly := Address{ID: "a-1", Label: AddressHome, Primary: true, Fix: &leadPin, GeoStatus: GeoLocated}
	referenceOnly := Address{ID: "a-1", Label: AddressHome, Primary: true, Fix: &cepFix, GeoStatus: GeoApproximate}
	cases := []struct {
		name    string
		stored  Address
		input   AddressInput
		wantErr bool
	}{
		{"sent back blank with its id", pinnedOnly, AddressInput{ID: "a-1", Label: AddressHome, Primary: true}, false},
		{"sent back with a new label", pinnedOnly, AddressInput{ID: "a-1", Label: AddressWork, Primary: true}, false},
		{"a new blank address", pinnedOnly, AddressInput{Label: AddressHome, Primary: true}, true},
		{"blank over a position that nobody confirmed", referenceOnly, AddressInput{ID: "a-1", Label: AddressHome, Primary: true}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := leadWith(tc.stored)
			err := l.SetAddresses([]AddressInput{tc.input})
			if (err != nil) != tc.wantErr {
				t.Fatalf("SetAddresses() error = %v, want error %v", err, tc.wantErr)
			}
			if tc.wantErr {
				if !errors.Is(err, address.ErrInvalidAddress) {
					t.Fatalf("SetAddresses() error = %v, want an invalid address", err)
				}
				return
			}
			if got := l.Addresses[0]; got.Fix == nil || *got.Fix != leadPin || got.GeoStatus != GeoLocated || got.Label != tc.input.Label {
				t.Fatalf("address = %+v", got)
			}
			if err := l.validateAddresses(); err != nil {
				t.Fatalf("validateAddresses() = %v", err)
			}
		})
	}
}

func TestAConfirmedPositionSurvivesTypingTheAddressText(t *testing.T) {
	pinnedOnly := Address{ID: "a-1", Label: AddressHome, Primary: true, Fix: &leadPin, GeoStatus: GeoLocated}
	cases := []struct {
		name    string
		stored  Address
		input   AddressInput
		wantFix *geo.Fix
	}{
		{"filling in the address an accepted location created", pinnedOnly, AddressInput{ID: "a-1", Label: AddressHome, Primary: true, Postal: homePostal}, &leadPin},
		{"filling it in while asking to keep the position", pinnedOnly, AddressInput{ID: "a-1", Label: AddressHome, Primary: true, Postal: homePostal, KeepFix: true}, &leadPin},
		{"fixing a typo under a location the lead sent", storedHome(&leadPin, GeoLocated), AddressInput{ID: "a-1", Label: AddressHome, Primary: true, Postal: workPostal, KeepFix: true}, &leadPin},
		{"a new text under a location the lead sent without keeping it", storedHome(&leadPin, GeoLocated), AddressInput{ID: "a-1", Label: AddressHome, Primary: true, Postal: workPostal}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := leadWith(tc.stored)
			if err := l.SetAddresses([]AddressInput{tc.input}); err != nil {
				t.Fatalf("SetAddresses() = %v", err)
			}
			got := l.Addresses[0]
			if !geo.SameFix(got.Fix, tc.wantFix) {
				t.Fatalf("fix = %+v, want %+v", got.Fix, tc.wantFix)
			}
			wantStatus := GeoPending
			if tc.wantFix != nil {
				wantStatus = GeoLocated
			}
			if got.GeoStatus != wantStatus {
				t.Fatalf("status = %q, want %q", got.GeoStatus, wantStatus)
			}
		})
	}
}

func TestAcceptingThenTypingTheAddressKeepsTheLocationTheLeadSent(t *testing.T) {
	l := leadWith()
	if _, err := l.AcceptLocation(leadPin); err != nil {
		t.Fatal(err)
	}
	l.Addresses[0].ID = "a-1"
	if err := l.SetAddresses([]AddressInput{{ID: "a-1", Label: AddressHome, Primary: true, Postal: homePostal}}); err != nil {
		t.Fatalf("SetAddresses() = %v", err)
	}
	got := l.Addresses[0]
	if got.Fix == nil || *got.Fix != leadPin || got.GeoStatus != GeoLocated || got.Postal.City == "" {
		t.Fatalf("address = %+v, want the lead_pin position kept and located", got)
	}
}
