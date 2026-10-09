package lead

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"vozko/domain/address"
	"vozko/domain/customfield"
	"vozko/domain/geo"
	"vozko/domain/shared"
)

var profileNow = time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)

func profileDefs() []*customfield.Definition {
	return []*customfield.Definition{
		{Key: "cor", ObjectType: customfield.ObjectLead, Type: customfield.TypeText},
		{Key: "interesse", ObjectType: customfield.ObjectLead, Type: customfield.TypeSelect, Options: []string{"alto", "baixo"}},
		{Key: "cliente", ObjectType: customfield.ObjectLead, Type: customfield.TypeBoolean},
		{Key: "classificacao", ObjectType: customfield.ObjectLead, Type: customfield.TypeSelect, Options: []string{"positivo", "negativo"}, Sensitive: true, LegalBasis: "consentimento"},
	}
}

func profileLead(addresses ...Address) *Lead {
	return &Lead{
		ID: "l-1", WorkspaceID: "ws-1", Number: "5511987654321", Name: "Ana", Version: 3,
		CustomFields: map[string]any{"cor": "azul"},
		Phones:       []ContactPhone{}, Addresses: append([]Address{}, addresses...), Relations: []Relation{},
	}
}

func areaOnlyHome() Address {
	return Address{ID: "a-1", Label: AddressHome, Primary: true, Postal: address.Postal{District: "Bela Vista", City: "São Paulo", State: "SP"}, GeoStatus: GeoApproximate,
		Fix: &geo.Fix{Point: geo.Point{Lat: -23.56, Lng: -46.65}, Precision: geo.PrecisionDistrict, Source: geo.SourceReference, FixedAt: fixedAt}}
}

func birth(raw string) *shared.Date {
	d, err := shared.ParseDate(raw)
	if err != nil {
		panic(err)
	}
	return &d
}

func TestApplyProfileAddress(t *testing.T) {
	cases := []struct {
		name      string
		stored    []Address
		incoming  address.Postal
		changed   []string
		conflicts []string
		check     func(t *testing.T, l *Lead)
	}{
		{
			name:     "a lead without address gets a primary home address waiting for a position",
			incoming: address.Postal{ZipCode: "01310-100", Street: "Avenida Paulista", Number: "1000", City: "São Paulo", State: "São Paulo"},
			changed:  []string{FieldAddresses},
			check: func(t *testing.T, l *Lead) {
				p := l.PrimaryAddress()
				if len(l.Addresses) != 1 || p == nil || p.Label != AddressHome || p.GeoStatus != GeoPending || p.Fix != nil {
					t.Fatalf("addresses = %+v", l.Addresses)
				}
				if p.Postal.ZipCode != "01310100" || p.Postal.State != "SP" || p.Postal.Street != "Avenida Paulista" {
					t.Fatalf("postal = %+v", p.Postal)
				}
			},
		},
		{
			name:     "an address that only had a bairro gets its street and is located again",
			stored:   []Address{areaOnlyHome()},
			incoming: address.Postal{ZipCode: "01310100", Street: "Avenida Paulista", Number: "1000", District: "bela vista", City: "Sao Paulo", State: "SP"},
			changed:  []string{FieldAddresses},
			check: func(t *testing.T, l *Lead) {
				p := l.PrimaryAddress()
				if len(l.Addresses) != 1 || p.ID != "a-1" || p.Postal.Street != "Avenida Paulista" || p.Postal.ZipCode != "01310100" || p.Postal.District != "Bela Vista" {
					t.Fatalf("primary = %+v", p)
				}
				if p.Fix != nil || p.GeoStatus != GeoPending {
					t.Fatalf("a filled address is located again, got %+v", p)
				}
			},
		},
		{
			name:     "a manual pin survives a filled address",
			stored:   []Address{{ID: "a-1", Label: AddressHome, Primary: true, Postal: address.Postal{City: "São Paulo", State: "SP"}, Fix: &manualFix, GeoStatus: GeoLocated}},
			incoming: address.Postal{District: "Bela Vista"},
			changed:  []string{FieldAddresses},
			check: func(t *testing.T, l *Lead) {
				p := l.PrimaryAddress()
				if p.Postal.District != "Bela Vista" || p.Fix == nil || p.Fix.Source != geo.SourceManual || p.GeoStatus != GeoLocated {
					t.Fatalf("primary = %+v", p)
				}
			},
		},
		{
			name:     "the same address written another way changes nothing",
			stored:   []Address{storedHome(&cepFix, GeoApproximate)},
			incoming: address.Postal{ZipCode: "01310100", Street: "avenida paulista", City: "SAO PAULO", State: "São Paulo"},
			check: func(t *testing.T, l *Lead) {
				if !reflect.DeepEqual(l.Addresses, []Address{storedHome(&cepFix, GeoApproximate)}) {
					t.Fatalf("addresses = %+v", l.Addresses)
				}
			},
		},
		{
			name:      "a different street is a conflict and the stored address stays",
			stored:    []Address{storedHome(&manualFix, GeoLocated)},
			incoming:  address.Postal{Street: "Rua Augusta", Number: "500"},
			conflicts: []string{FieldAddresses},
			check: func(t *testing.T, l *Lead) {
				if !reflect.DeepEqual(l.Addresses, []Address{storedHome(&manualFix, GeoLocated)}) {
					t.Fatalf("addresses = %+v", l.Addresses)
				}
			},
		},
		{
			name:      "one conflicting part keeps the whole address as it was",
			stored:    []Address{areaOnlyHome()},
			incoming:  address.Postal{Street: "Rua Augusta", District: "Consolação"},
			conflicts: []string{FieldAddresses},
			check: func(t *testing.T, l *Lead) {
				if !reflect.DeepEqual(l.Addresses, []Address{areaOnlyHome()}) {
					t.Fatalf("addresses = %+v", l.Addresses)
				}
			},
		},
		{
			name:     "a bairro is compared with the primary address only, never the others",
			stored:   []Address{{ID: "a-1", Label: AddressHome, Primary: true, Postal: workPostal.Normalize(), GeoStatus: GeoPending}, {ID: "a-2", Label: AddressWork, Postal: address.Postal{City: "Santos", State: "SP"}, GeoStatus: GeoPending}},
			incoming: address.Postal{District: "Gonzaga"},
			check: func(t *testing.T, l *Lead) {
				if l.Addresses[1].Postal.District != "" {
					t.Fatalf("only the primary address is completed, got %+v", l.Addresses[1])
				}
			},
			conflicts: []string{FieldAddresses},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := profileLead(tc.stored...)
			got, err := l.ApplyProfile(Profile{Address: tc.incoming}, profileDefs(), profileNow)
			if err != nil {
				t.Fatalf("ApplyProfile: %v", err)
			}
			if !reflect.DeepEqual(got.Changed, tc.changed) || !reflect.DeepEqual(got.Conflicts, tc.conflicts) {
				t.Fatalf("outcome = %+v, want changed %v conflicts %v", got, tc.changed, tc.conflicts)
			}
			tc.check(t, l)
		})
	}
}

func TestApplyProfileRefusesAnAddressItCannotStore(t *testing.T) {
	cases := []struct {
		name     string
		stored   []Address
		incoming address.Postal
	}{
		{"a bairro without a city for a lead without address", nil, address.Postal{District: "Bela Vista"}},
		{"a malformed CEP", nil, address.Postal{ZipCode: "0131", City: "São Paulo", State: "SP"}},
		{"a malformed CEP over a stored address", []Address{areaOnlyHome()}, address.Postal{ZipCode: "0131"}},
		{"an unknown state", nil, address.Postal{City: "Buenos Aires", State: "Buenos Aires"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := profileLead(tc.stored...)
			before := profileLead(tc.stored...)
			if _, err := l.ApplyProfile(Profile{Address: tc.incoming}, profileDefs(), profileNow); !errors.Is(err, address.ErrInvalidAddress) {
				t.Fatalf("err = %v, want an invalid address", err)
			}
			if !reflect.DeepEqual(l, before) {
				t.Fatalf("a refused profile must leave the lead untouched, got %+v", l)
			}
		})
	}
}

func TestApplyProfileBirthDate(t *testing.T) {
	cases := []struct {
		name      string
		stored    *shared.Date
		incoming  string
		want      *shared.Date
		changed   []string
		conflicts []string
		err       error
	}{
		{name: "fills an empty birth date written the Brazilian way", incoming: "15/03/1980", want: birth("1980-03-15"), changed: []string{FieldBirthDate}},
		{name: "the same date changes nothing", stored: birth("1980-03-15"), incoming: "1980-03-15", want: birth("1980-03-15")},
		{name: "a different date is a conflict", stored: birth("1980-03-15"), incoming: "16/03/1980", want: birth("1980-03-15"), conflicts: []string{FieldBirthDate}},
		{name: "a date in the future is refused", incoming: "01/01/2030", err: ErrLeadBirthDateInvalid},
		{name: "a date too far back is refused", incoming: "01/01/1850", err: ErrLeadBirthDateInvalid},
		{name: "text that is not a date is refused", incoming: "março de 80", err: ErrLeadBirthDateInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := profileLead()
			l.BirthDate = tc.stored
			got, err := l.ApplyProfile(Profile{BirthDate: tc.incoming}, profileDefs(), profileNow)
			if tc.err != nil {
				if !errors.Is(err, tc.err) {
					t.Fatalf("err = %v, want %v", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ApplyProfile: %v", err)
			}
			if !reflect.DeepEqual(l.BirthDate, tc.want) || !reflect.DeepEqual(got.Changed, tc.changed) || !reflect.DeepEqual(got.Conflicts, tc.conflicts) {
				t.Fatalf("birth = %v outcome = %+v", l.BirthDate, got)
			}
		})
	}
}

func TestApplyProfileCustomFields(t *testing.T) {
	cases := []struct {
		name      string
		incoming  map[string]string
		want      map[string]any
		changed   []string
		conflicts []string
		err       error
	}{
		{
			name:     "fills empty fields with values of their type",
			incoming: map[string]string{"interesse": "alto", "cliente": "sim", "cor": " "},
			want:     map[string]any{"cor": "azul", "interesse": "alto", "cliente": true},
			changed:  []string{CustomFieldName("cliente"), CustomFieldName("interesse")},
		},
		{
			name:      "a filled field with another value is a conflict and keeps its value",
			incoming:  map[string]string{"cor": "verde", "interesse": "baixo"},
			want:      map[string]any{"cor": "azul", "interesse": "baixo"},
			changed:   []string{CustomFieldName("interesse")},
			conflicts: []string{CustomFieldName("cor")},
		},
		{
			name:     "the same value changes nothing",
			incoming: map[string]string{"cor": "azul"},
			want:     map[string]any{"cor": "azul"},
		},
		{name: "an unknown field is refused", incoming: map[string]string{"time": "Santos"}, err: customfield.ErrUnknownKey},
		{name: "a sensitive field is never written by an automation", incoming: map[string]string{"classificacao": "positivo"}, err: customfield.ErrValueForbidden},
		{name: "a value outside the options is refused", incoming: map[string]string{"interesse": "médio"}, err: customfield.ErrValueNotInOptions},
		{name: "a value of the wrong type is refused", incoming: map[string]string{"cliente": "talvez"}, err: customfield.ErrValueType},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := profileLead()
			got, err := l.ApplyProfile(Profile{CustomFields: tc.incoming}, profileDefs(), profileNow)
			if tc.err != nil {
				if !errors.Is(err, tc.err) {
					t.Fatalf("err = %v, want %v", err, tc.err)
				}
				if !reflect.DeepEqual(l.CustomFields, map[string]any{"cor": "azul"}) {
					t.Fatalf("a refused profile must leave the fields untouched, got %v", l.CustomFields)
				}
				return
			}
			if err != nil {
				t.Fatalf("ApplyProfile: %v", err)
			}
			if !reflect.DeepEqual(l.CustomFields, tc.want) || !reflect.DeepEqual(got.Changed, tc.changed) || !reflect.DeepEqual(got.Conflicts, tc.conflicts) {
				t.Fatalf("fields = %v outcome = %+v", l.CustomFields, got)
			}
		})
	}
}

func TestApplyProfileIsAllOrNothing(t *testing.T) {
	l := profileLead()
	before := profileLead()
	_, err := l.ApplyProfile(Profile{
		Address:      address.Postal{ZipCode: "01310100", City: "São Paulo", State: "SP"},
		BirthDate:    "15/03/1980",
		CustomFields: map[string]string{"interesse": "médio"},
	}, profileDefs(), profileNow)
	if !errors.Is(err, customfield.ErrValueNotInOptions) {
		t.Fatalf("err = %v", err)
	}
	if !reflect.DeepEqual(l, before) {
		t.Fatalf("nothing may be applied when one part is refused, got %+v", l)
	}
}

func TestApplyProfileRefuses(t *testing.T) {
	partial := profileLead()
	partial.Addresses = nil
	cases := []struct {
		name    string
		lead    *Lead
		profile Profile
		want    error
	}{
		{"an empty profile", profileLead(), Profile{CustomFields: map[string]string{"cor": " "}}, ErrProfileEmpty},
		{"a lead read without its addresses", partial, Profile{BirthDate: "15/03/1980"}, ErrAggregateNotLoaded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.lead.ApplyProfile(tc.profile, profileDefs(), profileNow); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestProfileSource(t *testing.T) {
	for _, s := range []ProfileSource{ProfileFromConversation, ProfileFromWorkflow, ProfileFromForm} {
		if !s.Valid() {
			t.Errorf("%q must be valid", s)
		}
		if got := string(s.EventKind()); got != "profile_from_"+string(s) || len(got) > 32 {
			t.Errorf("event kind of %q = %q", s, got)
		}
	}
	for _, s := range []ProfileSource{"", "manual", "import"} {
		if s.Valid() {
			t.Errorf("%q must not be a profile source", s)
		}
	}
}

func TestProfileEmpty(t *testing.T) {
	if !(Profile{CustomFields: map[string]string{"cor": ""}, BirthDate: " ", Address: address.Postal{Street: " "}}).Empty() {
		t.Fatal("blank values are not a profile")
	}
	if (Profile{Address: address.Postal{District: "Centro"}}).Empty() {
		t.Fatal("a bairro is a profile")
	}
}

func TestProfileErrorCodes(t *testing.T) {
	cases := []struct {
		err   error
		want  string
		input bool
	}{
		{ErrProfileEmpty, "lead_profile_empty", true},
		{ErrProfileSourceInvalid, "lead_profile_source_invalid", false},
		{ErrProfileCEPUnknown, "lead_cep_not_found", true},
		{ErrProfileCEPUnchecked, "lead_cep_unavailable", false},
		{address.ErrCEPMismatch, "lead_address_cep_mismatch", true},
	}
	for _, tc := range cases {
		if got := ErrorCode(tc.err); got != tc.want {
			t.Errorf("ErrorCode(%v) = %q, want %q", tc.err, got, tc.want)
		}
		if IsInputRefusal(tc.err) != tc.input {
			t.Errorf("IsInputRefusal(%v) = %v, want %v", tc.err, !tc.input, tc.input)
		}
	}
}

func TestProfileWritableFieldsLeaveOutSensitiveFields(t *testing.T) {
	got := ProfileWritableFields(append(profileDefs(), nil))
	keys := make([]string, 0, len(got))
	for _, def := range got {
		keys = append(keys, def.Key)
	}
	if !reflect.DeepEqual(keys, []string{"cor", "interesse", "cliente"}) {
		t.Fatalf("keys = %v", keys)
	}
}

func TestApplyProfileRefusesANumberThatIsNotFinite(t *testing.T) {
	defs := append(profileDefs(), &customfield.Definition{Key: "renda", ObjectType: customfield.ObjectLead, Type: customfield.TypeNumber})
	for _, raw := range []string{"NaN", "inf", "-Infinity"} {
		l := profileLead()
		if _, err := l.ApplyProfile(Profile{CustomFields: map[string]string{"renda": raw}}, defs, profileNow); !errors.Is(err, customfield.ErrValueType) {
			t.Fatalf("renda %q: err = %v, want %v", raw, err, customfield.ErrValueType)
		}
		if _, written := l.CustomFields["renda"]; written {
			t.Fatalf("renda %q was written: %v", raw, l.CustomFields)
		}
	}
}
