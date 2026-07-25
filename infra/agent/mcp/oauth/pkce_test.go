package oauth

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNewPKCE(t *testing.T) {
	p, err := NewPKCE(bytes.NewReader(bytes.Repeat([]byte{0xAB}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Verifier) == 0 || len(p.Challenge) == 0 {
		t.Fatal("empty pkce")
	}

	p2, _ := NewPKCE(bytes.NewReader(bytes.Repeat([]byte{0xAB}, 32)))
	if p.Verifier != p2.Verifier {
		t.Fatal("verifier must be deterministic for same input")
	}
}

func TestNewPKCEReadFail(t *testing.T) {
	if _, err := NewPKCE(errReader{}); err == nil {
		t.Fatal("expected error")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func TestNewPKCENilReaderUsesCryptoRand(t *testing.T) {
	p, err := NewPKCE(nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Verifier == "" {
		t.Fatal("empty verifier")
	}
}

func TestSignerRoundTrip(t *testing.T) {
	now := time.Unix(1700000000, 0)
	s := NewSigner([]byte("k"), 0)
	s.now = func() time.Time { return now }
	tok, err := s.Sign(State{Kind: "builtin", WorkspaceID: "ws", BindingID: "b", Verifier: "v"})
	if err != nil {
		t.Fatal(err)
	}
	st, err := s.Verify(tok)
	if err != nil {
		t.Fatal(err)
	}
	if st.Kind != "builtin" || st.WorkspaceID != "ws" || st.BindingID != "b" || st.Verifier != "v" {
		t.Fatalf("%+v", st)
	}
	if st.Nonce == "" {
		t.Fatal("nonce missing")
	}
}

func TestVerifyExpired(t *testing.T) {
	s := NewSigner([]byte("k"), time.Minute)
	s.now = func() time.Time { return time.Unix(1000, 0) }
	tok, _ := s.Sign(State{Kind: "remote"})
	s.now = func() time.Time { return time.Unix(2000, 0) }
	if _, err := s.Verify(tok); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("err=%v", err)
	}
}

func TestVerifyMalformed(t *testing.T) {
	s := NewSigner([]byte("k"), time.Minute)
	cases := []string{"", "noseparator", ".sig", "payload.", "!!.@@"}
	for _, c := range cases {
		if _, err := s.Verify(c); err == nil {
			t.Fatalf("expected err for %q", c)
		}
	}
}

func TestVerifyBadSignature(t *testing.T) {
	a := NewSigner([]byte("a"), time.Minute)
	a.now = func() time.Time { return time.Unix(100, 0) }
	tok, _ := a.Sign(State{Kind: "x"})
	b := NewSigner([]byte("b"), time.Minute)
	b.now = a.now
	if _, err := b.Verify(tok); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("err=%v", err)
	}
}

func TestVerifyMissingIssuedAt(t *testing.T) {
	s := NewSigner([]byte("k"), time.Minute)
	s.now = func() time.Time { return time.Unix(0, 0) }
	tok, _ := s.Sign(State{})

	if _, err := s.Verify(tok); err == nil {
		t.Fatal("expected missing issuedAt")
	}
}
