package mediagen

import (
	"regexp"
	"strings"
)

const (
	MaxMusicPromptRunes = 2000
	MaxScriptRunes      = 1500
)

var voiceToken = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

func voiceIssues(r Request) []FieldIssue {
	issues := textIssues(FieldPrompt, r.Prompt, MaxScriptRunes)
	if voice := strings.TrimSpace(r.Voice); voice != "" && !voiceToken.MatchString(voice) {
		issues = append(issues, FieldIssue{Field: FieldVoice, Code: CodeUnknown})
	}
	return issues
}
