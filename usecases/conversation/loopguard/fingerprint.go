package loopguard

import (
	"crypto/sha1"
	"encoding/hex"
	"regexp"
	"strings"
	"unicode"
)

var urlRegex = regexp.MustCompile(`(?i)https?://\S+`)

var whitespaceRegex = regexp.MustCompile(`\s+`)

const maxFingerprintInput = 512

func Fingerprint(text string) string {
	norm := normalize(text)
	if norm == "" {
		return ""
	}
	sum := sha1.Sum([]byte(norm))
	return hex.EncodeToString(sum[:])[:16]
}

func normalize(text string) string {
	if text == "" {
		return ""
	}

	s := strings.ToLower(text)
	s = urlRegex.ReplaceAllString(s, " ")

	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(r)
		case unicode.IsSpace(r):
			b.WriteRune(' ')
		default:

		}
	}
	s = whitespaceRegex.ReplaceAllString(b.String(), " ")
	s = strings.TrimSpace(s)
	if len(s) > maxFingerprintInput {
		s = s[:maxFingerprintInput]
	}
	return s
}
