package piigorm

import (
	"bytes"
	"errors"
	"testing"

	"vozko/infra/crypto/pii"
	"vozko/infra/crypto/vault"
)

func newService(t *testing.T) *pii.Service {
	t.Helper()
	key := bytes.Repeat([]byte{0x77}, 32)
	v, err := vault.New(key, 1)
	if err != nil {
		t.Fatal(err)
	}
	s, err := pii.New(map[byte]*vault.Vault{1: v}, 1, bytes.Repeat([]byte{0xBB}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func withService(t *testing.T, s *pii.Service) {
	t.Helper()
	SetService(s)
	t.Cleanup(func() { SetService(nil) })
}

func TestEncryptedString_RoundTrip_Bytes(t *testing.T) {
	withService(t, newService(t))

	src := NewEncrypted("12345678900")
	v, err := src.Value()
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := v.([]byte)
	if !ok {
		t.Fatalf("Value returned %T, want []byte", v)
	}
	if len(raw) == 0 {
		t.Fatal("ciphertext empty")
	}

	var dst EncryptedString
	if err := dst.Scan(raw); err != nil {
		t.Fatal(err)
	}
	if !dst.Valid || dst.Plain != "12345678900" {
		t.Fatalf("round-trip mismatch: %+v", dst)
	}
	if dst.String() != "12345678900" {
		t.Fatalf("String()=%q", dst.String())
	}
	if (EncryptedString{}).GormDataType() != "bytea" {
		t.Fatal("GormDataType")
	}
}

func TestEncryptedString_RoundTrip_StringSource(t *testing.T) {
	withService(t, newService(t))
	src := NewEncrypted("hello")
	v, _ := src.Value()
	raw := v.([]byte)

	var dst EncryptedString
	if err := dst.Scan(string(raw)); err != nil {
		t.Fatal(err)
	}
	if dst.Plain != "hello" {
		t.Fatalf("got %q", dst.Plain)
	}
}

func TestEncryptedString_Null(t *testing.T) {
	withService(t, newService(t))

	n := Null()
	v, err := n.Value()
	if err != nil {
		t.Fatal(err)
	}
	if v != nil {
		t.Fatalf("Null Value() = %v, want nil", v)
	}

	var dst EncryptedString
	if err := dst.Scan(nil); err != nil {
		t.Fatal(err)
	}
	if dst.Valid {
		t.Fatal("expected !Valid")
	}
	if dst.String() != "" {
		t.Fatal("expected empty string")
	}

	dst = EncryptedString{Plain: "x", Valid: true}
	if err := dst.Scan([]byte{}); err != nil {
		t.Fatal(err)
	}
	if dst.Valid {
		t.Fatal("empty []byte should reset to invalid")
	}
}

func TestEncryptedString_Scan_UnsupportedType(t *testing.T) {
	withService(t, newService(t))
	var dst EncryptedString
	if err := dst.Scan(42); err == nil {
		t.Fatal("expected unsupported-type error")
	}
}

func TestEncryptedString_Scan_DecryptFailure(t *testing.T) {
	withService(t, newService(t))
	var dst EncryptedString
	if err := dst.Scan([]byte{0xFF, 0xFF}); err == nil {
		t.Fatal("expected decrypt failure")
	}
}

func TestEncryptedString_NoServiceConfigured(t *testing.T) {
	SetService(nil)

	_, err := NewEncrypted("x").Value()
	if !errors.Is(err, ErrServiceNotConfigured) {
		t.Fatalf("Value err=%v", err)
	}
	var dst EncryptedString
	if err := dst.Scan([]byte{0x01, 0x01, 0x02, 0x03}); !errors.Is(err, ErrServiceNotConfigured) {
		t.Fatalf("Scan err=%v", err)
	}
}

func TestService_Getter(t *testing.T) {
	SetService(nil)
	if Service() != nil {
		t.Fatal("expected nil")
	}
	s := newService(t)
	SetService(s)
	t.Cleanup(func() { SetService(nil) })
	if Service() != s {
		t.Fatal("Service() did not return the configured instance")
	}
}

func TestBlindIndex(t *testing.T) {
	withService(t, newService(t))

	bi, err := NewBlindIndex("users.cpf", "")
	if err != nil || bi != nil {
		t.Fatalf("empty value: bi=%v err=%v", bi, err)
	}
	if v, _ := BlindIndex(nil).Value(); v != nil {
		t.Fatalf("empty Value()=%v", v)
	}

	bi1, err := NewBlindIndex("users.cpf", "12345678900")
	if err != nil {
		t.Fatal(err)
	}
	if len(bi1) != 32 {
		t.Fatalf("len=%d", len(bi1))
	}
	if (BlindIndex{}).GormDataType() != "bytea" {
		t.Fatal("GormDataType")
	}

	v, err := bi1.Value()
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := v.([]byte)
	if !ok || !bytes.Equal(raw, []byte(bi1)) {
		t.Fatal("Value mismatch")
	}

	var got BlindIndex
	if err := got.Scan(raw); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, bi1) {
		t.Fatal("Scan mismatch")
	}

	var got2 BlindIndex
	if err := got2.Scan(string(raw)); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got2, raw) {
		t.Fatal("Scan(string) mismatch")
	}

	var got3 BlindIndex = []byte{1, 2, 3}
	if err := got3.Scan(nil); err != nil {
		t.Fatal(err)
	}
	if got3 != nil {
		t.Fatal("Scan(nil) should reset")
	}

	if err := (&BlindIndex{}).Scan(123); err == nil {
		t.Fatal("expected unsupported-type error")
	}
}

func TestBlindIndex_NoServiceConfigured(t *testing.T) {
	SetService(nil)
	if _, err := NewBlindIndex("scope", "v"); !errors.Is(err, ErrServiceNotConfigured) {
		t.Fatalf("err=%v", err)
	}
}
