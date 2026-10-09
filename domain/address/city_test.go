package address

import "testing"

func TestCityKey(t *testing.T) {
	cases := []struct {
		name, city, state, want string
	}{
		{"accents, case and spacing fold away", "  São   Paulo ", "sp", "sp:sao paulo"},
		{"abbreviations expand like a bairro", "Sta. Rita do Sapucaí", "MG", "mg:santa rita do sapucai"},
		{"the state keeps two cities with one name apart", "Bom Jesus", "PI", "pi:bom jesus"},
		{"a city without a state has no key", "Bom Jesus", "", ""},
		{"a state without a city has no key", "", "RS", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CityKey(tc.city, tc.state); got != tc.want {
				t.Fatalf("CityKey(%q, %q) = %q, want %q", tc.city, tc.state, got, tc.want)
			}
		})
	}
	if CityKey("Bom Jesus", "PI") == CityKey("Bom Jesus", "RS") {
		t.Fatal("two cities with one name in two states must not share a key")
	}
}

func TestParseCityKey_GivesTheStoredKey(t *testing.T) {
	cases := []struct {
		raw, want string
		ok        bool
	}{
		{"sp:sao paulo", "sp:sao paulo", true},
		{"SP:São Paulo", "sp:sao paulo", true},
		{" mg : Sta. Rita do Sapucaí ", "mg:santa rita do sapucai", true},
		{"Sao Paulo", "", false},
		{"xx:sao paulo", "", false},
		{"spx:sao paulo", "", false},
		{"sp:", "", false},
		{":sao paulo", "", false},
	}
	for _, tc := range cases {
		got, ok := ParseCityKey(tc.raw)
		if got != tc.want || ok != tc.ok {
			t.Errorf("ParseCityKey(%q) = %q, %v; want %q, %v", tc.raw, got, ok, tc.want, tc.ok)
		}
	}
	if key, _ := ParseCityKey("SP:São Paulo"); key != CityKey("São Paulo", "SP") {
		t.Fatal("a parsed key must equal the key stored on the address")
	}
}

func TestCityNameKeyIsTheNamePartOfTheCityKey(t *testing.T) {
	cases := []struct{ name, want string }{
		{"  São   Paulo ", "sao paulo"},
		{"Sta. Rita do Sapucaí", "santa rita do sapucai"},
		{"ALTA FLORESTA D'OESTE", "alta floresta d oeste"},
		{"", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CityNameKey(tc.name); got != tc.want {
				t.Fatalf("CityNameKey(%q) = %q, want %q", tc.name, got, tc.want)
			}
			if tc.want != "" && CityKey(tc.name, "SP") != "sp:"+tc.want {
				t.Fatalf("CityKey and CityNameKey disagree on %q", tc.name)
			}
		})
	}
}
