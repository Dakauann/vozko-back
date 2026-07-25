package support_inbox

import (
	"strings"
	"testing"
	"time"
)

func TestGenerateSessionToken(t *testing.T) {
	token, err := GenerateSessionToken("test-secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts separated by '.', got %d", len(parts))
	}
	if len(parts[0]) != 64 {
		t.Errorf("expected payload of 64 hex chars, got %d", len(parts[0]))
	}
}

func TestValidateSessionToken_Valid(t *testing.T) {
	secret := "my-secret-key"
	token, err := GenerateSessionToken(secret)
	if err != nil {
		t.Fatalf("unexpected error generating token: %v", err)
	}

	if !ValidateSessionToken(token, secret) {
		t.Fatal("expected token to be valid")
	}
}

func TestValidateSessionToken_WrongSecret(t *testing.T) {
	token, _ := GenerateSessionToken("correct-secret")
	if ValidateSessionToken(token, "wrong-secret") {
		t.Fatal("expected token to be invalid with wrong secret")
	}
}

func TestValidateSessionToken_TamperedPayload(t *testing.T) {
	token, _ := GenerateSessionToken("secret")
	parts := strings.Split(token, ".")
	if len(parts) == 2 {
		tampered := "aaaa" + parts[0][4:] + "." + parts[1]
		if ValidateSessionToken(tampered, "secret") {
			t.Fatal("expected tampered token to be invalid")
		}
	}
}

func TestValidateSessionToken_InvalidFormat(t *testing.T) {
	if ValidateSessionToken("not-a-valid-token", "secret") {
		t.Fatal("expected invalid format token to be invalid")
	}
}

func TestValidateSessionToken_EmptyToken(t *testing.T) {
	if ValidateSessionToken("", "secret") {
		t.Fatal("expected empty token to be invalid")
	}
}

func TestSupportSession_IsExpired(t *testing.T) {
	t.Run("not expired", func(t *testing.T) {
		s := &SupportSession{
			ExpiresAt: time.Now().Add(1 * time.Hour),
		}
		if s.IsExpired() {
			t.Fatal("session should not be expired")
		}
	})
	t.Run("expired", func(t *testing.T) {
		s := &SupportSession{
			ExpiresAt: time.Now().Add(-1 * time.Hour),
		}
		if !s.IsExpired() {
			t.Fatal("session should be expired")
		}
	})
}
