package lead

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"vozko/domain/address"
)

func TestDigestHidesTheNumberAndCarriesTheSummary(t *testing.T) {
	at := time.Date(2026, 9, 20, 14, 30, 0, 0, time.UTC)
	age := 34
	d := Digest(
		&Lead{ID: "l1", Name: "Maria", Number: "5584994409624", StoredAge: &age, CreatedAt: at},
		&LeadSummary{TotalCampaigns: 3, Memories: 2, LastActivityAt: &at, WhatsAppWindowOpen: true},
	)
	if d.Contact != "••••9624" {
		t.Fatalf("contact = %q", d.Contact)
	}
	if d.LeadID != "l1" || d.Name != "Maria" || *d.Age != 34 || d.Campaigns != 3 || d.Memories != 2 ||
		!d.WindowOpen || d.LastActivityAt != "2026-09-20T14:30:00Z" || d.CreatedAt != "2026-09-20T14:30:00Z" {
		t.Fatalf("digest = %+v", d)
	}
}

func TestDigestWithoutASummary(t *testing.T) {
	d := Digest(&Lead{ID: "l1", Blocked: true}, nil)
	if d.LeadID != "l1" || !d.Blocked || d.Campaigns != 0 || d.LastActivityAt != "" {
		t.Fatalf("digest = %+v", d)
	}
}

func TestDigestNeverCallsANumberAName(t *testing.T) {
	d := Digest(&Lead{ID: "l1", Name: "+55 84 99440-9624", Number: "5584994409624"}, nil)
	if d.Name != "" {
		t.Fatalf("name = %q; a name that is the lead's own number is no name", d.Name)
	}
}

func TestDigestCarriesThePlaceOfThePrimaryAddressButNeverTheStreet(t *testing.T) {
	l := &Lead{ID: "l1", Number: "5584994409624", Addresses: []Address{
		{Label: AddressWork, Postal: address.Postal{Street: "Rua do Trabalho", Number: "10", District: "Lagoa Nova", City: "Natal", State: "RN"}},
		{Label: AddressHome, Primary: true, Postal: address.Postal{ZipCode: "59000000", Street: "Rua das Flores", Number: "123", Complement: "apto 4", District: "Centro", City: "Natal", State: "RN"}},
	}}
	d := Digest(l, nil)
	if d.District != "Centro" || d.City != "Natal" || d.State != "RN" {
		t.Fatalf("digest = %+v", d)
	}
	encoded, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"Rua das Flores", "123", "apto 4", "59000000"} {
		if strings.Contains(string(encoded), leaked) {
			t.Fatalf("the digest leaks %q: %s", leaked, encoded)
		}
	}
}

func TestDigestOfAListItemNamesTheOwnerAndTheOptOut(t *testing.T) {
	at := time.Date(2026, 9, 20, 14, 30, 0, 0, time.UTC)
	item := &LeadWithSummary{
		Lead:      &Lead{ID: "l1", Number: "5584994409624", Owner: "4b1c8a52-0d9e-4f6a-9c3e-2a7d5b8e1f00", OptedOutAt: &at},
		Summary:   &LeadSummary{TotalCampaigns: 2},
		OwnerName: "Ana",
	}
	d := DigestItem(item)
	if d.Owner != "Ana" || !d.OptedOut || d.Campaigns != 2 || d.Contact != "••••9624" {
		t.Fatalf("digest = %+v", d)
	}
	if DigestItem(&LeadWithSummary{Lead: &Lead{ID: "l2"}}).OptedOut {
		t.Fatal("a lead that never opted out is not marked")
	}
}

func TestDigestNeverCarriesContactPhonesOrCustomFields(t *testing.T) {
	l := &Lead{ID: "l1", Number: "5584994409624",
		Phones:       []ContactPhone{{Number: "5584988887777", Label: PhoneLandline}},
		CustomFields: map[string]any{"posicao": "Positivo"},
	}
	encoded, err := json.Marshal(Digest(l, nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"5584988887777", "88887777", "Positivo", "posicao", "5584994409624"} {
		if strings.Contains(string(encoded), leaked) {
			t.Fatalf("the digest leaks %q: %s", leaked, encoded)
		}
	}
}
