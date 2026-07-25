package vault

import (
	"bytes"
	"crypto/aes"
	"errors"
	"testing"
)

func newTestVault(t *testing.T) *Vault {
	t.Helper()
	key := bytes.Repeat([]byte{0x42}, 32)
	v, err := New(key, 7)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestNewBadKey(t *testing.T) {
	if _, err := New([]byte("short"), 1); err == nil {
		t.Fatal("expected error for short key")
	}

	if _, err := New(bytes.Repeat([]byte{1}, 24), 1); err != nil {
		t.Fatal(err)
	}

	if _, err := New(bytes.Repeat([]byte{1}, 13), 1); err == nil {
		t.Fatal("expected aes error")
	}
}

func TestVaultRoundTrip(t *testing.T) {
	v := newTestVault(t)
	plain := []byte("notion_secret_xyz")
	env, err := v.Seal(plain)
	if err != nil {
		t.Fatal(err)
	}
	got, err := v.Open(env)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("plain mismatch: %q", got)
	}
	if v.Version() != 7 {
		t.Fatalf("version=%d", v.Version())
	}
	if v.NonceSize() != 12 {
		t.Fatalf("nonce size=%d, want 12", v.NonceSize())
	}
}

func TestOpenWrongKey(t *testing.T) {
	v := newTestVault(t)
	env, _ := v.Seal([]byte("x"))
	other, _ := New(bytes.Repeat([]byte{0x99}, 32), 1)
	if _, err := other.Open(env); err == nil {
		t.Fatal("expected open with wrong key to fail")
	}
}

func TestOpenShort(t *testing.T) {
	v := newTestVault(t)
	if _, err := v.Open([]byte("short")); !errors.Is(err, ErrCiphertextShort) {
		t.Fatalf("err=%v", err)
	}
}

func TestOpenTamperedTag(t *testing.T) {
	v := newTestVault(t)
	env, _ := v.Seal([]byte("payload"))
	env[len(env)-1] ^= 0xFF
	if _, err := v.Open(env); err == nil {
		t.Fatal("expected aead tag mismatch")
	}
}

func TestSealEmpty(t *testing.T) {
	v := newTestVault(t)
	env, err := v.Seal(nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(env) <= v.NonceSize() {
		t.Fatalf("envelope too small: %d", len(env))
	}
	got, err := v.Open(env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty plaintext, got %d bytes", len(got))
	}
}

func TestSealRandFails(t *testing.T) {
	v := newTestVault(t)
	v.rand = errReader{}
	if _, err := v.Seal([]byte("x")); err == nil {
		t.Fatal("expected rand error")
	}
}

func TestNewWithRand_NilDefaultsToCryptoRand(t *testing.T) {
	v, err := NewWithRand(bytes.Repeat([]byte{0x42}, 32), 9, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v.Version() != 9 {
		t.Fatal("version not stored")
	}

	if _, err := v.Seal([]byte("hi")); err != nil {
		t.Fatal(err)
	}
}

func TestNewWithRand_BadKey(t *testing.T) {
	if _, err := NewWithRand([]byte("bad"), 1, nil); err == nil {
		t.Fatal("expected key error")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func TestKeyLen(t *testing.T) {
	if _, err := aes.NewCipher(bytes.Repeat([]byte{0}, 32)); err != nil {
		t.Fatal(err)
	}
}
