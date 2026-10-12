package shared

import "testing"

func TestEditDistanceCountsRuneEdits(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"", "abc", 3},
		{"efeitos", "efeitos", 0},
		{"efeito", "efeitos", 1},
		{"forma", "formas", 1},
		{"transição", "transicao", 2},
		{"kitten", "sitting", 3},
	}
	for _, c := range cases {
		if got := EditDistance(c.a, c.b); got != c.want {
			t.Errorf("EditDistance(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
		if got := EditDistance(c.b, c.a); got != c.want {
			t.Errorf("EditDistance(%q, %q) = %d, want %d", c.b, c.a, got, c.want)
		}
	}
}
