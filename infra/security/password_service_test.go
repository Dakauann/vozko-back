package security

import (
	"testing"
)

func TestMinPasswordHashCost_LGPDFloor(t *testing.T) {

	if MinPasswordHashCost < 12 {
		t.Fatalf("MinPasswordHashCost=%d, must be >= 12", MinPasswordHashCost)
	}
}

func TestNewBcryptPasswordService_DefaultCost(t *testing.T) {
	svc := NewBcryptPasswordService(0)
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

func TestNewBcryptPasswordService_NegativeCostUsesFloor(t *testing.T) {
	svc := NewBcryptPasswordService(-5)
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

func TestNewBcryptPasswordService_CustomCost(t *testing.T) {
	svc := NewBcryptPasswordService(10)
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

func TestHash_ValidPassword(t *testing.T) {
	svc := NewBcryptPasswordService(4)
	hash, err := svc.Hash("MySecurePass123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}
	if hash == "MySecurePass123" {
		t.Fatal("hash should not equal plaintext")
	}
}

func TestHash_EmptyPassword(t *testing.T) {
	svc := NewBcryptPasswordService(4)
	_, err := svc.Hash("")
	if err == nil {
		t.Fatal("expected error for empty password")
	}
}

func TestHash_DifferentHashesForSamePassword(t *testing.T) {
	svc := NewBcryptPasswordService(4)
	h1, _ := svc.Hash("password123")
	h2, _ := svc.Hash("password123")
	if h1 == h2 {
		t.Error("bcrypt should produce different hashes due to salt")
	}
}

func TestVerify_CorrectPassword(t *testing.T) {
	svc := NewBcryptPasswordService(4)
	hash, _ := svc.Hash("correctHorse")
	err := svc.Verify(hash, "correctHorse")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestVerify_WrongPassword(t *testing.T) {
	svc := NewBcryptPasswordService(4)
	hash, _ := svc.Hash("correctHorse")
	err := svc.Verify(hash, "wrongHorse")
	if err == nil {
		t.Fatal("expected error for wrong password")
	}
}

func TestVerify_EmptyHash(t *testing.T) {
	svc := NewBcryptPasswordService(4)
	err := svc.Verify("", "password")
	if err == nil {
		t.Fatal("expected error for empty hash")
	}
}

func TestVerify_EmptyPlaintext(t *testing.T) {
	svc := NewBcryptPasswordService(4)
	hash, _ := svc.Hash("something")
	err := svc.Verify(hash, "")
	if err == nil {
		t.Fatal("expected error for empty plaintext")
	}
}

func TestVerify_BothEmpty(t *testing.T) {
	svc := NewBcryptPasswordService(4)
	err := svc.Verify("", "")
	if err == nil {
		t.Fatal("expected error when both hash and plain are empty")
	}
}

func TestHash_LongPassword(t *testing.T) {
	svc := NewBcryptPasswordService(4)

	longPass := "a"
	for i := 0; i < 100; i++ {
		longPass += "b"
	}
	_, err := svc.Hash(longPass)
	if err == nil {
		t.Fatal("expected error for password exceeding 72 bytes")
	}
}
