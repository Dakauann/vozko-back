package selection

import "testing"

func TestASelectionExceedsACapOnlyWithWhatRemainsAfterItsExclusions(t *testing.T) {
	cases := []struct {
		name    string
		s       Selection
		matched int
		max     int
		want    bool
	}{
		{"under the cap", Selection{Mode: ModeAllMatching}, 100, 100, false},
		{"over the cap", Selection{Mode: ModeAllMatching}, 101, 100, true},
		{"over the cap until the exclusions", Selection{Mode: ModeAllMatching, ExcludeIDs: []string{"a", "b"}}, 102, 100, false},
		{"first n under the cap", Selection{Mode: ModeFirstN, Limit: 50}, 500, 100, false},
		{"explicit ids", Selection{Mode: ModeIDs, IDs: []string{"a"}}, 1, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.s.Exceeds(tc.matched, tc.max); got != tc.want {
				t.Fatalf("Exceeds = %v, want %v", got, tc.want)
			}
		})
	}
}
