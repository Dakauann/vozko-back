package imagegen

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

type Aspect string

const (
	AspectSquare   Aspect = "square"
	AspectPortrait Aspect = "portrait"
	AspectStory    Aspect = "story"
)

type Size struct {
	Width  int
	Height int
}

var aspectSizes = map[Aspect]Size{
	AspectSquare:   {1080, 1080},
	AspectPortrait: {1080, 1350},
	AspectStory:    {1080, 1920},
}

func (a Aspect) Size() (Size, error) {
	size, ok := aspectSizes[a]
	if !ok {
		return Size{}, fmt.Errorf("imagegen: unknown aspect %q", a)
	}
	return size, nil
}

const (
	MaxPromptRunes     = 4000
	MaxReferenceImages = 16
)

type Request struct {
	WorkspaceID       string
	Prompt            string
	Aspect            Aspect
	ReferenceMediaIDs []string
}

type ReferenceImage struct {
	MediaID string
	URL     string
}

func (r Request) Validate() error {
	if strings.TrimSpace(r.WorkspaceID) == "" {
		return ErrWorkspaceRequired
	}
	var issues []FieldIssue
	prompt := strings.TrimSpace(r.Prompt)
	switch {
	case prompt == "":
		issues = append(issues, FieldIssue{Field: FieldPrompt, Code: CodeRequired})
	case utf8.RuneCountInString(prompt) > MaxPromptRunes:
		issues = append(issues, FieldIssue{Field: FieldPrompt, Code: CodeTooLong})
	}
	switch _, err := r.Aspect.Size(); {
	case r.Aspect == "":
		issues = append(issues, FieldIssue{Field: FieldAspect, Code: CodeRequired})
	case err != nil:
		issues = append(issues, FieldIssue{Field: FieldAspect, Code: CodeUnknown})
	}
	if code := referencesIssue(r.ReferenceMediaIDs); code != "" {
		issues = append(issues, FieldIssue{Field: FieldReferences, Code: code})
	}
	if len(issues) > 0 {
		return &ValidationError{Issues: issues}
	}
	return nil
}

func referencesIssue(ids []string) string {
	if len(ids) > MaxReferenceImages {
		return CodeTooMany
	}
	seen := make(map[string]struct{}, len(ids))
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if id == "" {
			return CodeRequired
		}
		if _, dup := seen[id]; dup {
			return CodeDuplicate
		}
		seen[id] = struct{}{}
	}
	return ""
}

func trimmedReferences(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = strings.TrimSpace(id)
	}
	return out
}

type GeneratedImage struct {
	Bytes              []byte
	MIMEType           string
	Model              string
	ProviderCostMicros int64
}
