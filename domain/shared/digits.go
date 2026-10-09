package shared

import "strings"

func DigitsOf(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func AllDigits(s string) bool {
	return s != "" && onlyDigits(s)
}
