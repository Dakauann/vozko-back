package shared

import "strings"

// accentFolds maps the accented letters that appear in pt-BR and es to their
// bare form. Kept as an explicit table rather than pulling in a Unicode
// normalization dependency: the alphabet a commenter can type here is small and
// known, and the table is trivial to extend.
var accentFolds = map[rune]rune{
	'á': 'a', 'à': 'a', 'ã': 'a', 'â': 'a', 'ä': 'a',
	'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e',
	'í': 'i', 'ì': 'i', 'î': 'i', 'ï': 'i',
	'ó': 'o', 'ò': 'o', 'õ': 'o', 'ô': 'o', 'ö': 'o',
	'ú': 'u', 'ù': 'u', 'û': 'u', 'ü': 'u',
	'ç': 'c', 'ñ': 'n',
}

// FoldForMatch makes text matching forgiving in the way users expect: case-
// and accent-insensitive, so "promoção", "PROMOCAO" and "Promocao" are one
// keyword and "Saúde Pública" and "saude publica" are one topic. A Brazilian
// audience types all of these, and a match that only recognised the accented
// spelling would silently miss most of them.
func FoldForMatch(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if folded, ok := accentFolds[r]; ok {
			b.WriteRune(folded)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
