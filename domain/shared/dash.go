package shared

import "strings"

const (
	enDash = rune(0x2013)
	emDash = rune(0x2014)
)

func HasLongDash(text string) bool {
	return strings.ContainsRune(text, enDash) || strings.ContainsRune(text, emDash)
}
