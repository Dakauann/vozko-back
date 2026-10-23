package emailsending

import "testing"

func TestConnectMethodsAreAutomaticOrManual(t *testing.T) {
	cases := map[ConnectMethod]bool{
		ConnectAutomatic: true,
		ConnectManual:    true,
		"entri":          false,
		"":               false,
	}
	for method, want := range cases {
		if got := method.Valid(); got != want {
			t.Errorf("%q valid = %v, want %v", method, got, want)
		}
	}
}
