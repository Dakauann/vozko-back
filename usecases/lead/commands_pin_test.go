package lead_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/address"
	"vozko/domain/geo"
	"vozko/domain/lead"
)

var (
	draftPin    = geo.Point{Lat: -8.0476, Lng: -34.8770}
	draftPostal = address.Postal{ZipCode: "50030-230", Street: "Rua da Aurora", Number: "10", District: "Boa Vista", City: "Recife", State: "PE"}
)

func pinPermissions() fakePermissions {
	perms := allPermissions()
	perms["leads:read_addresses"] = true
	return perms
}

func TestCreateStoresThePinChosenBeforeSavingAsAManualPosition(t *testing.T) {
	f := newCommandsFixture(t, pinPermissions())
	created, err := f.cmds.Create(context.Background(), operator(), lead.Draft{Name: "Maria", Addresses: []lead.AddressInput{{Label: lead.AddressHome, Postal: draftPostal, Pin: &draftPin}}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got := created.Lead.Addresses[0]
	if got.Fix == nil || got.Fix.Point != draftPin || got.Fix.Source != geo.SourceManual || got.Fix.Precision != geo.PrecisionExact || got.GeoStatus != lead.GeoLocated {
		t.Fatalf("address = %+v, want the manual pin located", got)
	}
	if got.Fix.FixedAt.IsZero() {
		t.Fatal("the server stamps the pin time")
	}
}

func TestCreateWithAPinNeedsThePinPermissions(t *testing.T) {
	for name, perms := range map[string]fakePermissions{
		"without full addresses": allPermissions(),
		"without update":         {"leads:read": true, "leads:create": true, "leads:read_addresses": true},
	} {
		t.Run(name, func(t *testing.T) {
			f := newCommandsFixture(t, perms)
			_, err := f.cmds.Create(context.Background(), operator(), lead.Draft{Name: "Maria", Addresses: []lead.AddressInput{{Label: lead.AddressHome, Postal: draftPostal, Pin: &draftPin}}})
			if !errors.Is(err, lead.ErrLeadForbidden) && !errors.Is(err, lead.ErrAddressesForbidden) {
				t.Fatalf("Create() err = %v, want a refusal", err)
			}
			if len(f.store.saves) != 0 {
				t.Fatal("a refused create must not write")
			}
		})
	}
}

func TestCreateWithoutAPinKeepsItsOwnPermissions(t *testing.T) {
	f := newCommandsFixture(t, fakePermissions{"leads:read": true, "leads:create": true})
	if _, err := f.cmds.Create(context.Background(), operator(), lead.Draft{Name: "Maria", Addresses: []lead.AddressInput{{Label: lead.AddressHome, Postal: draftPostal}}}); err != nil {
		t.Fatalf("Create() err = %v, want an address without a pin to need only create", err)
	}
}

func TestUpdateStoresThePinInTheSameCommand(t *testing.T) {
	f := newCommandsFixture(t, pinPermissions(), storedLead())
	inputs := []lead.AddressInput{{Label: lead.AddressHome, Postal: draftPostal, Pin: &draftPin}}
	updated, err := f.cmds.Update(context.Background(), operator(), "l-1", version(3), lead.Edit{Addresses: &inputs})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	got := updated.Addresses[0]
	if got.Fix == nil || got.Fix.Point != draftPin || got.Fix.Source != geo.SourceManual || got.GeoStatus != lead.GeoLocated {
		t.Fatalf("address = %+v, want the manual pin", got)
	}
	if len(f.store.saves) != 1 {
		t.Fatalf("saves = %d, want one write for the text and the pin", len(f.store.saves))
	}
}

func TestUpdateRefusesAPinOutsideBrazil(t *testing.T) {
	f := newCommandsFixture(t, pinPermissions(), storedLead())
	lisbon := geo.Point{Lat: 38.7, Lng: -9.1}
	inputs := []lead.AddressInput{{Label: lead.AddressHome, Postal: draftPostal, Pin: &lisbon}}
	if _, err := f.cmds.Update(context.Background(), operator(), "l-1", version(3), lead.Edit{Addresses: &inputs}); !errors.Is(err, lead.ErrLocationInvalid) {
		t.Fatalf("Update() err = %v, want ErrLocationInvalid", err)
	}
	if len(f.store.saves) != 0 {
		t.Fatal("a refused pin must not write")
	}
}
