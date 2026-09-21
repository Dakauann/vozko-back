package shared

import "testing"

func TestFoldForMatch(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"whitespace only", "   ", ""},
		{"lowercases", "PROMOCAO", "promocao"},
		{"trims", "  quero  ", "quero"},
		{"folds pt-BR accents", "promoção", "promocao"},
		{"folds uppercase accents", "PROMOÇÃO", "promocao"},
		{"folds a full topic label", "Saúde Pública", "saude publica"},
		{"folds es accents", "mañana señor", "manana senor"},
		{"folds diaeresis and circumflex", "Müller ê", "muller e"},
		{"keeps inner spaces", "asfalto e pavimentacao", "asfalto e pavimentacao"},
		{"leaves punctuation and digits", "r$ 10,00!", "r$ 10,00!"},
		{"leaves unmapped letters alone", "ß ø", "ß ø"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FoldForMatch(tc.in); got != tc.want {
				t.Errorf("FoldForMatch(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestFoldForMatch_CollapsesSpellings(t *testing.T) {
	pairs := [][2]string{
		{"Saúde Pública", "saude publica"},
		{"promoção", "PROMOCAO"},
		{"Educação", "educacao"},
	}
	for _, p := range pairs {
		if FoldForMatch(p[0]) != FoldForMatch(p[1]) {
			t.Errorf("%q and %q should fold to the same key", p[0], p[1])
		}
	}
}
