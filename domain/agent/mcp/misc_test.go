package mcp

import (
	"errors"
	"testing"
	"time"
)

func TestCredentialShouldRefresh(t *testing.T) {
	now := time.Unix(1000, 0)
	var nilCred *Credential
	if nilCred.ShouldRefresh(now) {
		t.Fatal("nil should not refresh")
	}
	c := &Credential{Mode: AuthAPIKey}
	if c.ShouldRefresh(now) {
		t.Fatal("api key mode never refreshes")
	}
	past := now.Add(-time.Minute)
	future := now.Add(time.Minute)
	c2 := &Credential{Mode: AuthOAuth2, RefreshHint: &past}
	if !c2.ShouldRefresh(now) {
		t.Fatal("past hint should refresh")
	}
	c3 := &Credential{Mode: AuthOAuth2, RefreshHint: &future}
	if c3.ShouldRefresh(now) {
		t.Fatal("future hint should not refresh")
	}
	c4 := &Credential{Mode: AuthOAuth2}
	if c4.ShouldRefresh(now) {
		t.Fatal("nil hint must not refresh")
	}
}

func TestCredentialExpired(t *testing.T) {
	now := time.Unix(1000, 0)
	var nilCred *Credential
	if nilCred.Expired(now) {
		t.Fatal("nil cannot expire")
	}
	c := &Credential{}
	if c.Expired(now) {
		t.Fatal("nil expires-at cannot expire")
	}
	past := now.Add(-time.Second)
	future := now.Add(time.Second)
	if !(&Credential{ExpiresAt: &past}).Expired(now) {
		t.Fatal("past expiry")
	}
	if (&Credential{ExpiresAt: &future}).Expired(now) {
		t.Fatal("future should not be expired")
	}
}

func TestTextAndErrorResult(t *testing.T) {
	r := TextResult("hi")
	if r.IsError || len(r.Content) != 1 || r.Content[0].Text != "hi" {
		t.Fatal("text result shape")
	}
	e := ErrorResult("no")
	if !e.IsError || e.Content[0].Text != "no" {
		t.Fatal("error result shape")
	}
}

func TestParseQualifiedName(t *testing.T) {
	cases := []struct {
		in   string
		ok   bool
		kind Kind
		src  string
		tool string
	}{
		{"builtin:notion.search", true, KindBuiltin, "notion", "search"},
		{"remote:srv_abc.create_issue", true, KindRemote, "srv_abc", "create_issue"},
		{"builtin:notion.pages.create", true, KindBuiltin, "notion", "pages.create"},
		{"bad", false, "", "", ""},
		{"weird:notion.search", false, "", "", ""},
		{":notion.search", false, "", "", ""},
		{"builtin:.search", false, "", "", ""},
		{"builtin:notion.", false, "", "", ""},
		{"builtin:notion", false, "", "", ""},
	}
	for _, tc := range cases {
		q, err := ParseQualifiedName(tc.in)
		if tc.ok {
			if err != nil {
				t.Fatalf("%q: err=%v", tc.in, err)
			}
			if q.Kind != tc.kind || q.SourceID != tc.src || q.Tool != tc.tool {
				t.Fatalf("%q -> %+v", tc.in, q)
			}
			if q.String() != tc.in {
				t.Fatalf("roundtrip %q -> %q", tc.in, q.String())
			}
		} else {
			if !errors.Is(err, ErrToolNameMalformed) {
				t.Fatalf("%q: err=%v", tc.in, err)
			}
		}
	}
}

func TestNewCachedTool(t *testing.T) {
	orig := Now
	fixed := time.Unix(1234, 0)
	Now = func() time.Time { return fixed }
	t.Cleanup(func() { Now = orig })

	c := NewCachedTool("builtin:notion", "ws", Tool{
		Name:        "search",
		Title:       "Search",
		Description: "d",
		InputSchema: []byte(`{"type":"object"}`),
	})
	if c.SourceID != "builtin:notion" || c.Name != "search" {
		t.Fatalf("%+v", c)
	}
	if c.Hash == "" {
		t.Fatal("hash must be set")
	}
	if !c.RefreshedAt.Equal(fixed) {
		t.Fatalf("refreshedAt=%v", c.RefreshedAt)
	}

	schema := []byte(`{"type":"object"}`)
	c2 := NewCachedTool("s", "w", Tool{Name: "n", InputSchema: schema})
	schema[0] = 'X'
	if c2.InputSchema[0] != '{' {
		t.Fatal("cached schema must be copied")
	}
}
