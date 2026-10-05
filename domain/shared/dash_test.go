package shared

import "testing"

func TestLongDashesAreDetectedAndHyphensAreNot(t *testing.T) {
	cases := map[string]bool{
		"teste " + string(rune(0x2014)) + " errado": true,
		"teste " + string(rune(0x2013)) + " errado": true,
		"Wi-Fi e auto-atendimento":                  false,
		"":                                          false,
	}
	for text, want := range cases {
		if got := HasLongDash(text); got != want {
			t.Errorf("%q: got %v", text, got)
		}
	}
}
