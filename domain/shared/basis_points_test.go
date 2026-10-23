package shared

import "testing"

func TestBasisPointsRoundsHalfUpAndNeverDividesByZero(t *testing.T) {
	cases := []struct {
		name string
		n, d int64
		want int
	}{
		{"nothing over nothing is zero, not a panic", 0, 0, 0},
		{"a part over nothing is zero", 5, 0, 0},
		{"a negative part is zero", -1, 10, 0},
		{"the whole is ten thousand", 10, 10, 10000},
		{"one third rounds to 3333", 1, 3, 3333},
		{"two thirds rounds up to 6667", 2, 3, 6667},
		{"one in twenty thousand rounds half up to one", 1, 20000, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := BasisPoints(tc.n, tc.d); got != tc.want {
				t.Fatalf("BasisPoints(%d, %d) = %d, want %d", tc.n, tc.d, got, tc.want)
			}
		})
	}
}
