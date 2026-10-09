package lead

import (
	"errors"
	"slices"
	"testing"
	"time"

	"vozko/domain/address"
	"vozko/domain/geo"
)

var (
	homePostal = address.Postal{ZipCode: "01310-100", Street: "Avenida Paulista", Number: "1000", District: "Bela Vista", City: "São Paulo", State: "sp"}
	workPostal = address.Postal{ZipCode: "30130-010", Street: "Rua da Bahia", Number: "10", District: "Centro", City: "Belo Horizonte", State: "MG"}
	fixedAt    = time.Date(2026, time.October, 1, 9, 0, 0, 0, time.UTC)
	manualFix  = geo.Fix{Point: geo.Point{Lat: -23.5613, Lng: -46.6565}, Precision: geo.PrecisionExact, Source: geo.SourceManual, FixedAt: fixedAt}
	cepFix     = geo.Fix{Point: geo.Point{Lat: -23.5610, Lng: -46.6560}, Precision: geo.PrecisionPostalCode, Source: geo.SourceReference, FixedAt: fixedAt}
)

func storedHome(fix *geo.Fix, status GeoStatus) Address {
	return Address{ID: "a-1", Label: AddressHome, Primary: true, Postal: homePostal.Normalize(), Fix: fix, GeoStatus: status}
}

func TestSetAddresses(t *testing.T) {
	cases := []struct {
		name      string
		stored    []Address
		inputs    []AddressInput
		check     func(t *testing.T, got []Address)
		wantErr   error
		wantIndex int
	}{
		{
			name:   "a new address is normalized, pending and primary when it is the only one",
			inputs: []AddressInput{{Label: AddressHome, Postal: homePostal}},
			check: func(t *testing.T, got []Address) {
				if len(got) != 1 || !got[0].Primary || got[0].GeoStatus != GeoPending || got[0].Fix != nil {
					t.Fatalf("addresses = %+v", got)
				}
				if got[0].Postal.ZipCode != "01310100" || got[0].Postal.State != "SP" {
					t.Fatalf("postal not normalized: %+v", got[0].Postal)
				}
			},
		},
		{
			name: "two addresses need exactly one primary",
			inputs: []AddressInput{
				{Label: AddressHome, Postal: homePostal},
				{Label: AddressWork, Postal: workPostal},
			},
			wantErr:   ErrAddressPrimary,
			wantIndex: -1,
		},
		{
			name: "two primaries are refused",
			inputs: []AddressInput{
				{Label: AddressHome, Primary: true, Postal: homePostal},
				{Label: AddressWork, Primary: true, Postal: workPostal},
			},
			wantErr:   ErrAddressPrimary,
			wantIndex: -1,
		},
		{
			name:   "the primary can move to another address",
			stored: []Address{storedHome(&cepFix, GeoApproximate)},
			inputs: []AddressInput{
				{ID: "a-1", Label: AddressHome, Postal: homePostal},
				{Label: AddressWork, Primary: true, Postal: workPostal},
			},
			check: func(t *testing.T, got []Address) {
				if got[0].Primary || !got[1].Primary {
					t.Fatalf("primary = %v, %v", got[0].Primary, got[1].Primary)
				}
				if got[0].Fix == nil || got[0].GeoStatus != GeoApproximate {
					t.Fatalf("an unchanged address keeps its position, got %+v", got[0])
				}
			},
		},
		{
			name:   "editing the postal text resets the position to pending",
			stored: []Address{storedHome(&cepFix, GeoApproximate)},
			inputs: []AddressInput{{ID: "a-1", Label: AddressHome, Postal: workPostal}},
			check: func(t *testing.T, got []Address) {
				if got[0].Fix != nil || got[0].GeoStatus != GeoPending {
					t.Fatalf("an edited address goes back to pending, got %+v", got[0])
				}
			},
		},
		{
			name:   "a manual pin is kept when the person chose to keep it",
			stored: []Address{storedHome(&manualFix, GeoLocated)},
			inputs: []AddressInput{{ID: "a-1", Label: AddressHome, Postal: workPostal, KeepFix: true}},
			check: func(t *testing.T, got []Address) {
				if got[0].Fix == nil || *got[0].Fix != manualFix || got[0].GeoStatus != GeoLocated {
					t.Fatalf("the manual pin must stay, got %+v", got[0])
				}
			},
		},
		{
			name:   "a manual pin is dropped when the person did not keep it",
			stored: []Address{storedHome(&manualFix, GeoLocated)},
			inputs: []AddressInput{{ID: "a-1", Label: AddressHome, Postal: workPostal}},
			check: func(t *testing.T, got []Address) {
				if got[0].Fix != nil || got[0].GeoStatus != GeoPending {
					t.Fatalf("got %+v", got[0])
				}
			},
		},
		{
			name:   "keeping only applies to a confirmed pin",
			stored: []Address{storedHome(&cepFix, GeoApproximate)},
			inputs: []AddressInput{{ID: "a-1", Label: AddressHome, Postal: workPostal, KeepFix: true}},
			check: func(t *testing.T, got []Address) {
				if got[0].Fix != nil || got[0].GeoStatus != GeoPending {
					t.Fatalf("a reference position never survives an edit, got %+v", got[0])
				}
			},
		},
		{
			name:   "a new complement keeps the position",
			stored: []Address{storedHome(&cepFix, GeoApproximate)},
			inputs: []AddressInput{{ID: "a-1", Label: AddressHome, Postal: withComplement(homePostal, "apto 12")}},
			check: func(t *testing.T, got []Address) {
				if got[0].Fix == nil || got[0].Postal.Complement != "apto 12" {
					t.Fatalf("got %+v", got[0])
				}
			},
		},
		{
			name:   "an empty list removes every address",
			stored: []Address{storedHome(nil, GeoPending)},
			inputs: []AddressInput{},
			check: func(t *testing.T, got []Address) {
				if got == nil || len(got) != 0 {
					t.Fatalf("got %+v", got)
				}
			},
		},
		{
			name:      "an id that is not one of the lead's addresses is refused",
			inputs:    []AddressInput{{ID: "a-9", Label: AddressHome, Postal: homePostal}},
			wantErr:   ErrAddressUnknown,
			wantIndex: 0,
		},
		{
			name:      "an unknown label is refused",
			inputs:    []AddressInput{{Label: "summer", Postal: homePostal}},
			wantErr:   ErrAddressLabelInvalid,
			wantIndex: 0,
		},
		{
			name:      "an address without a CEP or a city and state is refused",
			inputs:    []AddressInput{{Label: AddressHome, Postal: address.Postal{Street: "Rua A"}}},
			wantErr:   address.ErrInvalidAddress,
			wantIndex: 0,
		},
		{
			name: "five addresses are the limit",
			inputs: []AddressInput{
				{Label: AddressHome, Primary: true, Postal: homePostal}, {Label: AddressWork, Postal: workPostal},
				{Label: AddressOther, Postal: homePostal}, {Label: AddressOther, Postal: workPostal},
				{Label: AddressOther, Postal: homePostal}, {Label: AddressOther, Postal: workPostal},
			},
			wantErr:   ErrAddressLimit,
			wantIndex: -1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := &Lead{WorkspaceID: "ws-1", Name: "Maria", Addresses: tc.stored}
			before := append([]Address(nil), tc.stored...)
			err := l.SetAddresses(tc.inputs)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("SetAddresses error = %v, want %v", err, tc.wantErr)
				}
				var item *ItemError
				if tc.wantIndex >= 0 && (!errors.As(err, &item) || item.Field != FieldAddresses || item.Index != tc.wantIndex) {
					t.Fatalf("SetAddresses error = %#v, want addresses[%d]", err, tc.wantIndex)
				}
				if len(l.Addresses) != len(before) {
					t.Fatalf("a refused change must leave the addresses as they were, got %+v", l.Addresses)
				}
				return
			}
			if err != nil {
				t.Fatalf("SetAddresses: %v", err)
			}
			tc.check(t, l.Addresses)
		})
	}
}

func withComplement(p address.Postal, complement string) address.Postal {
	p.Complement = complement
	return p
}

func TestSetAddressesNeverChangesTheStoredSlice(t *testing.T) {
	stored := []Address{storedHome(&cepFix, GeoApproximate)}
	l := &Lead{WorkspaceID: "ws-1", Name: "Maria", Addresses: stored}
	if err := l.SetAddresses([]AddressInput{{ID: "a-1", Label: AddressWork, Postal: workPostal}}); err != nil {
		t.Fatal(err)
	}
	if stored[0].Label != AddressHome || stored[0].Fix == nil {
		t.Fatalf("the slice the lead was loaded with must not change, got %+v", stored[0])
	}
}

func TestApplyGeocode(t *testing.T) {
	streetFix := geo.Fix{Point: geo.Point{Lat: -23.5612, Lng: -46.6562}, Precision: geo.PrecisionStreet, Source: geo.SourceReference, FixedAt: fixedAt}
	cityFix := geo.Fix{Point: geo.Point{Lat: -23.55, Lng: -46.63}, Precision: geo.PrecisionCity, Source: geo.SourceReference, FixedAt: fixedAt}
	cases := []struct {
		name        string
		current     *geo.Fix
		status      GeoStatus
		fix         geo.Fix
		stale       bool
		wantErr     error
		wantChanged bool
		wantFix     *geo.Fix
		wantStatus  GeoStatus
	}{
		{name: "a house-level fix locates a pending address", status: GeoPending, fix: streetFix, wantChanged: true, wantFix: &streetFix, wantStatus: GeoLocated},
		{name: "a city fix leaves the address approximate", status: GeoPending, fix: cityFix, wantChanged: true, wantFix: &cityFix, wantStatus: GeoApproximate},
		{name: "a better fix replaces a worse one", current: &cepFix, status: GeoApproximate, fix: streetFix, wantChanged: true, wantFix: &streetFix, wantStatus: GeoLocated},
		{name: "a worse fix changes nothing", current: &streetFix, status: GeoLocated, fix: cityFix, wantFix: &streetFix, wantStatus: GeoLocated},
		{name: "a manual pin is never replaced", current: &manualFix, status: GeoLocated, fix: streetFix, wantFix: &manualFix, wantStatus: GeoLocated},
		{name: "a fix for an older text is refused", status: GeoPending, fix: streetFix, stale: true, wantErr: ErrGeocodeStale, wantStatus: GeoPending},
		{name: "an invalid fix is refused", status: GeoPending, fix: geo.Fix{Precision: geo.PrecisionStreet, Source: geo.SourceReference}, wantErr: geo.ErrInvalidPoint, wantStatus: GeoPending},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := storedHome(tc.current, tc.status)
			fingerprint := a.Fingerprint()
			if tc.stale {
				fingerprint = workPostal.Fingerprint()
			}
			changed, err := a.ApplyGeocode(tc.fix, fingerprint)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("ApplyGeocode error = %v, want %v", err, tc.wantErr)
			}
			if changed != tc.wantChanged || a.GeoStatus != tc.wantStatus {
				t.Fatalf("changed = %v status = %s, want %v %s", changed, a.GeoStatus, tc.wantChanged, tc.wantStatus)
			}
			if (a.Fix == nil) != (tc.wantFix == nil) || (a.Fix != nil && *a.Fix != *tc.wantFix) {
				t.Fatalf("fix = %+v, want %+v", a.Fix, tc.wantFix)
			}
		})
	}
}

func TestPrimaryAddress(t *testing.T) {
	l := &Lead{Addresses: []Address{{ID: "a-1"}, {ID: "a-2", Primary: true}}}
	if got := l.PrimaryAddress(); got == nil || got.ID != "a-2" {
		t.Fatalf("PrimaryAddress = %+v", got)
	}
	if (&Lead{}).PrimaryAddress() != nil {
		t.Fatal("a lead without addresses has no primary")
	}
}

func TestValidateRecordChecksTheAddresses(t *testing.T) {
	l := &Lead{WorkspaceID: "ws-1", Name: "Maria", Addresses: []Address{
		{Label: AddressHome, Primary: true, Postal: homePostal.Normalize(), GeoStatus: GeoPending},
		{Label: AddressWork, Primary: true, Postal: workPostal.Normalize(), GeoStatus: GeoPending},
	}}
	if err := l.ValidateRecord(); !errors.Is(err, ErrAddressPrimary) {
		t.Fatalf("ValidateRecord = %v, want %v", err, ErrAddressPrimary)
	}
}

func TestAdoptPrimaryAddressOf(t *testing.T) {
	anchor := &Lead{Addresses: []Address{{ID: "a-1", Label: AddressWork, Postal: workPostal.Normalize()}, storedHome(&cepFix, GeoApproximate)}}
	anchor.Addresses[1].ID = "a-2"

	relative := &Lead{WorkspaceID: "ws-1", Name: "João", Addresses: []Address{}}
	if err := relative.AdoptPrimaryAddressOf(anchor); err != nil {
		t.Fatalf("AdoptPrimaryAddressOf: %v", err)
	}
	got := relative.Addresses
	if len(got) != 1 || got[0].ID != "" || !got[0].Primary || got[0].Label != AddressHome || got[0].Fix == nil || got[0].Fix == anchor.Addresses[1].Fix {
		t.Fatalf("the relative gets its own copy of the primary address, position included, got %+v", got)
	}

	withOwn := &Lead{WorkspaceID: "ws-1", Name: "Ana", Addresses: []Address{{Label: AddressWork, Primary: true, Postal: workPostal.Normalize()}}}
	if err := withOwn.AdoptPrimaryAddressOf(anchor); err != nil {
		t.Fatal(err)
	}
	if len(withOwn.Addresses) != 2 || withOwn.Addresses[1].Primary || !withOwn.Addresses[0].Primary {
		t.Fatalf("a copied address never takes the primary from the relative's own, got %+v", withOwn.Addresses)
	}

	if err := relative.AdoptPrimaryAddressOf(&Lead{}); !errors.Is(err, ErrNoPrimaryAddress) {
		t.Fatalf("an anchor without address = %v, want %v", err, ErrNoPrimaryAddress)
	}
}

func TestStatusOfFix(t *testing.T) {
	cases := []struct {
		precision geo.Precision
		want      GeoStatus
	}{
		{geo.PrecisionExact, GeoLocated},
		{geo.PrecisionAddress, GeoLocated},
		{geo.PrecisionStreet, GeoLocated},
		{geo.PrecisionPostalCode, GeoApproximate},
		{geo.PrecisionDistrict, GeoApproximate},
		{geo.PrecisionCity, GeoApproximate},
	}
	for _, tc := range cases {
		t.Run(string(tc.precision), func(t *testing.T) {
			if got := StatusOfFix(geo.Fix{Precision: tc.precision}); got != tc.want {
				t.Fatalf("StatusOfFix(%q) = %q, want %q", tc.precision, got, tc.want)
			}
		})
	}
}

func TestGeoStatusQueued(t *testing.T) {
	queued := map[GeoStatus]bool{GeoPending: true, GeoUnavailable: true, GeoQuotaExceeded: true}
	for _, s := range GeoStatuses() {
		if got := s.Queued(); got != queued[s] {
			t.Fatalf("%q.Queued() = %v, want %v", s, got, queued[s])
		}
	}
	if got := QueuedGeoStatuses(); len(got) != 3 || got[0] != GeoPending {
		t.Fatalf("QueuedGeoStatuses() = %v, want pending, unavailable and quota_exceeded", got)
	}
	if got := UnlocatedGeoStatuses(); len(got) != 2 || got[0] != GeoNotFound || got[1] != GeoAmbiguous {
		t.Fatalf("UnlocatedGeoStatuses() = %v, want not_found and ambiguous", got)
	}
}

func TestGeoStatusesNameEveryStatusOnceAndOnlyThoseAreValid(t *testing.T) {
	want := []GeoStatus{GeoPending, GeoLocated, GeoApproximate, GeoNotFound, GeoAmbiguous, GeoRefused, GeoUnavailable, GeoQuotaExceeded}
	got := GeoStatuses()
	if len(got) != len(want) {
		t.Fatalf("GeoStatuses() = %v, want %v", got, want)
	}
	for i, s := range want {
		if got[i] != s || !s.Valid() {
			t.Fatalf("GeoStatuses()[%d] = %q (valid %v), want %q", i, got[i], got[i].Valid(), s)
		}
	}
	if GeoStatus("provider_refused").Valid() || GeoStatus("").Valid() {
		t.Fatal("an unknown status must not be valid")
	}
}

func TestSettledGeoStatusesAreTheUnlocatedAnswersThatNoLongerWaitForAPosition(t *testing.T) {
	settled := map[GeoStatus]bool{GeoNotFound: true, GeoAmbiguous: true, GeoQuotaExceeded: true, GeoRefused: true}
	got := SettledGeoStatuses()
	for _, s := range GeoStatuses() {
		if slices.Contains(got, s) != settled[s] {
			t.Fatalf("SettledGeoStatuses() = %v, %q settled must be %v", got, s, settled[s])
		}
	}
	if len(got) != len(settled) {
		t.Fatalf("SettledGeoStatuses() = %v, want each settled status once", got)
	}
	got[0] = GeoPending
	if SettledGeoStatuses()[0] == GeoPending {
		t.Fatal("SettledGeoStatuses must hand out a fresh list")
	}
}

func TestARefusedAddressTextLeavesTheQueueAndIsNeverUnlocatedByTheProvider(t *testing.T) {
	if GeoRefused.Queued() {
		t.Fatal("a refused address text is not asked again until it changes")
	}
	for _, s := range UnlocatedGeoStatuses() {
		if s == GeoRefused {
			t.Fatal("a refused text was never searched, so it is not counted as not found")
		}
	}
}
