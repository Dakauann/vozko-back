package mercadolivre

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateNotificationToken(t *testing.T) {
	cases := []struct {
		name  string
		token string
		ok    bool
	}{
		{"32 random bytes in base64url", "q3Jv9xZ0b2xH5nLw1aPq8sT4uV7yB6cD-eF_gH2iJkM", true},
		{"longer is fine", strings.Repeat("a", 96), true},
		{"empty", "", false},
		{"too short to carry 32 bytes", strings.Repeat("a", 42), false},
		{"outside base64url", strings.Repeat("a", 42) + "/", false},
		{"padding is not url safe", strings.Repeat("a", 42) + "=", false},
		{"too long", strings.Repeat("a", 129), false},
		{"spaces", strings.Repeat("a", 40) + " bc", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateNotificationToken(tc.token)
			if tc.ok && err != nil {
				t.Fatalf("err = %v", err)
			}
			if !tc.ok && !errors.Is(err, ErrNotificationTokenWeak) {
				t.Fatalf("err = %v, want ErrNotificationTokenWeak", err)
			}
		})
	}
}

func TestValidateRedirectURI(t *testing.T) {
	cases := []struct {
		uri string
		ok  bool
	}{
		{"https://api.example.com/oauth/mercadolivre/callback", true},
		{"http://api.example.com/oauth/mercadolivre/callback", false},
		{"https://api.example.com/oauth/mercadolivre/callback?x=1", false},
		{"https://api.example.com/oauth/mercadolivre/callback#frag", false},
		{"https:///oauth/mercadolivre/callback", false},
		{"", false},
		{"not a url", false},
	}
	for _, tc := range cases {
		err := ValidateRedirectURI(tc.uri)
		if tc.ok != (err == nil) {
			t.Errorf("%q: err = %v", tc.uri, err)
		}
		if !tc.ok && !errors.Is(err, ErrRedirectURIInvalid) {
			t.Errorf("%q: err = %v, want ErrRedirectURIInvalid", tc.uri, err)
		}
	}
}

func TestValidateAppID(t *testing.T) {
	if err := ValidateAppID("1234567890123456"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "abc", "12a"} {
		if err := ValidateAppID(bad); !errors.Is(err, ErrAppIDInvalid) {
			t.Errorf("%q: err = %v", bad, err)
		}
	}
}
