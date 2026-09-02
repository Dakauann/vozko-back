package shared

import "testing"

// EstimateTokens was moved verbatim from the RAG document processor. Its
// output is baked into every chunk's stored token_count, so the fixture below
// pins the exact numbers: a "better" estimate here would silently disagree
// with existing embeddings.
func TestEstimateTokens_FixtureCorpus(t *testing.T) {
	cases := []struct {
		name string
		text string
		want int
	}{
		{"empty is zero", "", 0},
		{"one byte rounds up to one", "a", 1},
		{"three bytes round up to one", "abc", 1},
		{"four bytes is one", "abcd", 1},
		{"seven bytes is one", "abcdefg", 1},
		{"eight bytes is two", "abcdefgh", 2},
		{"forty-three ascii bytes is ten", "The quick brown fox jumps over the lazy dog", 10},
		// len() counts BYTES, so accented pt-BR over-counts slightly. That is
		// the safe direction for chunking and must not be "fixed" here; a
		// consumer that needs a different bias applies its own factor.
		{"accented text counts bytes not runes", "promoção", 2}, // 10 bytes, 8 runes
		{"multi-line pt-BR", "Olá, tudo bem?\nQuero saber o preço da promoção.", 12},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EstimateTokens(tc.text); got != tc.want {
				t.Errorf("EstimateTokens(%q) = %d, want %d", tc.text, got, tc.want)
			}
		})
	}
}

func TestEstimateTokens_NeverZeroForNonEmpty(t *testing.T) {
	for _, s := range []string{" ", "é", "ab", "\n"} {
		if EstimateTokens(s) < 1 {
			t.Errorf("EstimateTokens(%q) must be at least 1", s)
		}
	}
}
