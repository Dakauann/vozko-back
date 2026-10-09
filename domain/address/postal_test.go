package address

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func paulista() Postal {
	return Postal{
		ZipCode:  "01310100",
		Street:   "Avenida Paulista",
		Number:   "1000",
		District: "Bela Vista",
		City:     "São Paulo",
		State:    "SP",
		CityCode: "3550308",
	}
}

func TestAddressKeepsTheBillingAPIJSON(t *testing.T) {
	at := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	addr := Address{
		ID:        "a1",
		UserID:    "u1",
		Name:      "Casa",
		Postal:    Postal{ZipCode: "01310100", Street: "Avenida Paulista", Number: "1000", Complement: "ap 12", District: "Bela Vista", City: "São Paulo", State: "SP"},
		IsDefault: true,
		CreatedAt: at,
		UpdatedAt: at,
	}
	raw, err := json.Marshal(addr)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	expected := map[string]any{
		"id": "a1", "userId": "u1", "name": "Casa",
		"street": "Avenida Paulista", "number": "1000", "complement": "ap 12",
		"district": "Bela Vista", "city": "São Paulo", "state": "SP", "zipCode": "01310100",
		"isDefault": true, "createdAt": "2026-10-08T12:00:00Z", "updatedAt": "2026-10-08T12:00:00Z",
	}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("billing address JSON changed:\n got %v\nwant %v", got, expected)
	}

	var back Address
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, addr) {
		t.Fatalf("round trip lost data:\n got %+v\nwant %+v", back, addr)
	}
}

func TestPostalNormalize(t *testing.T) {
	got := Postal{
		ZipCode:    " 01310-100 ",
		Street:     "  Avenida   Paulista ",
		Number:     " 1000 ",
		Complement: " ap  12 ",
		District:   " Bela  Vista",
		City:       "São   Paulo ",
		State:      " sp ",
		CityCode:   " 3550308 ",
	}.Normalize()
	expected := Postal{ZipCode: "01310100", Street: "Avenida Paulista", Number: "1000", Complement: "ap 12", District: "Bela Vista", City: "São Paulo", State: "SP", CityCode: "3550308"}
	if got != expected {
		t.Fatalf("Normalize() = %+v, expected %+v", got, expected)
	}
	if kept := (Postal{ZipCode: " 0131 "}).Normalize().ZipCode; kept != "0131" {
		t.Fatalf("an unreadable CEP is kept for Validate to report, got %q", kept)
	}
}

func TestPostalNormalizeTurnsAStateNameIntoItsCode(t *testing.T) {
	cases := []struct {
		state string
		want  string
	}{
		{"São Paulo", "SP"},
		{" sao   paulo ", "SP"},
		{"Rio Grande do Sul", "RS"},
		{"mg", "MG"},
		{"Atlântida", "ATLÂNTIDA"},
	}
	for _, tc := range cases {
		t.Run(tc.state, func(t *testing.T) {
			got := Postal{City: "Cidade", State: tc.state}.Normalize()
			if got.State != tc.want {
				t.Fatalf("Normalize().State = %q, want %q", got.State, tc.want)
			}
			if again := got.Normalize(); again != got {
				t.Fatalf("Normalize is not stable: %+v then %+v", got, again)
			}
		})
	}
	if err := (Postal{City: "São Paulo", State: "São Paulo"}).Validate(); err != nil {
		t.Fatalf("a state written by name is a valid state, got %v", err)
	}
}

func TestPostalValidate(t *testing.T) {
	tests := []struct {
		name   string
		postal Postal
		field  Field
		rule   Rule
	}{
		{"accepts a full address", paulista(), "", ""},
		{"accepts only a CEP", Postal{ZipCode: "01310-100"}, "", ""},
		{"accepts only a city with its state", Postal{City: "Contagem", State: "MG"}, "", ""},
		{"accepts a lowercase state", Postal{City: "Contagem", State: "mg"}, "", ""},
		{"refuses an empty address", Postal{}, FieldZipCode, RuleZipOrCityRequired},
		{"refuses a city without its state", Postal{City: "Contagem"}, FieldZipCode, RuleZipOrCityRequired},
		{"refuses a street alone", Postal{Street: "Rua A", Number: "1"}, FieldZipCode, RuleZipOrCityRequired},
		{"refuses a CEP with seven digits", Postal{ZipCode: "0131010"}, FieldZipCode, RuleFormat},
		{"refuses a state outside the 27", Postal{City: "Contagem", State: "XX"}, FieldState, RuleUnknownState},
		{"refuses a city code that is not 7 digits", Postal{ZipCode: "01310100", CityCode: "35503"}, FieldCityCode, RuleFormat},
		{"refuses a street over the cap", Postal{ZipCode: "01310100", Street: strings.Repeat("a", MaxStreetLength+1)}, FieldStreet, RuleTooLong},
		{"counts characters, not bytes", Postal{ZipCode: "01310100", Street: strings.Repeat("ã", MaxStreetLength)}, "", ""},
		{"refuses a number over the cap", Postal{ZipCode: "01310100", Number: strings.Repeat("1", MaxNumberLength+1)}, FieldNumber, RuleTooLong},
		{"refuses a complement over the cap", Postal{ZipCode: "01310100", Complement: strings.Repeat("a", MaxComplementLength+1)}, FieldComplement, RuleTooLong},
		{"refuses a district over the cap", Postal{ZipCode: "01310100", District: strings.Repeat("a", MaxDistrictLength+1)}, FieldDistrict, RuleTooLong},
		{"refuses a city over the cap", Postal{City: strings.Repeat("a", MaxCityLength+1), State: "SP"}, FieldCity, RuleTooLong},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.postal.Validate()
			if tt.field == "" {
				if err != nil {
					t.Fatalf("expected valid, got %v", err)
				}
				return
			}
			var invalid InvalidFieldError
			if !errors.As(err, &invalid) || !errors.Is(err, ErrInvalidAddress) {
				t.Fatalf("expected an InvalidFieldError wrapping ErrInvalidAddress, got %v", err)
			}
			if invalid.Field != tt.field || invalid.Rule != tt.rule {
				t.Fatalf("got %s/%s, expected %s/%s", invalid.Field, invalid.Rule, tt.field, tt.rule)
			}
		})
	}
}

func TestPostalMissing(t *testing.T) {
	tests := []struct {
		name     string
		postal   Postal
		expected []Field
	}{
		{"a complete address misses nothing", paulista(), nil},
		{"the complement and city code are optional", Postal{ZipCode: "01310100", Street: "Av", Number: "1", District: "Centro", City: "SP", State: "SP"}, nil},
		{"blank text counts as missing", Postal{ZipCode: "  ", Street: "Av", Number: "1", District: "Centro", City: "SP", State: "SP"}, []Field{FieldZipCode}},
		{"lists every missing field in billing order", Postal{Street: "Rua Sem Numero"}, []Field{FieldZipCode, FieldNumber, FieldDistrict, FieldCity, FieldState}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.postal.Missing(); !reflect.DeepEqual(got, tt.expected) {
				t.Fatalf("Missing() = %v, expected %v", got, tt.expected)
			}
		})
	}
}

func TestAddressMissingIncludesTheName(t *testing.T) {
	got := Address{Postal: paulista()}.Missing()
	if !reflect.DeepEqual(got, []Field{FieldName}) {
		t.Fatalf("Missing() = %v", got)
	}
}

func TestPostalFingerprint(t *testing.T) {
	base := paulista().Fingerprint()
	if len(base) != 32 {
		t.Fatalf("expected a 32 character fingerprint, got %q", base)
	}
	same := Postal{ZipCode: "01310-100", Street: " AVENIDA  PAULISTA", Number: "1000", District: "bela vista", City: "Sao Paulo", State: "sp", Complement: "ap 12"}
	if got := same.Fingerprint(); got != base {
		t.Fatalf("spelling, accents, case and complement must not change the fingerprint: %q vs %q", got, base)
	}
	moved := paulista()
	moved.Number = "1001"
	if moved.Fingerprint() == base {
		t.Fatal("a different house number must change the fingerprint")
	}
	shifted := Postal{Street: "Avenida Paulista 1000", City: "São Paulo", State: "SP", ZipCode: "01310100", District: "Bela Vista"}
	if shifted.Fingerprint() == base {
		t.Fatal("moving text between fields must change the fingerprint")
	}
}

func TestDistrictKey(t *testing.T) {
	tests := []struct {
		raw      string
		expected string
	}{
		{"Centro", "centro"},
		{"  CENTRO  ", "centro"},
		{"Jd. América", "jardim america"},
		{"JARDIM AMÉRICA", "jardim america"},
		{"Jd América", "jardim america"},
		{"Vl. Mariana", "vila mariana"},
		{"Vl Madalena", "vila madalena"},
		{"Pq. São Lucas", "parque sao lucas"},
		{"Res. Alphaville", "residencial alphaville"},
		{"Cj. Hab. Teotônio Vilela", "conjunto hab teotonio vilela"},
		{"Conj. Ceará", "conjunto ceara"},
		{"St. Bueno", "setor bueno"},
		{"Setor Bueno", "setor bueno"},
		{"Sta. Efigênia", "santa efigenia"},
		{"Santa Efigênia", "santa efigenia"},
		{"Sto. Amaro", "santo amaro"},
		{"N. Sra. das Graças", "nossa senhora das gracas"},
		{"N Sra Aparecida", "nossa senhora aparecida"},
		{"Nossa Senhora das Graças", "nossa senhora das gracas"},
		{"Jardim  Paulista", "jardim paulista"},
		{"Jd.Paulista", "jardim paulista"},
		{"Vila São José - Setor 2", "vila sao jose setor 2"},
		{"Jardinópolis", "jardinopolis"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			if got := DistrictKey(tt.raw); got != tt.expected {
				t.Fatalf("DistrictKey(%q) = %q, expected %q", tt.raw, got, tt.expected)
			}
		})
	}
}

func TestPostalPlaceKeepsCityAndDistrictTogether(t *testing.T) {
	contagem := Postal{District: "Centro", City: "Contagem", State: "MG", CityCode: "3118601"}.Place()
	vespasiano := Postal{District: "Centro", City: "Vespasiano", State: "MG", CityCode: "3171204"}.Place()
	if contagem == vespasiano {
		t.Fatal("Centro in two cities must be two places")
	}
	if contagem != (Place{CityCode: "3118601", DistrictKey: "centro"}) {
		t.Fatalf("Place() = %+v", contagem)
	}
	if err := contagem.Validate(); err != nil {
		t.Fatalf("expected a valid place, got %v", err)
	}
	for name, place := range map[string]Place{
		"no city code":    {DistrictKey: "centro"},
		"no district":     {CityCode: "3118601"},
		"short city code": {CityCode: "31186", DistrictKey: "centro"},
		"letters in code": {CityCode: "31186AB", DistrictKey: "centro"},
	} {
		if err := place.Validate(); !errors.Is(err, ErrInvalidPlace) {
			t.Fatalf("%s: expected ErrInvalidPlace, got %v", name, err)
		}
	}
}

func TestValidCityCode(t *testing.T) {
	for code, valid := range map[string]bool{"3550308": true, "355030": false, "35503080": false, "355030A": false, "": false} {
		if got := ValidCityCode(code); got != valid {
			t.Fatalf("ValidCityCode(%q) = %v, expected %v", code, got, valid)
		}
	}
}
