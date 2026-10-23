package emailsending

import "testing"

func TestValidRawMessageKey(t *testing.T) {
	cases := []struct {
		key  string
		want bool
	}{
		{"inbound/d6iitobk75ur44p8kdnnp7g2n800", true},
		{"inbound/", false},
		{"", false},
		{"outbound/abc", false},
		{"inbound/../secret", false},
		{"inbound//etc", false},
		{"Inbound/abc", false},
	}
	for _, tc := range cases {
		if got := ValidRawMessageKey(tc.key); got != tc.want {
			t.Errorf("%q: got %v, want %v", tc.key, got, tc.want)
		}
	}
}
