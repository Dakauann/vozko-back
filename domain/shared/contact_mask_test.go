package shared

import "testing"

func TestMaskContactKeepsOnlyTheLastFourCharacters(t *testing.T) {
	cases := map[string]string{
		"+5584994409624": "••••9624",
		"9624":           "••••",
		"":               "",
		"@maria.silva":   "••••ilva",
	}
	for in, want := range cases {
		if got := MaskContact(in); got != want {
			t.Errorf("MaskContact(%q) = %q, want %q", in, got, want)
		}
	}
}
