package address

import "testing"

func TestStateCode(t *testing.T) {
	cases := []struct {
		raw   string
		want  string
		known bool
	}{
		{"SP", "SP", true},
		{" rj ", "RJ", true},
		{"São Paulo", "SP", true},
		{"sao paulo", "SP", true},
		{"Rio Grande do Sul", "RS", true},
		{"ESPÍRITO SANTO", "ES", true},
		{"Distrito Federal", "DF", true},
		{"Pará", "PA", true},
		{"Paraíba", "PB", true},
		{"Paraná", "PR", true},
		{"  Mato  Grosso do Sul ", "MS", true},
		{"", "", false},
		{"XX", "", false},
		{"Buenos Aires", "", false},
	}
	for _, tc := range cases {
		got, known := StateCode(tc.raw)
		if got != tc.want || known != tc.known {
			t.Errorf("StateCode(%q) = %q, %v, want %q, %v", tc.raw, got, known, tc.want, tc.known)
		}
	}
}

func TestStateOfCityCode(t *testing.T) {
	cases := []struct {
		code, state string
		ok          bool
	}{
		{"1400100", "RR", true},
		{"3550308", "SP", true},
		{"5300108", "DF", true},
		{"9900000", "", false},
		{"35", "", false},
		{"35a0308", "", false},
	}
	for _, tc := range cases {
		state, ok := StateOfCityCode(tc.code)
		if state != tc.state || ok != tc.ok {
			t.Errorf("StateOfCityCode(%q) = %q, %v, want %q, %v", tc.code, state, ok, tc.state, tc.ok)
		}
	}
}

func TestIBGEStatesCoverEveryState(t *testing.T) {
	states := IBGEStates()
	if len(states) != len(brazilStates) {
		t.Fatalf("IBGEStates() has %d units, want %d", len(states), len(brazilStates))
	}
	for _, s := range states {
		if !brazilStates[s.State] || len(s.Code) != 2 {
			t.Fatalf("IBGE unit %+v is not a known state with a two digit code", s)
		}
	}
	if states[0] != (IBGEState{Code: "11", State: "RO"}) {
		t.Fatalf("first unit = %+v, want 11 RO in IBGE order", states[0])
	}
}
