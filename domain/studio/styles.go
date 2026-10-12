package studio

import (
	"math"
	"strings"
	"unicode/utf8"
)

const (
	StyleColor  = "color"
	StyleText   = "text"
	StyleEffect = "effect"

	MinStyleFontPx       = 1.0
	MaxStyleFontPx       = 2000.0
	MaxStyleDescription  = 500
	styleFontPxTolerance = 0.01
	styleNumberTolerance = 1e-6
)

var styleRoles = map[string]bool{"": true, "heading": true, "subheading": true, "body": true}

type Style struct {
	ID            string    `json:"id"`
	Kind          string    `json:"kind"`
	Name          string    `json:"name"`
	Description   string    `json:"description,omitempty"`
	Color         string    `json:"color,omitempty"`
	Gradient      *Gradient `json:"gradient,omitempty"`
	Role          string    `json:"role,omitempty"`
	FontID        string    `json:"fontId,omitempty"`
	FontWeight    int       `json:"fontWeight,omitempty"`
	Italic        *bool     `json:"italic,omitempty"`
	FontSizePx    float64   `json:"fontSizePx,omitempty"`
	LineHeight    float64   `json:"lineHeight,omitempty"`
	LetterSpacing float64   `json:"letterSpacing,omitempty"`
	Shadow        *Shadow   `json:"shadow,omitempty"`
}

func (s Style) setsColor() bool { return s.Color != "" || s.Gradient != nil }

func (s Style) setsText() bool {
	return s.Role != "" || s.FontID != "" || s.FontWeight != 0 || s.Italic != nil || s.FontSizePx != 0 || s.LineHeight != 0 || s.LetterSpacing != 0
}

func (s Style) textIssue() string {
	switch {
	case s.FontID == "" && s.FontWeight == 0 && s.Italic == nil && s.FontSizePx == 0 && s.LineHeight == 0 && s.LetterSpacing == 0:
		return CodeInvalid
	case (s.FontID != "" && !fonts[s.FontID]) || !styleRoles[s.Role]:
		return CodeUnknown
	case s.FontWeight != 0 && (s.FontWeight < 100 || s.FontWeight > 900 || s.FontWeight%100 != 0):
		return CodeOutOfRange
	case s.FontSizePx != 0 && !within(s.FontSizePx, MinStyleFontPx, MaxStyleFontPx):
		return CodeOutOfRange
	case s.LineHeight != 0 && !within(s.LineHeight, 0.5, 4):
		return CodeOutOfRange
	case s.LetterSpacing != 0 && !within(s.LetterSpacing, -0.5, 2):
		return CodeOutOfRange
	}
	return ""
}

func (s Style) issue() string {
	switch {
	case !validToken(s.ID) || strings.TrimSpace(s.Name) == "" || utf8.RuneCountInString(s.Name) > maxTokenRunes*2 || utf8.RuneCountInString(s.Description) > MaxStyleDescription:
		return CodeInvalid
	case s.Kind != StyleColor && s.Kind != StyleText && s.Kind != StyleEffect:
		return CodeUnknown
	case s.Kind != StyleColor && s.setsColor(), s.Kind != StyleText && s.setsText(), s.Kind != StyleEffect && s.Shadow != nil:
		return CodeInvalid
	}
	switch s.Kind {
	case StyleText:
		return s.textIssue()
	case StyleEffect:
		if s.Shadow == nil || !s.Shadow.valid() {
			return CodeInvalid
		}
		return ""
	}
	if (s.Color != "") == (s.Gradient != nil) {
		return CodeInvalid
	}
	if s.Color != "" && !validColor(s.Color, true) || !s.Gradient.valid() {
		return CodeInvalid
	}
	return ""
}

func (sh *Shadow) valid() bool {
	return validColor(sh.Color, true) && within(sh.Blur, 0, 200) && within(sh.X, -500, 500) && within(sh.Y, -500, 500)
}

func stylesIssue(styles []Style) string {
	seen := make(map[string]bool, len(styles))
	for _, s := range styles {
		if seen[s.ID] {
			return CodeDuplicate
		}
		seen[s.ID] = true
		if code := s.issue(); code != "" {
			return code
		}
	}
	return ""
}

type styleSlot int

const (
	slotFill styleSlot = iota
	slotStroke
	slotText
	slotEffect
)

func (l Layer) styleLinks() map[styleSlot]string {
	links := map[styleSlot]string{}
	for slot, id := range map[styleSlot]string{slotFill: l.FillStyleID, slotStroke: l.StrokeStyleID, slotText: l.TextStyleID, slotEffect: l.EffectStyleID} {
		if id != "" {
			links[slot] = id
		}
	}
	return links
}

func (l Layer) linksStyles() bool { return len(l.styleLinks()) > 0 }

func (l Layer) styleIDsIssue() string {
	for _, id := range l.styleLinks() {
		if !validToken(id) {
			return CodeInvalid
		}
	}
	return ""
}

func (l Layer) suits(slot styleSlot, s Style) bool {
	switch slot {
	case slotFill:
		painted := l.Type == LayerText || l.Type == LayerIcon || l.Type == LayerShape
		return s.Kind == StyleColor && painted && !l.strokedShape() && len(l.Fills) == 0 && !(s.Gradient != nil && l.Type == LayerIcon)
	case slotStroke:
		stroked := l.strokedShape() || l.StrokeWidth > 0
		return s.Kind == StyleColor && s.Gradient == nil && (l.Type == LayerText || l.Type == LayerShape || l.Type == LayerImage) && len(l.Strokes) == 0 && stroked
	case slotText:
		return s.Kind == StyleText && l.Type == LayerText
	}
	return s.Kind == StyleEffect && (l.Type == LayerText || l.Type == LayerShape || l.Type == LayerImage || l.Type == LayerIcon) && len(l.Effects) == 0
}

func sameColor(a, b string) bool { return strings.EqualFold(a, b) }

func sameNumber(a, b float64) bool { return math.Abs(a-b) <= styleNumberTolerance }

func sameGradient(a, b *Gradient) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return sameColor(a.From, b.From) && sameColor(a.To, b.To) && sameColor(a.Via, b.Via) && a.Kind == b.Kind &&
		sameNumber(a.Angle, b.Angle) && sameNumber(a.CX, b.CX) && sameNumber(a.CY, b.CY) && sameNumber(a.Radius, b.Radius)
}

func sameShadow(a, b *Shadow) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return sameColor(a.Color, b.Color) && sameNumber(a.Blur, b.Blur) && sameNumber(a.X, b.X) && sameNumber(a.Y, b.Y)
}

func (s Style) paints(color string, gradient *Gradient) bool {
	if s.Gradient != nil {
		return sameGradient(gradient, s.Gradient) && sameColor(color, s.Gradient.From)
	}
	return gradient == nil && sameColor(color, s.Color)
}

func (s Style) setsTextOf(l Layer, surfaceHeight int) bool {
	switch {
	case s.FontID != "" && l.FontID != s.FontID, s.FontWeight != 0 && l.FontWeight != s.FontWeight:
		return false
	case s.Italic != nil && l.Italic != *s.Italic:
		return false
	case s.FontSizePx != 0 && math.Abs(l.FontSize*float64(surfaceHeight)-s.FontSizePx) > styleFontPxTolerance:
		return false
	case s.LineHeight != 0 && !sameNumber(l.LineHeight, s.LineHeight), s.LetterSpacing != 0 && !sameNumber(l.Letter, s.LetterSpacing):
		return false
	}
	return true
}

func (l Layer) linkHolds(slot styleSlot, s Style, surfaceHeight int) bool {
	if !l.suits(slot, s) {
		return false
	}
	switch slot {
	case slotFill:
		return s.paints(l.Fill, l.Gradient)
	case slotStroke:
		return sameColor(l.Stroke, s.Color)
	case slotText:
		return s.setsTextOf(l, surfaceHeight)
	}
	return sameShadow(l.Shadow, s.Shadow)
}

func styleLinksIssue(artboards []Artboard, styles []Style) string {
	byID := make(map[string]Style, len(styles))
	for _, s := range styles {
		byID[s.ID] = s
	}
	for _, a := range artboards {
		if id := a.Canvas.BackgroundStyleID; id != "" {
			s, found := byID[id]
			if !found || s.Kind != StyleColor || !s.paints(a.Canvas.Background, a.Canvas.Gradient) {
				return CodeInvalid
			}
		}
		for _, l := range a.Layers {
			for slot, id := range l.styleLinks() {
				s, found := byID[id]
				if !found || !l.linkHolds(slot, s, a.Canvas.Height) {
					return CodeInvalid
				}
			}
		}
	}
	return ""
}
