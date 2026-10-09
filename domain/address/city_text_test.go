package address

import "testing"

func TestCityKeyFromTextReadsTheWaysPeopleWriteACity(t *testing.T) {
	cases := []struct {
		name, raw, state string
		want             string
		ok               bool
	}{
		{"a city key", "sp:campinas", "", "sp:campinas", true},
		{"a city key in capitals", "SP:Campinas", "", "sp:campinas", true},
		{"name and UF with a slash", "Campinas/SP", "", "sp:campinas", true},
		{"name and UF with a comma", "São José dos Campos, SP", "", "sp:sao jose dos campos", true},
		{"name and UF with a dash", "Feira de Santana - BA", "", "ba:feira de santana", true},
		{"name and state name", "Natal/Rio Grande do Norte", "", "rn:natal", true},
		{"name with the UF apart", "Campinas", "sp", "sp:campinas", true},
		{"the UF apart agrees with the text", "Campinas/SP", "SP", "sp:campinas", true},
		{"a name without a state", "Campinas", "", "", false},
		{"an unknown state", "Campinas/XX", "", "", false},
		{"the UF apart disagrees with the text", "Campinas/SP", "RJ", "", false},
		{"an unknown UF apart", "Campinas", "ZZ", "", false},
		{"nothing", "  ", "SP", "", false},
		{"a key of an unknown state", "zz:campinas", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := CityKeyFromText(tc.raw, tc.state)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("CityKeyFromText(%q, %q) = %q, %v; want %q, %v", tc.raw, tc.state, got, ok, tc.want, tc.ok)
			}
		})
	}
}
