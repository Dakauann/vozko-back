package buttonaction

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"vozko/domain/shared"
)

const (
	MinExitWordLength = 1
	MaxExitWordLength = 30
	ExitCommandPrefix = "/"
)

func NormalizeExitWord(word string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(word), ExitCommandPrefix))
}

func FoldExitWord(word string) string {
	return shared.FoldForMatch(NormalizeExitWord(word))
}

func MatchesExitWord(message string, words []string) bool {
	command, ok := exitCommandOf(message)
	if !ok {
		return false
	}
	for _, word := range words {
		if FoldExitWord(word) == command {
			return true
		}
	}
	return false
}

func MayBeExitCommand(message string) bool {
	_, ok := exitCommandOf(message)
	return ok
}

func exitCommandOf(message string) (string, bool) {
	word, ok := strings.CutPrefix(shared.FoldForMatch(message), ExitCommandPrefix)
	if !ok || wordShape(word) != nil {
		return "", false
	}
	return word, true
}

func ValidateExitWord(word string) error {
	return wordShape(FoldExitWord(word))
}

func wordShape(folded string) error {
	length := utf8.RuneCountInString(folded)
	if length < MinExitWordLength || length > MaxExitWordLength {
		return ErrExitWordLength
	}
	if strings.HasPrefix(folded, ExitCommandPrefix) || strings.ContainsFunc(folded, unicode.IsSpace) {
		return ErrExitWordFormat
	}
	return nil
}
