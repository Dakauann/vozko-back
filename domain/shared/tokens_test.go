package shared

import "testing"

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
		{"accented text counts bytes not runes", "promoção", 2},
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
