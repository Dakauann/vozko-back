package user

import "testing"

func TestIsSuperAdmin(t *testing.T) {
	cases := []struct {
		email string
		want  bool
	}{
		{"dakauannc@gmail.com", true},
		{"dakauannc@vozkoia.com", true},
		{"  DakauAnnc@Gmail.com ", true},
		{"someone@vozkoia.com", false},
		{"", false},
		{"dakauannc@gmail.com.evil.io", false},
	}
	for _, tc := range cases {
		if got := IsSuperAdmin(tc.email); got != tc.want {
			t.Errorf("%q: got %v, want %v", tc.email, got, tc.want)
		}
	}
}
