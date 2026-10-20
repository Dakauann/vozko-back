package workflow

import "testing"

func TestOnlyAPinWithItsOutputCanStandInForTheNode(t *testing.T) {
	cases := []struct {
		name string
		pin  *NodePin
		want bool
	}{
		{"no pin", nil, false},
		{"a pin with its output", &NodePin{Output: map[string]interface{}{"ok": true}, Source: PinSourceManual, PinnedAt: pinTime}, true},
		{"a production pin withheld from this viewer", &NodePin{Source: PinSourceOfRun("run-hidden"), PinnedAt: pinTime, Withheld: true}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.pin.Servable(); got != tc.want {
				t.Fatalf("Servable() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAWithheldPinIsReportedOnlyWhenItIsWithheld(t *testing.T) {
	if (*NodePin)(nil).WithheldNotice() != "" {
		t.Fatal("no pin has nothing to report")
	}
	if (&NodePin{Source: PinSourceManual, PinnedAt: pinTime}).WithheldNotice() != "" {
		t.Fatal("a servable pin has nothing to report")
	}
	if got := (&NodePin{Source: PinSourceOfRun("run-hidden"), PinnedAt: pinTime, Withheld: true}).WithheldNotice(); got != "saída fixada indisponível para você" {
		t.Fatalf("notice = %q", got)
	}
}
