package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func hubSig256ForTest(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestNormalizeAppSecrets(t *testing.T) {
	got := normalizeAppSecrets([]string{" a ", "", "b", "a", "  ", "b"})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("normalize: want [a b], got %v", got)
	}
	if len(normalizeAppSecrets(nil)) != 0 || len(normalizeAppSecrets([]string{"", "  "})) != 0 {
		t.Fatal("empty/blank input must yield no secrets")
	}
}

func TestVerifyHubSignatureAny(t *testing.T) {
	body := []byte(`{"object":"whatsapp_business_account"}`)
	sig := hubSig256ForTest("new-app-secret", body) // signed by the NEW app's secret

	// One endpoint carrying both old+new secrets accepts a webhook signed by either
	// (the app-migration case the feature exists for).
	if !verifyHubSignatureAny([]string{"old-app-secret", "new-app-secret"}, body, sig) {
		t.Fatal("must verify against any configured secret")
	}
	// No matching secret → reject.
	if verifyHubSignatureAny([]string{"old-app-secret"}, body, sig) {
		t.Fatal("must reject when no configured secret matches")
	}
	// Empty signature header → reject.
	if verifyHubSignatureAny([]string{"new-app-secret"}, body, "") {
		t.Fatal("empty signature header must reject")
	}
}
