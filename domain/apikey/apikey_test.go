package apikey

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestGenerateYieldsAKeyWhosePrefixFindsItAndWhoseHashMatchesOnlyItself(t *testing.T) {
	secret, err := Generate(bytes.NewReader(bytes.Repeat([]byte{7}, 64)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(secret.Plain, TokenPrefix) || len(secret.LookupID) != lookupIDLength {
		t.Fatalf("secret = %+v", secret)
	}
	lookup, ok := Parse(secret.Plain)
	if !ok || lookup != secret.LookupID {
		t.Fatalf("parse = %q %v, want %q", lookup, ok, secret.LookupID)
	}
	if !Matches(secret.Hash, secret.Plain) {
		t.Fatal("the key does not match its own hash")
	}
	if Matches(secret.Hash, secret.Plain+"x") || Matches(secret.Hash, strings.Replace(secret.Plain, secret.LookupID, strings.Repeat("a", lookupIDLength), 1)) {
		t.Fatal("another token matched the hash")
	}
	if strings.Contains(secret.Hash, secret.Plain) || len(secret.Hash) != 64 {
		t.Fatalf("hash %q must be a sha256 hex digest", secret.Hash)
	}
}

func TestGenerateRefusesAShortRandomSource(t *testing.T) {
	if _, err := Generate(bytes.NewReader([]byte{1, 2, 3})); err == nil {
		t.Fatal("a short random source must fail")
	}
}

func TestParseRefusesMalformedTokens(t *testing.T) {
	for _, token := range []string{"", "wsk_", "Bearer x", "wsk_abc_def", "sk_" + strings.Repeat("a", 12) + "_" + strings.Repeat("b", 43), "wsk_" + strings.Repeat("a", 12) + "_" + strings.Repeat("b", 42), "wsk_" + strings.Repeat("a", 11) + "!_" + strings.Repeat("b", 43)} {
		if _, ok := Parse(token); ok {
			t.Fatalf("Parse(%q) accepted a malformed token", token)
		}
	}
}

func TestPrefixIsTheVisiblePartOnly(t *testing.T) {
	k := Key{LookupID: "abcdefghijkl"}
	if k.Prefix() != "wsk_abcdefghijkl" {
		t.Fatalf("prefix = %q", k.Prefix())
	}
}

func TestValidateScopesNeedsKnownDistinctScopes(t *testing.T) {
	cases := []struct {
		in   []string
		want []Scope
		err  bool
	}{
		{in: []string{"email:send"}, want: []Scope{ScopeEmailSend}},
		{in: []string{"email:read", "email:send", "email:read"}, want: []Scope{ScopeEmailRead, ScopeEmailSend}},
		{in: nil, err: true},
		{in: []string{"email:delete"}, err: true},
		{in: []string{" email:send"}, err: true},
	}
	for _, c := range cases {
		got, err := ValidateScopes(c.in)
		if (err != nil) != c.err {
			t.Fatalf("ValidateScopes(%v) err = %v", c.in, err)
		}
		if c.err {
			if !errors.Is(err, ErrScopesInvalid) {
				t.Fatalf("err = %v, want ErrScopesInvalid", err)
			}
			continue
		}
		if len(got) != len(c.want) {
			t.Fatalf("ValidateScopes(%v) = %v, want %v", c.in, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("ValidateScopes(%v) = %v, want %v", c.in, got, c.want)
			}
		}
	}
}

func TestEveryCatalogScopeBelongsToItsProduct(t *testing.T) {
	seen := map[Scope]bool{}
	for _, p := range Catalog {
		if len(p.Scopes) == 0 {
			t.Fatalf("product %q has no scopes", p.Key)
		}
		for _, s := range p.Scopes {
			if !strings.HasPrefix(string(s.Scope), p.Key+":") || seen[s.Scope] {
				t.Fatalf("scope %q does not belong to %q or repeats", s.Scope, p.Key)
			}
			seen[s.Scope] = true
		}
	}
	if !seen[ScopeEmailSend] || !seen[ScopeEmailRead] {
		t.Fatal("the email scopes are missing from the catalog")
	}
}

func TestValidateName(t *testing.T) {
	if got, err := ValidateName("  Produção  "); err != nil || got != "Produção" {
		t.Fatalf("ValidateName = %q %v", got, err)
	}
	for _, bad := range []string{"", "   ", "a\nb", strings.Repeat("x", 81)} {
		if _, err := ValidateName(bad); !errors.Is(err, ErrNameInvalid) {
			t.Fatalf("ValidateName(%q) = %v", bad, err)
		}
	}
}

func TestPrincipalAllowsOnlyItsScopes(t *testing.T) {
	p := Principal{WorkspaceID: "ws", KeyID: "k", Scopes: []Scope{ScopeEmailSend}}
	if !p.Allows(ScopeEmailSend) || p.Allows(ScopeEmailRead) {
		t.Fatalf("principal %+v", p)
	}
}

func TestUsableRefusesRevokedKeys(t *testing.T) {
	now := time.Now()
	if err := (Key{}).Usable(); err != nil {
		t.Fatal(err)
	}
	if err := (Key{RevokedAt: &now}).Usable(); !errors.Is(err, ErrRevoked) {
		t.Fatalf("revoked key = %v", err)
	}
}

func TestDueForTouchOncePerInterval(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-30 * time.Second)
	old := now.Add(-2 * time.Minute)
	if !(Key{}).DueForTouch(now) || (Key{LastUsedAt: &recent}).DueForTouch(now) || !(Key{LastUsedAt: &old}).DueForTouch(now) {
		t.Fatal("touch must happen at most once per interval")
	}
}
