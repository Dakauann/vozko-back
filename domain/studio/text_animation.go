package studio

import (
	"slices"

	"vozko/domain/mediagen"
)

type TextUnit string

const (
	TextUnitChar TextUnit = "char"
	TextUnitWord TextUnit = "word"
	TextUnitLine TextUnit = "line"
)

type TextStyle string

const (
	TextStyleFade       TextStyle = "fade"
	TextStyleRise       TextStyle = "rise"
	TextStyleSlide      TextStyle = "slide"
	TextStylePop        TextStyle = "pop"
	TextStyleBlur       TextStyle = "blur"
	TextStyleTypewriter TextStyle = "typewriter"
)

const (
	MaxTextAnimationMS = 3_000
	MaxTextStaggerMS   = 2_000
)

type TextAnimator struct {
	Unit       TextUnit        `json:"unit"`
	Style      TextStyle       `json:"style"`
	DurationMS int64           `json:"durationMs"`
	StaggerMS  int64           `json:"staggerMs"`
	Easing     mediagen.Easing `json:"easing"`
}

func TextUnits() []TextUnit { return []TextUnit{TextUnitChar, TextUnitWord, TextUnitLine} }

func TextStyles() []TextStyle {
	return []TextStyle{TextStyleFade, TextStyleRise, TextStyleSlide, TextStylePop, TextStyleBlur, TextStyleTypewriter}
}

func (a *TextAnimator) issue() string {
	switch {
	case a == nil:
		return ""
	case !slices.Contains(TextUnits(), a.Unit) || !slices.Contains(TextStyles(), a.Style) || !a.Easing.Known():
		return CodeUnknown
	case a.DurationMS < 0 || a.DurationMS > MaxTextAnimationMS || a.StaggerMS < 0 || a.StaggerMS > MaxTextStaggerMS:
		return CodeOutOfRange
	}
	return ""
}

func (c Clip) animatesText() bool {
	return c.Type == ClipOverlay && c.Layer != nil && c.Layer.Type == LayerText
}

func (c Clip) textAnimationIssue() string {
	if c.TextIn == nil && c.TextOut == nil {
		return ""
	}
	if !c.animatesText() {
		return CodeInvalid
	}
	if code := c.TextIn.issue(); code != "" {
		return code
	}
	return c.TextOut.issue()
}
