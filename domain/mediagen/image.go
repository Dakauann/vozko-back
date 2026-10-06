package mediagen

import (
	"fmt"
	"strings"
)

type Aspect string

const (
	AspectSquare    Aspect = "square"
	AspectPortrait  Aspect = "portrait"
	AspectStory     Aspect = "story"
	AspectLandscape Aspect = "landscape"
)

type Size struct {
	Width  int
	Height int
}

var aspectSizes = map[Aspect]Size{
	AspectSquare:    {1080, 1080},
	AspectPortrait:  {1080, 1350},
	AspectStory:     {1080, 1920},
	AspectLandscape: {1920, 1080},
}

func (a Aspect) Size() (Size, error) {
	size, ok := aspectSizes[a]
	if !ok {
		return Size{}, fmt.Errorf("mediagen: unknown aspect %q", a)
	}
	return size, nil
}

func aspectIssues(a Aspect) []FieldIssue {
	switch _, err := a.Size(); {
	case a == "":
		return []FieldIssue{{Field: FieldAspect, Code: CodeRequired}}
	case err != nil:
		return []FieldIssue{{Field: FieldAspect, Code: CodeUnknown}}
	}
	return nil
}

const (
	MaxPromptRunes     = 4000
	MaxReferenceImages = 16
)

func imageIssues(r Request) []FieldIssue {
	issues := append(textIssues(FieldPrompt, r.Prompt, MaxPromptRunes), aspectIssues(r.Aspect)...)
	if code := referencesIssue(r.ReferenceMediaIDs); code != "" {
		issues = append(issues, FieldIssue{Field: FieldReferences, Code: code})
	}
	return issues
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
