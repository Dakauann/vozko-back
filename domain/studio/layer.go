package studio

import (
	"math"
	"regexp"
	"strings"
	"unicode/utf8"
)

type LayerType string

const (
	LayerImage LayerType = "image"
	LayerText  LayerType = "text"
	LayerShape LayerType = "shape"
	LayerIcon  LayerType = "icon"
)

type ShapeKind string

const (
	ShapeRect     ShapeKind = "rect"
	ShapeEllipse  ShapeKind = "ellipse"
	ShapeLine     ShapeKind = "line"
	ShapeArrow    ShapeKind = "arrow"
	ShapeTriangle ShapeKind = "triangle"
	ShapeStar     ShapeKind = "star"
)

const (
	MaxTextRunes  = 2000
	maxTokenRunes = 64
)

var (
	colorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}([0-9a-fA-F]{2})?$`)
	tokenPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	shapes       = map[ShapeKind]bool{ShapeRect: true, ShapeEllipse: true, ShapeLine: true, ShapeArrow: true, ShapeTriangle: true, ShapeStar: true}
	aligns       = map[string]bool{"": true, "left": true, "center": true, "right": true}
	frames       = map[ShapeKind]bool{"": true, ShapeEllipse: true, ShapeTriangle: true, ShapeStar: true}
	blendModes   = map[string]bool{
		"": true, "normal": true, "multiply": true, "screen": true, "overlay": true, "darken": true, "lighten": true,
		"color-dodge": true, "color-burn": true, "hard-light": true, "soft-light": true, "difference": true, "exclusion": true,
		"hue": true, "saturation": true, "color": true, "luminosity": true,
	}
)

type Gradient struct {
	From  string  `json:"from"`
	To    string  `json:"to"`
	Angle float64 `json:"angle"`
}

func (g *Gradient) valid() bool {
	return g == nil || (validColor(g.From, true) && validColor(g.To, true) && within(g.Angle, -360, 360))
}

type Highlight struct {
	Color  string  `json:"color"`
	Radius float64 `json:"radius"`
}

type Transform struct {
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	W        float64 `json:"w"`
	H        float64 `json:"h"`
	Rotation float64 `json:"rotation"`
	Opacity  float64 `json:"opacity"`
}

type Crop struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

type Filters struct {
	Brightness float64 `json:"brightness"`
	Contrast   float64 `json:"contrast"`
	Saturation float64 `json:"saturation"`
	Blur       float64 `json:"blur"`
}

type Shadow struct {
	Color string  `json:"color"`
	Blur  float64 `json:"blur"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
}

type Layer struct {
	ID          string     `json:"id"`
	Type        LayerType  `json:"type"`
	Name        string     `json:"name,omitempty"`
	Transform   Transform  `json:"transform"`
	Hidden      bool       `json:"hidden,omitempty"`
	Locked      bool       `json:"locked,omitempty"`
	GroupID     string     `json:"groupId,omitempty"`
	AssetID     string     `json:"assetId,omitempty"`
	Crop        *Crop      `json:"crop,omitempty"`
	FlipX       bool       `json:"flipX,omitempty"`
	FlipY       bool       `json:"flipY,omitempty"`
	Radius      float64    `json:"radius,omitempty"`
	Filters     *Filters   `json:"filters,omitempty"`
	Text        string     `json:"text,omitempty"`
	FontID      string     `json:"fontId,omitempty"`
	FontSize    float64    `json:"fontSize,omitempty"`
	FontWeight  int        `json:"fontWeight,omitempty"`
	Italic      bool       `json:"italic,omitempty"`
	Align       string     `json:"align,omitempty"`
	LineHeight  float64    `json:"lineHeight,omitempty"`
	Letter      float64    `json:"letterSpacing,omitempty"`
	Fill        string     `json:"fill,omitempty"`
	Stroke      string     `json:"stroke,omitempty"`
	StrokeWidth float64    `json:"strokeWidth,omitempty"`
	Dash        bool       `json:"dash,omitempty"`
	Shadow      *Shadow    `json:"shadow,omitempty"`
	Shape       ShapeKind  `json:"shape,omitempty"`
	ArrowStart  bool       `json:"arrowStart,omitempty"`
	ArrowEnd    bool       `json:"arrowEnd,omitempty"`
	IconID      string     `json:"iconId,omitempty"`
	BlendMode   string     `json:"blendMode,omitempty"`
	Clip        bool       `json:"clip,omitempty"`
	Gradient    *Gradient  `json:"gradient,omitempty"`
	Highlight   *Highlight `json:"highlight,omitempty"`
	Curve       float64    `json:"curve,omitempty"`
	Frame       ShapeKind  `json:"frame,omitempty"`
}

func finite(values ...float64) bool {
	for _, v := range values {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

func within(v, lo, hi float64) bool { return finite(v) && v >= lo && v <= hi }

func validColor(c string, required bool) bool {
	if c == "" {
		return !required
	}
	return colorPattern.MatchString(c)
}

func validToken(t string) bool {
	return utf8.RuneCountInString(t) <= maxTokenRunes && tokenPattern.MatchString(t)
}

func (t Transform) issue() string {
	switch {
	case !within(t.X, -1, 2) || !within(t.Y, -1, 2):
		return CodeOutOfRange
	case !within(t.W, 0.001, 4) || !within(t.H, 0.001, 4):
		return CodeOutOfRange
	case !within(t.Rotation, -360, 360) || !within(t.Opacity, 0, 1):
		return CodeOutOfRange
	}
	return ""
}

func (l Layer) issue() string {
	if !validToken(l.ID) || (l.GroupID != "" && !validToken(l.GroupID)) || utf8.RuneCountInString(l.Name) > maxTokenRunes*2 {
		return CodeInvalid
	}
	if code := l.Transform.issue(); code != "" {
		return code
	}
	if !validColor(l.Fill, false) || !validColor(l.Stroke, false) || !within(l.StrokeWidth, 0, 200) || !within(l.Radius, 0, 1) {
		return CodeInvalid
	}
	if l.Shadow != nil && (!validColor(l.Shadow.Color, true) || !within(l.Shadow.Blur, 0, 200) || !within(l.Shadow.X, -500, 500) || !within(l.Shadow.Y, -500, 500)) {
		return CodeInvalid
	}
	if !blendModes[l.BlendMode] || !frames[l.Frame] {
		return CodeUnknown
	}
	if !l.Gradient.valid() || (l.Highlight != nil && (!validColor(l.Highlight.Color, true) || !within(l.Highlight.Radius, 0, 1))) {
		return CodeInvalid
	}
	if !within(l.Curve, -1, 1) {
		return CodeOutOfRange
	}
	switch l.Type {
	case LayerImage:
		return l.imageIssue()
	case LayerText:
		return l.textIssue()
	case LayerShape:
		if !shapes[l.Shape] {
			return CodeUnknown
		}
		return ""
	case LayerIcon:
		if !validToken(l.IconID) {
			return CodeInvalid
		}
		return ""
	}
	return CodeUnknown
}

func (l Layer) imageIssue() string {
	if strings.TrimSpace(l.AssetID) == "" {
		return CodeRequired
	}
	if c := l.Crop; c != nil && (!within(c.X, 0, 1) || !within(c.Y, 0, 1) || !within(c.W, 0.01, 1) || !within(c.H, 0.01, 1) || c.X+c.W > 1.0001 || c.Y+c.H > 1.0001) {
		return CodeOutOfRange
	}
	if f := l.Filters; f != nil && (!within(f.Brightness, -1, 1) || !within(f.Contrast, -100, 100) || !within(f.Saturation, -2, 10) || !within(f.Blur, 0, 40)) {
		return CodeOutOfRange
	}
	return ""
}

func (l Layer) textIssue() string {
	switch {
	case strings.TrimSpace(l.Text) == "" || utf8.RuneCountInString(l.Text) > MaxTextRunes:
		return CodeInvalid
	case !fonts[l.FontID]:
		return CodeUnknown
	case !within(l.FontSize, 0.005, 0.5) || !within(l.LineHeight, 0, 4) || !within(l.Letter, -0.5, 2):
		return CodeOutOfRange
	case l.FontWeight != 0 && (l.FontWeight < 100 || l.FontWeight > 900 || l.FontWeight%100 != 0):
		return CodeOutOfRange
	case !aligns[l.Align]:
		return CodeUnknown
	}
	return ""
}
