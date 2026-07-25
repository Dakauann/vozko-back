package webhookauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func TestConstantTimeEqual(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"abc", "abc", true},
		{"abc", "abd", false},
		{"", "", true},
		{"abc", "", false},
		{"abc", "abcd", false},
	}
	for _, c := range cases {
		if got := ConstantTimeEqual(c.a, c.b); got != c.want {
			t.Errorf("ConstantTimeEqual(%q,%q)=%v want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestVerifyHexHMAC(t *testing.T) {
	secret := "s3cr3t"
	body := []byte(`{"a":1}`)
	good := sign(secret, body)

	if !VerifyHexHMAC(secret, body, good) {
		t.Fatal("expected valid signature to verify")
	}
	if VerifyHexHMAC("wrong", body, good) {
		t.Fatal("wrong secret must not verify")
	}
	if VerifyHexHMAC(secret, []byte(`{"a":2}`), good) {
		t.Fatal("tampered body must not verify")
	}
	if VerifyHexHMAC("", body, good) {
		t.Fatal("empty secret must not verify")
	}
	if VerifyHexHMAC(secret, body, "") {
		t.Fatal("empty signature must not verify")
	}
	if VerifyHexHMAC(secret, body, "zzz-not-hex") {
		t.Fatal("non-hex signature must not verify")
	}
	if VerifyHexHMAC(secret, body, "  "+good+"  ") == false {
		t.Fatal("surrounding whitespace should be trimmed and verify")
	}
}

func TestVerifyPrefixedHMAC(t *testing.T) {
	secret := "s3cr3t"
	body := []byte(`payload`)
	good := "sha256=" + sign(secret, body)

	if !VerifyPrefixedHMAC(secret, body, good) {
		t.Fatal("expected valid prefixed signature to verify")
	}
	if VerifyPrefixedHMAC(secret, body, "") {
		t.Fatal("empty header must not verify")
	}
	if VerifyPrefixedHMAC(secret, body, sign(secret, body)) {
		t.Fatal("missing sha256= prefix must not verify")
	}
	if VerifyPrefixedHMAC(secret, body, "sha1="+sign(secret, body)) {
		t.Fatal("wrong algo prefix must not verify")
	}
	if VerifyPrefixedHMAC(secret, body, "sha256=nothex") {
		t.Fatal("bad hex must not verify")
	}
}

func TestRandomToken(t *testing.T) {
	tok, err := RandomToken(16)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tok) != 32 {
		t.Fatalf("expected 32 hex chars for 16 bytes, got %d", len(tok))
	}
	if _, err := hex.DecodeString(tok); err != nil {
		t.Fatalf("token must be valid hex: %v", err)
	}
	other, _ := RandomToken(16)
	if tok == other {
		t.Fatal("two random tokens must not collide")
	}
}
