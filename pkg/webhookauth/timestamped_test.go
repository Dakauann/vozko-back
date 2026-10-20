package webhookauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"testing"
	"time"
)

func timestampedHeader(secret string, ts int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(ts, 10) + "." + string(body)))
	return "t=" + strconv.FormatInt(ts, 10) + ",s=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifyTimestampedHMAC(t *testing.T) {
	secret := "app-secret"
	body := []byte(`{"event":"im_receive_msg"}`)
	now := time.Unix(1641974633, 0)
	tolerance := 5 * time.Minute
	good := timestampedHeader(secret, now.Unix(), body)

	if err := VerifyTimestampedHMAC(secret, good, body, now, tolerance); err != nil {
		t.Fatalf("valid signature refused: %v", err)
	}

	cases := []struct {
		name   string
		secret string
		header string
		body   []byte
		now    time.Time
		want   error
	}{
		{"wrong secret", "other", good, body, now, ErrSignatureMismatch},
		{"tampered body", secret, good, []byte(`{"event":"im_send_msg"}`), now, ErrSignatureMismatch},
		{"stale", secret, good, body, now.Add(tolerance + time.Second), ErrSignatureStale},
		{"from the future", secret, good, body, now.Add(-tolerance - time.Second), ErrSignatureStale},
		{"no secret", "", good, body, now, ErrSignatureMismatch},
		{"empty header", secret, "", body, now, ErrSignatureMalformed},
		{"no timestamp", secret, "s=abcd", body, now, ErrSignatureMalformed},
		{"no signature", secret, "t=1641974633", body, now, ErrSignatureMalformed},
		{"duplicate timestamp", secret, good + ",t=1641974633", body, now, ErrSignatureMalformed},
		{"duplicate signature", secret, good + ",s=00", body, now, ErrSignatureMalformed},
		{"non numeric timestamp", secret, "t=abc,s=00", body, now, ErrSignatureMalformed},
		{"negative timestamp", secret, "t=-5,s=00", body, now, ErrSignatureMalformed},
		{"element without a value", secret, "t,s=00", body, now, ErrSignatureMalformed},
		{"not hex", secret, "t=1641974633,s=zz", body, now, ErrSignatureMismatch},
	}
	for _, tc := range cases {
		err := VerifyTimestampedHMAC(tc.secret, tc.header, tc.body, tc.now, tolerance)
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
}

func TestVerifyTimestampedHMACAcceptsTheEdgeOfTheTolerance(t *testing.T) {
	secret := "s"
	body := []byte("{}")
	signedAt := time.Unix(1700000000, 0)
	header := timestampedHeader(secret, signedAt.Unix(), body)
	if err := VerifyTimestampedHMAC(secret, header, body, signedAt.Add(5*time.Minute), 5*time.Minute); err != nil {
		t.Fatalf("exactly at the tolerance: %v", err)
	}
}

func TestVerifyTimestampedHMACIgnoresSpacesAroundElements(t *testing.T) {
	secret := "s"
	body := []byte("{}")
	now := time.Unix(1700000000, 0)
	header := timestampedHeader(secret, now.Unix(), body)
	spaced := " " + header[:12] + " , " + header[13:]
	if err := VerifyTimestampedHMAC(secret, spaced, body, now, time.Minute); err != nil {
		t.Fatalf("spaced header %q: %v", spaced, err)
	}
}
