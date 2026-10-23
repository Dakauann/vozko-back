package choicestats

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestATrackedButtonURLIsTheShortLinkWithTheMetaSuffixVariable(t *testing.T) {
	base := "https://vz.co/r"
	cases := []struct {
		name string
		url  string
		code string
		ok   bool
	}{
		{"the tracked form", "https://vz.co/r/Ab3_x-9/{{1}}", "Ab3_x-9", true},
		{"a trailing slash on the base changes nothing", "https://vz.co/r/Ab3x/{{1}}", "Ab3x", true},
		{"a static destination is not tracked", "https://loja.com/promo", "", false},
		{"the short link without the variable cannot carry the send", "https://vz.co/r/Ab3x", "", false},
		{"another host with the same path is not ours", "https://evil.co/r/Ab3x/{{1}}", "", false},
		{"a code with a slash is not a code", "https://vz.co/r/a/b/{{1}}", "", false},
		{"a dynamic url elsewhere is not tracked", "https://loja.com/{{1}}", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, ok := TrackedLinkCode(base, tc.url)
			if ok != tc.ok || code != tc.code {
				t.Fatalf("TrackedLinkCode = %q %v, want %q %v", code, ok, tc.code, tc.ok)
			}
		})
	}
	if _, ok := TrackedLinkCode("", "https://vz.co/r/Ab3x/{{1}}"); ok {
		t.Fatal("without a configured base nothing is tracked")
	}
	if got := TrackedButtonURL("https://vz.co/r/", "Ab3x"); got != "https://vz.co/r/Ab3x/{{1}}" {
		t.Fatalf("button url = %q", got)
	}
}

func testTokens(t *testing.T, secret string) *ClickTokens {
	t.Helper()
	tokens, err := NewClickTokens(secret)
	if err != nil {
		t.Fatal(err)
	}
	return tokens
}

func TestClickTokensNeedASecret(t *testing.T) {
	for _, secret := range []string{"", "   "} {
		if _, err := NewClickTokens(secret); !errors.Is(err, ErrClickTokenSecretMissing) {
			t.Fatalf("secret %q: err = %v", secret, err)
		}
	}
	var unwired *ClickTokens
	if _, err := unwired.Issue(0, "tpl:a"); !errors.Is(err, ErrClickTokenSecretMissing) {
		t.Fatalf("an unwired issuer = %v", err)
	}
	if _, _, ok := unwired.Verify("abc"); ok {
		t.Fatal("an unwired verifier accepted a token")
	}
}

func TestAClickTokenCarriesTheButtonAndThePromptBackWithoutShowingThePrompt(t *testing.T) {
	tokens := testTokens(t, "server-secret")
	key := WorkflowPromptKey("8f6c2a52-1b0f-4f7e-9a34-0d5f8f1e2c3b", "node_17#4")
	token, err := tokens.Issue(2, key)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(token, "/?#{}.= ") || len(token) > MaxClickTokenLength {
		t.Fatalf("token %q is not a compact url suffix", token)
	}
	for _, leak := range []string{key, "8f6c2a52", base64.RawURLEncoding.EncodeToString([]byte(key))[:12]} {
		if strings.Contains(token, leak) {
			t.Fatalf("token %q shows the prompt key", token)
		}
	}
	again, _ := tokens.Issue(2, key)
	if again != token {
		t.Fatal("the same send must always get the same token")
	}
	index, gotKey, ok := tokens.Verify(token)
	if !ok || index != 2 || gotKey != key {
		t.Fatalf("verified = %d %q %v", index, gotKey, ok)
	}
}

func TestOnlyATokenTheServerIssuedIsAccepted(t *testing.T) {
	tokens := testTokens(t, "server-secret")
	token, _ := tokens.Issue(1, "tpl:attempt-1")
	other, _ := testTokens(t, "another-secret").Issue(1, "tpl:attempt-1")
	raw, _ := base64.RawURLEncoding.DecodeString(token)
	flipped := append([]byte(nil), raw...)
	flipped[len(flipped)-1] ^= 0x01
	swappedButton := append([]byte(nil), raw...)
	swappedButton[clickTagSize] ^= 0x03
	forged := map[string]string{
		"empty":                      "",
		"not base64":                 "***",
		"truncated":                  token[:len(token)-4],
		"only the tag":               token[:16],
		"a flipped prompt byte":      base64.RawURLEncoding.EncodeToString(flipped),
		"another button":             base64.RawURLEncoding.EncodeToString(swappedButton),
		"signed with another secret": other,
		"the old readable format":    "1." + base64.RawURLEncoding.EncodeToString([]byte("tpl:attempt-1")),
		"too long":                   strings.Repeat("A", MaxClickTokenLength+1),
		"padded":                     token + "==",
	}
	for name, bad := range forged {
		if index, key, ok := tokens.Verify(bad); ok {
			t.Fatalf("%s: token %q was accepted as %d %q", name, bad, index, key)
		}
	}
	for _, refused := range []struct {
		index int
		key   string
	}{{-1, "tpl:a"}, {10, "tpl:a"}, {0, ""}, {0, strings.Repeat("k", MaxClickTokenLength)}} {
		if _, err := tokens.Issue(refused.index, refused.key); !errors.Is(err, ErrClickTokenInvalid) {
			t.Fatalf("issue %d %q = %v", refused.index, refused.key, err)
		}
	}
}

func TestOptionIDsOfTemplateButtons(t *testing.T) {
	if got := URLOptionID(3); got != "template_url:3" {
		t.Fatalf("url option = %q", got)
	}
	if got := TemplateButtonOptionID(1); got != "template_button:1" {
		t.Fatalf("button option = %q", got)
	}
}

func TestATrackedLinkTellsTheEditorWhatToSubmitToMeta(t *testing.T) {
	link := NewTrackedLink("https://vz.co/r/", "Ab3x", "sl-1", "https://loja.com/promo")
	if link.ButtonURL != "https://vz.co/r/Ab3x/{{1}}" || link.ShortLinkID != "sl-1" || link.Destination != "https://loja.com/promo" {
		t.Fatalf("link = %+v", link)
	}
	if code, ok := TrackedLinkCode("https://vz.co/r", link.ButtonURL); !ok || code != "Ab3x" {
		t.Fatal("the url handed to the editor must read back as tracked")
	}
	if !strings.HasPrefix(link.ExampleURL, "https://vz.co/r/Ab3x/") || strings.Contains(link.ExampleURL, "{{") {
		t.Fatalf("example = %q, Meta needs a filled sample", link.ExampleURL)
	}
}
