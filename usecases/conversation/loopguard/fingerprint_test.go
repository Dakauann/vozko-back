package loopguard

import (
	"strings"
	"testing"
)

func TestFingerprint_StabilityAndNormalization(t *testing.T) {
	t.Parallel()

	t.Run("identical inputs produce identical fingerprints", func(t *testing.T) {
		a := Fingerprint("Olá, tudo bem?")
		b := Fingerprint("Olá, tudo bem?")
		if a == "" || a != b {
			t.Fatalf("expected stable fingerprint, got %q vs %q", a, b)
		}
	})

	t.Run("case differences are ignored", func(t *testing.T) {
		a := Fingerprint("HELLO world")
		b := Fingerprint("hello WORLD")
		if a != b {
			t.Fatalf("expected case-insensitive match: %q vs %q", a, b)
		}
	})

	t.Run("whitespace differences are ignored", func(t *testing.T) {
		a := Fingerprint("foo bar")
		b := Fingerprint("  foo\t\nbar  ")
		if a != b {
			t.Fatalf("expected whitespace-insensitive match: %q vs %q", a, b)
		}
	})

	t.Run("punctuation and emoji are stripped", func(t *testing.T) {
		a := Fingerprint("ola mundo")
		b := Fingerprint("ola, mundo!!! 🎉🥳")
		if a != b {
			t.Fatalf("expected punctuation/emoji-insensitive match: %q vs %q", a, b)
		}
	})

	t.Run("URLs are erased so tracking tokens don't escape detection", func(t *testing.T) {

		a := Fingerprint("Segue o link: https://example.com/x?a=1")
		b := Fingerprint("Segue o link: https://example.com/y?a=2")
		if a != b {
			t.Fatalf("expected URL-insensitive match: %q vs %q", a, b)
		}
	})

	t.Run("different content produces different fingerprints", func(t *testing.T) {
		a := Fingerprint("regularize seu cartão hoje")
		b := Fingerprint("segue o link para o cardápio")
		if a == b {
			t.Fatalf("expected distinct fingerprints, both are %q", a)
		}
	})

	t.Run("empty / whitespace / punctuation-only inputs yield empty fingerprint", func(t *testing.T) {
		for _, in := range []string{"", "   ", "\t\n\r", "!!!", "—", "...", "🎉🎉🎉"} {
			if got := Fingerprint(in); got != "" {
				t.Fatalf("expected empty fingerprint for %q, got %q", in, got)
			}
		}
	})

	t.Run("very long inputs are bounded", func(t *testing.T) {

		prefix := strings.Repeat("a", maxFingerprintInput+10)
		a := Fingerprint(prefix + "tail-one")
		b := Fingerprint(prefix + "tail-two")
		if a != b {
			t.Fatalf("expected long-input cap to collapse tails: %q vs %q", a, b)
		}
	})

	t.Run("fingerprint length is fixed and hex", func(t *testing.T) {
		fp := Fingerprint("alguma mensagem")
		if len(fp) != 16 {
			t.Fatalf("expected fixed 16-char fingerprint, got %d (%q)", len(fp), fp)
		}
		for _, r := range fp {
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
				t.Fatalf("expected lowercase hex, got %q", fp)
			}
		}
	})
}
