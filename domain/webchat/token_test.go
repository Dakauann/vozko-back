package webchat

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func testKeys(t *testing.T) Keys {
	t.Helper()
	keys, err := DeriveKeys("root-secret-for-tests")
	if err != nil {
		t.Fatal(err)
	}
	return keys
}

func TestDeriveKeysRefusesAnEmptyRoot(t *testing.T) {
	if _, err := DeriveKeys(" "); !errors.Is(err, ErrSigningKeyMissing) {
		t.Fatalf("DeriveKeys(empty) = %v", err)
	}
}

func TestDerivedKeysAreSeparatedByPurpose(t *testing.T) {
	keys := testKeys(t)
	if string(keys.VisitorToken) == string(keys.Challenge) {
		t.Fatal("token and challenge keys must differ")
	}
}

func TestVisitorTokenRoundTrip(t *testing.T) {
	keys := testKeys(t)
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	token, err := NewVisitorToken(keys, VisitorGrant{WidgetID: "w1", VisitorID: "v1"}, now)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := VerifyVisitorToken(keys, token, now.Add(VisitorTokenTTL-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if grant.WidgetID != "w1" || grant.VisitorID != "v1" || !grant.ExpiresAt.Equal(now.Add(VisitorTokenTTL)) {
		t.Fatalf("grant = %+v", grant)
	}
}

func TestVisitorTokenRefusesTamperingAndExpiry(t *testing.T) {
	keys := testKeys(t)
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	token, _ := NewVisitorToken(keys, VisitorGrant{WidgetID: "w1", VisitorID: "v1"}, now)
	other, _ := DeriveKeys("another-root")
	forged, _ := NewVisitorToken(other, VisitorGrant{WidgetID: "w1", VisitorID: "v2"}, now)
	payload := strings.Split(token, ".")[1]
	swapped := strings.Replace(token, payload, strings.Split(forged, ".")[1], 1)

	cases := map[string]struct {
		token string
		at    time.Time
		want  error
	}{
		"expired":           {token, now.Add(VisitorTokenTTL + time.Second), ErrVisitorTokenExpired},
		"wrong key":         {forged, now, ErrVisitorTokenInvalid},
		"swapped payload":   {swapped, now, ErrVisitorTokenInvalid},
		"truncated":         {token[:len(token)-3], now, ErrVisitorTokenInvalid},
		"empty":             {"", now, ErrVisitorTokenInvalid},
		"other version":     {strings.Replace(token, "v1.", "v2.", 1), now, ErrVisitorTokenInvalid},
		"issued in future":  {token, now.Add(-time.Hour), ErrVisitorTokenInvalid},
		"no signature part": {"v1." + payload, now, ErrVisitorTokenInvalid},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := VerifyVisitorToken(keys, tc.token, tc.at); !errors.Is(err, tc.want) {
				t.Fatalf("VerifyVisitorToken = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestVisitorTokenNeedsBothIDs(t *testing.T) {
	if _, err := NewVisitorToken(testKeys(t), VisitorGrant{WidgetID: "w1"}, time.Now()); !errors.Is(err, ErrVisitorTokenInvalid) {
		t.Fatalf("NewVisitorToken without visitor = %v", err)
	}
}
