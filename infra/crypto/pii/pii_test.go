package pii

import (
	"bytes"
	"errors"
	"testing"

	"vozko/infra/crypto/vault"
)

func newKey(t *testing.T, b byte) []byte {
	t.Helper()
	return bytes.Repeat([]byte{b}, 32)
}

func newVault(t *testing.T, b byte, ver int) *vault.Vault {
	t.Helper()
	v, err := vault.New(newKey(t, b), ver)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func newSvc(t *testing.T) *Service {
	t.Helper()
	s, err := New(
		map[byte]*vault.Vault{1: newVault(t, 0x01, 1)},
		1,
		bytes.Repeat([]byte{0xAB}, 32),
	)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNew_Errors(t *testing.T) {
	cases := []struct {
		name      string
		vaults    map[byte]*vault.Vault
		active    byte
		blindKey  []byte
		wantErrIs error
	}{
		{"empty keyring", nil, 1, bytes.Repeat([]byte{1}, 32), ErrNoActiveKEK},
		{"nil entry", map[byte]*vault.Vault{1: nil}, 1, bytes.Repeat([]byte{1}, 32), ErrNilVault},
		{
			"missing active version",
			map[byte]*vault.Vault{1: newVault(t, 1, 1)},
			2,
			bytes.Repeat([]byte{1}, 32),
			ErrNoActiveKEK,
		},
		{
			"short blind key",
			map[byte]*vault.Vault{1: newVault(t, 1, 1)},
			1,
			[]byte("too-short"),
			ErrBlindIndexKey,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(tc.vaults, tc.active, tc.blindKey)
			if !errors.Is(err, tc.wantErrIs) {
				t.Fatalf("err=%v want %v", err, tc.wantErrIs)
			}
		})
	}
}

func TestNew_CopiesBlindKey(t *testing.T) {
	key := bytes.Repeat([]byte{0xCD}, 32)
	s, err := New(map[byte]*vault.Vault{1: newVault(t, 1, 1)}, 1, key)
	if err != nil {
		t.Fatal(err)
	}
	before := s.BlindIndex("scope", "v")

	for i := range key {
		key[i] = 0
	}
	after := s.BlindIndex("scope", "v")
	if !bytes.Equal(before, after) {
		t.Fatal("Service must defensively copy the blind-index key")
	}
}

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	s := newSvc(t)
	plain := []byte("12345678900")
	ct, err := s.Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	if ct[0] != EnvelopeFormatV1 {
		t.Fatalf("format byte=%x", ct[0])
	}
	if ct[1] != 1 {
		t.Fatalf("kek version byte=%d", ct[1])
	}
	got, err := s.Decrypt(ct)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("plain mismatch: %q", got)
	}
}

func TestEncrypt_NonDeterministic(t *testing.T) {
	s := newSvc(t)
	a, _ := s.Encrypt([]byte("same"))
	b, _ := s.Encrypt([]byte("same"))
	if bytes.Equal(a, b) {
		t.Fatal("same plaintext must encrypt to different ciphertexts (nonce)")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("simulated rand failure") }

func TestEncrypt_VaultSealError(t *testing.T) {
	s := newSvc(t)

	broken, err := vault.NewWithRand(bytes.Repeat([]byte{1}, 32), 1, errReader{})
	if err != nil {
		t.Fatal(err)
	}
	s.vaults[1] = broken
	if _, err := s.Encrypt([]byte("x")); err == nil {
		t.Fatal("expected vault.Seal failure to propagate")
	}
}

func TestDecrypt_Errors(t *testing.T) {
	s := newSvc(t)
	if _, err := s.Decrypt([]byte{0x01}); !errors.Is(err, ErrEnvelopeShort) {
		t.Fatalf("short envelope err=%v", err)
	}
	if _, err := s.Decrypt([]byte{0x02, 0x01, 0x00, 0x00}); !errors.Is(err, ErrUnknownFormat) {
		t.Fatalf("unknown format err=%v", err)
	}
	if _, err := s.Decrypt([]byte{EnvelopeFormatV1, 99, 0x00, 0x00}); !errors.Is(err, ErrUnknownKEKVersion) {
		t.Fatalf("unknown kek err=%v", err)
	}

	bad := append([]byte{EnvelopeFormatV1, 1}, bytes.Repeat([]byte{0}, 32)...)
	if _, err := s.Decrypt(bad); err == nil {
		t.Fatal("expected AEAD failure")
	}
}

func TestDecrypt_AcrossKEKVersions(t *testing.T) {
	v1 := newVault(t, 1, 1)
	v2 := newVault(t, 2, 2)
	blind := bytes.Repeat([]byte{0xEE}, 32)

	sV1, _ := New(map[byte]*vault.Vault{1: v1}, 1, blind)
	sBoth, _ := New(map[byte]*vault.Vault{1: v1, 2: v2}, 2, blind)

	ct, err := sV1.Encrypt([]byte("legacy row"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := sBoth.Decrypt(ct)
	if err != nil {
		t.Fatalf("multi-version decrypt failed: %v", err)
	}
	if !bytes.Equal(got, []byte("legacy row")) {
		t.Fatalf("plain mismatch: %q", got)
	}
	if sBoth.ActiveKEKVersion() != 2 {
		t.Fatalf("active=%d", sBoth.ActiveKEKVersion())
	}

	ct2, _ := sBoth.Encrypt([]byte("new row"))
	if ct2[1] != 2 {
		t.Fatalf("new ciphertext kek byte=%d, want 2", ct2[1])
	}
}

func TestBlindIndex_Properties(t *testing.T) {
	s := newSvc(t)
	a := s.BlindIndex("users.cpf", "12345678900")
	b := s.BlindIndex("users.cpf", "12345678900")
	c := s.BlindIndex("customers.cpf", "12345678900")
	d := s.BlindIndex("users.cpf", "00000000000")

	if len(a) != 32 {
		t.Fatalf("blind index len=%d, want 32", len(a))
	}
	if !bytes.Equal(a, b) {
		t.Fatal("blind index must be deterministic")
	}
	if bytes.Equal(a, c) {
		t.Fatal("scope must change the index")
	}
	if bytes.Equal(a, d) {
		t.Fatal("value must change the index")
	}
}

func TestBlindIndex_SeparatorPreventsAmbiguity(t *testing.T) {
	s := newSvc(t)

	x := s.BlindIndex("ab", "c")
	y := s.BlindIndex("a", "bc")
	if bytes.Equal(x, y) {
		t.Fatal("scope/value boundary must be unambiguous")
	}
}
