package lead

import "testing"

func TestSameNumberMatchesBothNinthDigitFormsAndNeverAnEmptyNumber(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{"identical", "5511987654321", "5511987654321", true},
		{"with and without the ninth digit", "5511987654321", "551187654321", true},
		{"another area with both forms", "5521998765432", "552198765432", true},
		{"another number", "5511987654321", "5511987654322", false},
		{"an empty number", "", "", false},
		{"one side empty", "5511987654321", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SameNumber(tc.a, tc.b); got != tc.want {
				t.Fatalf("SameNumber(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}
