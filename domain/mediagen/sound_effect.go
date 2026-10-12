package mediagen

const (
	MaxSoundEffectPromptRunes = 1000
	MinSoundEffectMS          = 500
	MaxSoundEffectMS          = 22_000
)

func soundEffectIssues(r Request) []FieldIssue {
	issues := textIssues(FieldPrompt, r.Prompt, MaxSoundEffectPromptRunes)
	switch {
	case r.DurationMS == 0:
		issues = append(issues, FieldIssue{Field: FieldDuration, Code: CodeRequired})
	case r.DurationMS < MinSoundEffectMS || r.DurationMS > MaxSoundEffectMS:
		issues = append(issues, FieldIssue{Field: FieldDuration, Code: CodeOutOfRange})
	}
	return issues
}
