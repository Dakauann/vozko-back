package vault

import (
	"bytes"
	"errors"
	"testing"

	cvault "vozko/infra/crypto/vault"
)

func TestAliasIdentity(t *testing.T) {
	var a Vault
	var b cvault.Vault

	_ = (*Vault)(&b)
	_ = (*cvault.Vault)(&a)

	if !errors.Is(ErrCiphertextShort, cvault.ErrCiphertextShort) {
		t.Fatal("ErrCiphertextShort alias drift")
	}
}

func TestAliasNewRoundTrip(t *testing.T) {
	v, err := New(bytes.Repeat([]byte{0x11}, 32), 3)
	if err != nil {
		t.Fatal(err)
	}
	if v.Version() != 3 {
		t.Fatalf("version=%d", v.Version())
	}
	env, err := v.Seal([]byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := v.Open(env)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte("hello")) {
		t.Fatalf("plain mismatch: %q", got)
	}
}

func TestAliasNewBadKey(t *testing.T) {
	if _, err := New([]byte("nope"), 1); err == nil {
		t.Fatal("expected error for short key")
	}
}
