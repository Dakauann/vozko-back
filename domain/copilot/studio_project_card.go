package copilot

import (
	"errors"
	"strings"

	"vozko/domain/mediagen"
	"vozko/domain/studio"
)

const (
	ActionStudioProject ActionKind = "studio_project"

	StudioMediaImage = "image"
	StudioMediaVideo = "video"
	StudioMediaAudio = "audio"

	MinStudioStillMs = 100
)

var ErrInvalidStudioProject = errors.New("copilot: the studio project order cannot be built")

type CanvasSize struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type StudioProjectMedia struct {
	MediaID string `json:"mediaId"`
	URL     string `json:"url"`
	Type    string `json:"type"`
}

type StudioProjectOrder struct {
	Kind       string               `json:"kind"`
	Name       string               `json:"name,omitempty"`
	Media      []StudioProjectMedia `json:"media,omitempty"`
	Sizes      []CanvasSize         `json:"sizes,omitempty"`
	Aspect     string               `json:"aspect,omitempty"`
	DurationMs int                  `json:"durationMs,omitempty"`
	ProjectID  string               `json:"projectId,omitempty"`
}

var studioVideoAspects = []mediagen.Aspect{mediagen.AspectSquare, mediagen.AspectPortrait, mediagen.AspectStory, mediagen.AspectLandscape}

func (s CanvasSize) Valid() bool {
	return s.Width >= studio.MinCanvasSide && s.Width <= studio.MaxCanvasSide && s.Height >= studio.MinCanvasSide && s.Height <= studio.MaxCanvasSide
}

func ValidStudioAspect(aspect string) bool {
	for _, known := range studioVideoAspects {
		if string(known) == aspect {
			return true
		}
	}
	return false
}

func (m StudioProjectMedia) fits(kind studio.Kind) bool {
	if strings.TrimSpace(m.MediaID) == "" || strings.TrimSpace(m.URL) == "" {
		return false
	}
	switch m.Type {
	case StudioMediaImage:
		return true
	case StudioMediaVideo, StudioMediaAudio:
		return kind == studio.KindVideo
	}
	return false
}

func (o StudioProjectOrder) valid() bool {
	if o.ProjectID != "" && !uuidPattern.MatchString(o.ProjectID) {
		return false
	}
	kind := studio.Kind(o.Kind)
	switch kind {
	case studio.KindImage:
		if o.Aspect != "" || o.DurationMs != 0 {
			return false
		}
		for _, size := range o.Sizes {
			if !size.Valid() {
				return false
			}
		}
	case studio.KindVideo:
		if len(o.Sizes) > 0 || (o.Aspect != "" && !ValidStudioAspect(o.Aspect)) {
			return false
		}
		if o.DurationMs != 0 && (o.DurationMs < MinStudioStillMs || o.DurationMs > studio.MaxVideoMS) {
			return false
		}
	default:
		return false
	}
	for _, m := range o.Media {
		if !m.fits(kind) {
			return false
		}
	}
	return true
}

func NewStudioProjectCard(order StudioProjectOrder) (*ActionCard, error) {
	if !order.valid() {
		return nil, ErrInvalidStudioProject
	}
	return &ActionCard{Kind: ActionStudioProject, StudioProject: &order}, nil
}
