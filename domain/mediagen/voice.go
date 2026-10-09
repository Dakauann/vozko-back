package mediagen

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
)

const (
	MaxMusicPromptRunes = 2000
	MaxScriptRunes      = 1500
)

var voiceToken = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

func SpokenAsWritten(script, transcript string) bool {
	written := spokenWords(script)
	return len(written) > 0 && slices.Equal(written, spokenWords(transcript))
}

func spokenWords(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

func voiceIssues(r Request) []FieldIssue {
	issues := textIssues(FieldPrompt, r.Prompt, MaxScriptRunes)
	if voice := strings.TrimSpace(r.Voice); voice != "" && !voiceToken.MatchString(voice) {
		issues = append(issues, FieldIssue{Field: FieldVoice, Code: CodeUnknown})
	}
	return issues
}
