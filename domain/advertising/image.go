package advertising

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
		return Size{}, fmt.Errorf("advertising: unknown aspect %q", a)
	}
	return size, nil
}

const maxImagePromptRunes = 4000

type ImageRequest struct {
	WorkspaceID string
	Prompt      string
	Aspect      Aspect
}

func (r ImageRequest) Validate() error {
	if strings.TrimSpace(r.WorkspaceID) == "" {
		return ErrWorkspaceRequired
	}
	prompt := strings.TrimSpace(r.Prompt)
	if prompt == "" || utf8.RuneCountInString(prompt) > maxImagePromptRunes {
		return fmt.Errorf("advertising: image prompt must have 1 to %d characters", maxImagePromptRunes)
	}
	_, err := r.Aspect.Size()
	return err
}

type GeneratedImage struct {
	Bytes              []byte
	MIMEType           string
	Model              string
	ProviderCostMicros int64
}
