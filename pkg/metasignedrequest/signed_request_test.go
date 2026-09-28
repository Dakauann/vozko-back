package metasignedrequest

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

const secret = "app-secret"

func build(t *testing.T, payload, key string) string {
	t.Helper()
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(encoded))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)) + "." + encoded
}

func TestParseValid(t *testing.T) {
	raw := build(t, `{"algorithm":"HMAC-SHA256","issued_at":1291836800,"user_id":"218471"}`, secret)
	got, err := Parse(raw, secret)
	if err != nil {
		t.Fatal(err)
	}
	if got.UserID != "218471" || !got.IssuedAt.Equal(time.Unix(1291836800, 0).UTC()) {
		t.Fatalf("payload = %+v", got)
	}
}

func TestParseAcceptsNumericUserID(t *testing.T) {
	raw := build(t, `{"algorithm":"HMAC-SHA256","user_id":218471}`, secret)
	got, err := Parse(raw, secret)
	if err != nil || got.UserID != "218471" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestParseAcceptsPaddedBase64(t *testing.T) {
	payload := base64.URLEncoding.EncodeToString([]byte(`{"algorithm":"HMAC-SHA256","user_id":"1"}`))
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	raw := base64.URLEncoding.EncodeToString(mac.Sum(nil)) + "." + payload
	if _, err := Parse(raw, secret); err != nil {
		t.Fatalf("padded form rejected: %v", err)
	}
}

func TestParseRejects(t *testing.T) {
	valid := build(t, `{"algorithm":"HMAC-SHA256","user_id":"1"}`, secret)
	cases := map[string]string{
		"wrong secret":     build(t, `{"algorithm":"HMAC-SHA256","user_id":"1"}`, "other"),
		"other algorithm":  build(t, `{"algorithm":"HMAC-SHA1","user_id":"1"}`, secret),
		"missing user":     build(t, `{"algorithm":"HMAC-SHA256"}`, secret),
		"tampered payload": valid[:len(valid)-2] + "xx",
		"no separator":     "abc",
		"empty":            "",
		"not json":         build(t, `nope`, secret),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(raw, secret); !errors.Is(err, ErrInvalid) {
				t.Fatalf("got %v, want ErrInvalid", err)
			}
		})
	}
}

func TestParseRequiresSecret(t *testing.T) {
	if _, err := Parse(build(t, `{"algorithm":"HMAC-SHA256","user_id":"1"}`, secret), ""); !errors.Is(err, ErrInvalid) {
		t.Fatal("empty secret accepted")
	}
}
