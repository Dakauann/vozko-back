package lead

import (
	"errors"
	"testing"

	"vozko/domain/address"
	"vozko/domain/customfield"
	"vozko/domain/geo"
)

func TestSetPrimaryArea(t *testing.T) {
	work := Address{ID: "a-2", Label: AddressWork, Postal: workPostal.Normalize(), GeoStatus: GeoPending}
	cases := []struct {
		name      string
		stored    []Address
		area      Area
		changed   bool
		wantErr   error
		check     func(t *testing.T, l *Lead)
		untouched bool
	}{
		{
			name:    "a lead without address gets a primary home address with the area",
			area:    Area{District: " Vila  Mariana ", City: "São Paulo", State: "sp"},
			changed: true,
			check: func(t *testing.T, l *Lead) {
				p := l.PrimaryAddress()
				if len(l.Addresses) != 1 || p == nil || p.Label != AddressHome || p.Postal.District != "Vila Mariana" || p.Postal.State != "SP" || p.GeoStatus != GeoPending {
					t.Fatalf("addresses = %+v", l.Addresses)
				}
			},
		},
		{
			name:    "the primary address keeps its street and number and is located again",
			stored:  []Address{storedHome(&cepFix, GeoApproximate), work},
			area:    Area{District: "Jardim Paulista", City: "São Paulo", State: "SP"},
			changed: true,
			check: func(t *testing.T, l *Lead) {
				p := l.PrimaryAddress()
				if p.ID != "a-1" || p.Postal.Street != "Avenida Paulista" || p.Postal.District != "Jardim Paulista" || p.Fix != nil || p.GeoStatus != GeoPending {
					t.Fatalf("primary = %+v", p)
				}
				if l.Addresses[1].ID != "a-2" || l.Addresses[1].Postal != work.Postal {
					t.Fatalf("the other address must not change, got %+v", l.Addresses[1])
				}
			},
		},
		{
			name:    "a manual pin survives a new bairro",
			stored:  []Address{storedHome(&manualFix, GeoLocated)},
			area:    Area{District: "Jardim Paulista", City: "São Paulo", State: "SP"},
			changed: true,
			check: func(t *testing.T, l *Lead) {
				if p := l.PrimaryAddress(); p.Fix == nil || p.Fix.Source != geo.SourceManual {
					t.Fatalf("primary = %+v", p)
				}
			},
		},
		{
			name:      "the same area changes nothing",
			stored:    []Address{storedHome(&cepFix, GeoApproximate)},
			area:      Area{District: " Bela  Vista", City: "São Paulo", State: "sp"},
			untouched: true,
		},
		{
			name:      "an area without a city and a state cannot stand alone",
			area:      Area{District: "Centro"},
			wantErr:   address.ErrInvalidAddress,
			untouched: true,
		},
		{
			name:      "an unknown state is refused",
			stored:    []Address{storedHome(nil, GeoPending)},
			area:      Area{District: "Centro", City: "São Paulo", State: "XX"},
			wantErr:   address.ErrInvalidAddress,
			untouched: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := &Lead{ID: "l-1", WorkspaceID: "ws-1", Name: "Maria", Phones: []ContactPhone{}, Addresses: append([]Address{}, tc.stored...)}
			before := append([]Address{}, l.Addresses...)
			changed, err := l.SetPrimaryArea(tc.area)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("SetPrimaryArea error = %v, want %v", err, tc.wantErr)
			}
			if changed != tc.changed {
				t.Fatalf("changed = %v, want %v", changed, tc.changed)
			}
			if tc.untouched && len(l.Addresses) != len(before) {
				t.Fatalf("addresses changed: %+v", l.Addresses)
			}
			if tc.check != nil {
				tc.check(t, l)
			}
		})
	}
}

func TestCardOfShowsTheAreaAndOnlyVisibleFields(t *testing.T) {
	l := recordWithEverything()
	l.Owner, l.RelativesCount, l.ReferredCount, l.Blocked = "u-2", 3, 1, true
	inbox := Viewer{Definitions: leadFieldDefs()}
	card := CardOf(l, inbox)
	if card.LeadID != "l-1" || card.Version != 4 || card.Name != "Maria" || card.Number != "5511987654321" || !card.Blocked {
		t.Fatalf("card = %+v", card)
	}
	if card.Owner != "u-2" || card.RelativesCount != 3 || card.ReferredCount != 1 {
		t.Fatalf("card = %+v", card)
	}
	if card.Area == nil || *card.Area != (Area{District: "Bela Vista", City: "São Paulo", State: "SP"}) {
		t.Fatalf("area = %+v", card.Area)
	}
	if len(card.CustomFields) != 1 || card.CustomFields["interesse"] != "alto" {
		t.Fatalf("custom fields = %v; sensitive values need the permission", card.CustomFields)
	}
	sensitive := CardOf(l, Viewer{Fields: customfield.Viewer{ReadsSensitive: true}, Definitions: leadFieldDefs()})
	if sensitive.CustomFields["classificacao"] != "positivo" {
		t.Fatalf("custom fields = %v", sensitive.CustomFields)
	}
	if none := CardOf(&Lead{ID: "l-2", Name: "Ana"}, inbox); none.Area != nil || none.CustomFields != nil {
		t.Fatalf("card = %+v", none)
	}
}
