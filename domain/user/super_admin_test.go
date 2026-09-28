package user

import "testing"

func TestIsSuperAdmin(t *testing.T) {
	cases := []struct {
		email string
		want  bool
	}{
		{"dakauannc@gmail.com", true},
		{"joscelioapinheiro@gmail.com", true},
		{"  DakauAnnc@Gmail.com ", true},
		{"dakauannc@vozkoia.com", false},
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

func TestVerifySuperAdminPin(t *testing.T) {
	cases := []struct {
		name  string
		email string
		pin   string
		want  bool
	}{
		{"own pin", "dakauannc@gmail.com", "1601", true},
		{"second admin's own pin", "joscelioapinheiro@gmail.com", "9412", true},
		{"email case and spaces do not matter", " DAKAUANNC@gmail.com ", " 1601 ", true},
		{"the other admin's pin is refused", "dakauannc@gmail.com", "9412", false},
		{"and the other way round", "joscelioapinheiro@gmail.com", "1601", false},
		{"wrong pin", "dakauannc@gmail.com", "0000", false},
		{"empty pin", "dakauannc@gmail.com", "", false},
		{"longer pin that starts right", "dakauannc@gmail.com", "16010", false},
		{"not a super admin", "someone@vozkoia.com", "1601", false},
		{"no email", "", "1601", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := VerifySuperAdminPin(tc.email, tc.pin); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSuperAdminPins_AreFourDigits(t *testing.T) {
	for email, pin := range superAdminPins {
		if len(pin) != 4 {
			t.Errorf("%s: pin must have 4 digits, has %d", email, len(pin))
		}
		for _, r := range pin {
			if r < '0' || r > '9' {
				t.Errorf("%s: pin must be numeric", email)
			}
		}
	}
}
