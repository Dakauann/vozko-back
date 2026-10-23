package emailtoken

import (
	"errors"
	"strings"
	"testing"
)

const secret = "0123456789abcdef0123456789abcdef-test"
const id = "0b6f2c1e-1111-4a2b-9c3d-2e4f5a6b7c8d"

func TestNewCodecRefusesShortSecrets(t *testing.T) {
	if _, err := NewCodec("short"); !errors.Is(err, ErrWeakSecret) {
		t.Fatalf("NewCodec short = %v", err)
	}
	if _, err := NewCodec(strings.Repeat(" ", 40)); !errors.Is(err, ErrWeakSecret) {
		t.Fatalf("NewCodec blank = %v", err)
	}
}

func TestSealOpenRoundTrip(t *testing.T) {
	c, err := NewCodec(secret)
	if err != nil {
		t.Fatal(err)
	}
	for _, purpose := range []Purpose{PurposeReply, PurposeUnsubscribe} {
		token, err := c.Seal(purpose, id)
		if err != nil {
			t.Fatal(err)
		}
		if len(token) > 58 {
			t.Fatalf("token %q is %d chars, must fit a local part with its prefix", token, len(token))
		}
		if strings.ToLower(token) != token {
			t.Fatalf("token %q must be lower case so case folding mailers keep it valid", token)
		}
		got, err := c.Open(purpose, token)
		if err != nil || got != id {
			t.Fatalf("Open = %q, %v", got, err)
		}
		upper, err := c.Open(purpose, strings.ToUpper(token))
		if err != nil || upper != id {
			t.Fatalf("Open upper = %q, %v", upper, err)
		}
	}
}

func TestOpenRefusesTampering(t *testing.T) {
	c, _ := NewCodec(secret)
	other, _ := NewCodec(secret + "-other")
	token, _ := c.Seal(PurposeReply, id)
	flipped := []byte(token)
	if flipped[3] == 'a' {
		flipped[3] = 'b'
	} else {
		flipped[3] = 'a'
	}
	cases := []struct {
		name    string
		codec   *Codec
		purpose Purpose
		token   string
	}{
		{name: "another purpose", codec: c, purpose: PurposeUnsubscribe, token: token},
		{name: "another secret", codec: other, purpose: PurposeReply, token: token},
		{name: "flipped character", codec: c, purpose: PurposeReply, token: string(flipped)},
		{name: "truncated", codec: c, purpose: PurposeReply, token: token[:len(token)-2]},
		{name: "empty", codec: c, purpose: PurposeReply, token: ""},
		{name: "garbage", codec: c, purpose: PurposeReply, token: "!!!"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.codec.Open(tc.purpose, tc.token); !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("Open = %v, want ErrInvalidToken", err)
			}
		})
	}
}

func TestSealRefusesNonUUID(t *testing.T) {
	c, _ := NewCodec(secret)
	if _, err := c.Seal(PurposeReply, "not-a-uuid"); err == nil {
		t.Fatal("Seal must refuse ids that are not UUIDs")
	}
	if _, err := c.Seal(Purpose("x"), id); err == nil {
		t.Fatal("Seal must refuse unknown purposes")
	}
}

func TestTaggedAddress(t *testing.T) {
	c, _ := NewCodec(secret)
	addr, err := c.TaggedAddress(PurposeReply, id, "reply.loja.com.br")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(addr), "reply+") || !strings.HasSuffix(string(addr), "@reply.loja.com.br") {
		t.Fatalf("TaggedAddress = %q", addr)
	}
	purpose, gotID, err := c.OpenAddress(addr)
	if err != nil || purpose != PurposeReply || gotID != id {
		t.Fatalf("OpenAddress = %q %q %v", purpose, gotID, err)
	}
	unsub, _ := c.TaggedAddress(PurposeUnsubscribe, id, "joao.vozkomail.com.br")
	if !strings.HasPrefix(string(unsub), "unsubscribe+") {
		t.Fatalf("unsubscribe address = %q", unsub)
	}
	if _, _, err := c.OpenAddress("contato@loja.com.br"); !errors.Is(err, ErrNotTagged) {
		t.Fatalf("plain address = %v", err)
	}
	if _, _, err := c.OpenAddress("reply+zzzz@loja.com.br"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("bad tag = %v", err)
	}
}
