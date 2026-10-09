package lead

import (
	"errors"
	"reflect"
	"testing"
)

func TestDuplicateCandidates(t *testing.T) {
	home := Address{Label: AddressHome, Primary: true, Postal: homePostal.Normalize()}
	newcomer := &Lead{
		Name:      "Maria  Souza",
		Phones:    []ContactPhone{{Number: "551133334444", Label: PhoneLandline}, {Number: "5511912345678", Label: PhoneMobile}},
		Addresses: []Address{home},
	}
	cases := []struct {
		name     string
		existing []*Lead
		want     []DuplicateCandidate
	}{
		{
			name:     "a lead with the same contact phone",
			existing: []*Lead{{ID: "a", Name: "João", Phones: []ContactPhone{{Number: "551133334444"}}}},
			want:     []DuplicateCandidate{{LeadID: "a", Reasons: []DuplicateReason{DuplicateSharedPhone}}},
		},
		{
			name:     "a lead whose WhatsApp number is one of the contact phones, in the other mobile format",
			existing: []*Lead{{ID: "b", Number: "551112345678"}},
			want:     []DuplicateCandidate{{LeadID: "b", Reasons: []DuplicateReason{DuplicateSharedPhone}}},
		},
		{
			name:     "the same real name at the same address",
			existing: []*Lead{{ID: "c", Name: "maria souza", Addresses: []Address{{Postal: withComplement(homePostal, "fundos").Normalize()}}}},
			want:     []DuplicateCandidate{{LeadID: "c", Reasons: []DuplicateReason{DuplicateSameNameAndAddress}}},
		},
		{
			name: "both reasons on one lead",
			existing: []*Lead{{ID: "d", Name: "MARIA SOUZA", Phones: []ContactPhone{{Number: "5511912345678"}},
				Addresses: []Address{home}}},
			want: []DuplicateCandidate{{LeadID: "d", Reasons: []DuplicateReason{DuplicateSharedPhone, DuplicateSameNameAndAddress}}},
		},
		{
			name:     "the same name somewhere else is not a duplicate",
			existing: []*Lead{{ID: "e", Name: "Maria Souza", Addresses: []Address{{Postal: workPostal.Normalize()}}}},
			want:     nil,
		},
		{
			name:     "another person at the same address is not a duplicate",
			existing: []*Lead{{ID: "f", Name: "Ana Souza", Addresses: []Address{home}}},
			want:     nil,
		},
		{
			name:     "a lead named after its own number never matches by name",
			existing: []*Lead{{ID: "g", Number: "5511900000000", Name: "5511900000000", Addresses: []Address{home}}},
			want:     nil,
		},
		{
			name:     "the lead itself is never its own duplicate",
			existing: []*Lead{{ID: "self", Name: "Maria Souza", Addresses: []Address{home}}},
			want:     nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := *newcomer
			candidate.ID = "self"
			if got := DuplicateCandidates(addressReader, &candidate, tc.existing); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("DuplicateCandidates = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestDuplicateCandidatesOfALeadWithoutNameOrPhonesIsEmpty(t *testing.T) {
	if got := DuplicateCandidates(addressReader, &Lead{Number: "5511987654321"}, []*Lead{{ID: "x", Name: "Ana"}}); got != nil {
		t.Fatalf("got %+v", got)
	}
}

var addressReader = Viewer{ReadsLeads: true, ReadsAddresses: true}

func TestDuplicatesByAddressAreOnlyForWhoReadsAddresses(t *testing.T) {
	home := Address{Label: AddressHome, Primary: true, Postal: homePostal.Normalize()}
	newcomer := &Lead{ID: "self", Name: "Maria Souza", Phones: []ContactPhone{{Number: "551133334444", Label: PhoneLandline}}, Addresses: []Address{home}}
	existing := []*Lead{
		{ID: "neighbour", Name: "Maria Souza", Addresses: []Address{home}},
		{ID: "family", Name: "João", Phones: []ContactPhone{{Number: "551133334444"}}, Addresses: []Address{home}},
	}
	areaOnly := Viewer{ReadsLeads: true}

	numbers, fingerprints := newcomer.DuplicateLookup(areaOnly)
	if !reflect.DeepEqual(numbers, []string{"551133334444"}) || len(fingerprints) != 0 {
		t.Fatalf("lookup without the address permission = %v, %v", numbers, fingerprints)
	}
	if _, prints := newcomer.DuplicateLookup(addressReader); len(prints) != 1 {
		t.Fatalf("lookup with the address permission = %v", prints)
	}
	want := []DuplicateCandidate{{LeadID: "family", Reasons: []DuplicateReason{DuplicateSharedPhone}}}
	if got := DuplicateCandidates(areaOnly, newcomer, existing); !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates without the address permission = %+v, want %+v", got, want)
	}
	if got := DuplicateCandidates(addressReader, newcomer, existing); len(got) != 2 {
		t.Fatalf("candidates with the address permission = %+v", got)
	}
}

func TestIdentityHolder(t *testing.T) {
	newcomer := &Lead{Number: "5511987654321"}
	holder := &Lead{ID: "a", Number: "551187654321"}
	if got := IdentityHolder(newcomer, []*Lead{{ID: "b", Phones: []ContactPhone{{Number: "5511987654321"}}}, holder}); got != holder {
		t.Fatalf("IdentityHolder = %+v, want the lead whose identity is the same number", got)
	}
	if got := IdentityHolder(&Lead{}, []*Lead{holder}); got != nil {
		t.Fatalf("a lead without identity has no identity holder, got %+v", got)
	}
}

func TestPromotionCandidate(t *testing.T) {
	holder := &Lead{ID: "a", Name: "Maria", Phones: []ContactPhone{{ID: "p-1", Number: "551187654321", Label: PhoneMobile}}}
	another := &Lead{ID: "b", Name: "João", Phones: []ContactPhone{{Number: "5511987654321", Label: PhoneMobile}}}
	withIdentity := &Lead{ID: "c", Number: "5511900000000", Phones: []ContactPhone{{Number: "5511987654321", Label: PhoneMobile}}}
	cases := []struct {
		name    string
		holders []*Lead
		want    *Lead
	}{
		{"exactly one identity-less lead holds the number", []*Lead{holder, withIdentity}, holder},
		{"two identity-less leads hold the number", []*Lead{holder, another}, nil},
		{"only leads with an identity hold the number", []*Lead{withIdentity}, nil},
		{"nobody holds the number", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PromotionCandidate("5511987654321", tc.holders); got != tc.want {
				t.Fatalf("PromotionCandidate = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestPromoteContactPhone(t *testing.T) {
	l := &Lead{ID: "a", WorkspaceID: "ws-1", Name: "Maria", Phones: []ContactPhone{
		{ID: "p-1", Number: "551187654321", Label: PhoneMobile},
		{ID: "p-2", Number: "551133334444", Label: PhoneLandline},
	}}
	stored := l.Phones
	if err := l.PromoteContactPhone("5511987654321"); err != nil {
		t.Fatalf("PromoteContactPhone: %v", err)
	}
	if l.Number != "5511987654321" || len(l.Phones) != 1 || l.Phones[0].ID != "p-2" {
		t.Fatalf("promoted lead = %+v", l)
	}
	if len(stored) != 2 {
		t.Fatalf("the loaded slice must not change, got %+v", stored)
	}
	if err := l.PromoteContactPhone("5511987654321"); !errors.Is(err, ErrIdentityInUse) {
		t.Fatalf("a lead that already has an identity is not promoted again, got %v", err)
	}
	if err := (&Lead{WorkspaceID: "ws-1", Name: "Ana"}).PromoteContactPhone("5511987654321"); !errors.Is(err, ErrPhoneUnknown) {
		t.Fatalf("a number the lead does not hold is not promoted, got %v", err)
	}
}

func TestChangeIdentity(t *testing.T) {
	cases := []struct {
		name       string
		current    string
		phones     []ContactPhone
		raw        string
		inUse      bool
		want       string
		wantPhones int
		changed    bool
		wantErr    error
	}{
		{name: "a lead without conversations gets a new number", current: "5511987654321", raw: "(21) 99876-5432", want: "5521998765432", changed: true},
		{name: "a lead without identity gets one", raw: "5521998765432", want: "5521998765432", changed: true},
		{name: "the same number in another format changes nothing", current: "5511987654321", raw: "551187654321", want: "5511987654321"},
		{name: "a lead with conversations keeps its number", current: "5511987654321", raw: "5521998765432", inUse: true, want: "5511987654321", wantErr: ErrIdentityInUse},
		{name: "a lead with conversations keeps its number even to clear it", current: "5511987654321", raw: "", inUse: true, want: "5511987654321", wantErr: ErrIdentityInUse},
		{name: "the number can be cleared when the lead has a name", current: "5511987654321", raw: "", want: "", changed: true},
		{name: "an invalid number is refused", current: "5511987654321", raw: "123", want: "5511987654321", wantErr: ErrLeadInvalid},
		{
			name: "a contact phone that becomes the identity leaves the contact phones", current: "5511987654321",
			phones: []ContactPhone{{ID: "p-1", Number: "5521998765432", Label: PhoneWork}, {ID: "p-2", Number: "551133334444", Label: PhoneLandline}},
			raw:    "5521998765432", want: "5521998765432", wantPhones: 1, changed: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := &Lead{WorkspaceID: "ws-1", Name: "Maria", Number: tc.current, Phones: tc.phones}
			changed, err := l.ChangeIdentity(tc.raw, tc.inUse)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("ChangeIdentity error = %v, want %v", err, tc.wantErr)
			}
			if changed != tc.changed || l.Number != tc.want || len(l.Phones) != tc.wantPhones {
				t.Fatalf("changed = %v number = %q phones = %+v", changed, l.Number, l.Phones)
			}
		})
	}
}

func TestChangeIdentityNeedsANameToClearTheNumber(t *testing.T) {
	l := &Lead{WorkspaceID: "ws-1", Number: "5511987654321"}
	if _, err := l.ChangeIdentity("", false); !errors.Is(err, ErrLeadIdentityRequired) {
		t.Fatalf("ChangeIdentity = %v, want %v", err, ErrLeadIdentityRequired)
	}
	if l.Number != "5511987654321" {
		t.Fatalf("a refused change keeps the number, got %q", l.Number)
	}
}

func TestWouldChangeIdentity(t *testing.T) {
	l := &Lead{Number: "5511987654321"}
	cases := []struct {
		raw  string
		want bool
	}{
		{"5511987654321", false},
		{"(11) 8765-4321", false},
		{"5521998765432", true},
		{"", true},
		{"123", true},
	}
	for _, tc := range cases {
		if got := l.WouldChangeIdentity(tc.raw); got != tc.want {
			t.Errorf("WouldChangeIdentity(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
	if (&Lead{}).WouldChangeIdentity("  ") {
		t.Fatal("clearing an absent number changes nothing")
	}
}
