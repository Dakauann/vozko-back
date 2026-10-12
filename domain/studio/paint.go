package studio

import "strings"

const (
	PatternPreset = "preset"
	PatternAsset  = "asset"

	PaintSolid    = "solid"
	PaintGradient = "gradient"
	PaintPattern  = "pattern"

	StrokeCenter  = "center"
	StrokeInside  = "inside"
	StrokeOutside = "outside"

	EffectDropShadow  = "drop-shadow"
	EffectInnerShadow = "inner-shadow"
	EffectOuterGlow   = "outer-glow"
	EffectInnerGlow   = "inner-glow"

	MinPatternSize     = 2.0
	MaxPatternSize     = 4096.0
	MaxPatternSpacing  = 2048.0
	MaxPatternOverlap  = 0.9
	MaxPatternAngle    = 180.0
	MaxEffectOffset    = 500.0
	MaxEffectBlur      = 200.0
	MaxEffectSpread    = 100.0
	MaxPaintStrokeSize = 200.0
)

var (
	patternPresets = map[string]bool{
		"dots": true, "grid-lines": true, "stripes": true, "diagonal": true, "checker": true, "cross": true,
		"zigzag": true, "waves": true, "triangles": true, "hexagons": true, "confetti": true, "plus": true,
	}
	patternTiles      = map[string]bool{"": true, "grid": true, "brick-row": true, "brick-column": true, "hex-row": true, "hex-column": true}
	brickTiles        = map[string]bool{"brick-row": true, "brick-column": true}
	mapInterpolations = map[string]bool{"": true, "perceptual": true, "srgb": true}
	strokeAligns      = map[string]bool{StrokeCenter: true, StrokeInside: true, StrokeOutside: true}
	effectTypes       = map[string]bool{EffectDropShadow: true, EffectInnerShadow: true, EffectOuterGlow: true, EffectInnerGlow: true}
	outerEffects      = map[string]bool{EffectDropShadow: true, EffectOuterGlow: true}
	glowSources       = map[string]bool{"": true, "edge": true, "center": true}
)

type PatternPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type Pattern struct {
	Source     string        `json:"source"`
	PresetID   string        `json:"presetId,omitempty"`
	AssetID    string        `json:"assetId,omitempty"`
	Color      string        `json:"color,omitempty"`
	Background string        `json:"background,omitempty"`
	Tile       string        `json:"tile,omitempty"`
	Size       float64       `json:"size"`
	Spacing    *PatternPoint `json:"spacing,omitempty"`
	Offset     float64       `json:"offset,omitempty"`
	Angle      float64       `json:"angle,omitempty"`
	Origin     *PatternPoint `json:"origin,omitempty"`
	Opacity    float64       `json:"opacity"`
	BlendMode  string        `json:"blendMode,omitempty"`
}

type GradientStop struct {
	At    float64 `json:"at"`
	Color string  `json:"color"`
}

type GradientMap struct {
	Stops         []GradientStop `json:"stops"`
	Intensity     float64        `json:"intensity"`
	Reverse       bool           `json:"reverse,omitempty"`
	Interpolation string         `json:"interpolation,omitempty"`
}

type Paint struct {
	Type      string    `json:"type"`
	Color     string    `json:"color,omitempty"`
	Gradient  *Gradient `json:"gradient,omitempty"`
	Pattern   *Pattern  `json:"pattern,omitempty"`
	Opacity   float64   `json:"opacity"`
	BlendMode string    `json:"blendMode,omitempty"`
	Hidden    bool      `json:"hidden,omitempty"`
}

type StrokePaint struct {
	Paint
	Width      float64   `json:"width"`
	Align      string    `json:"align"`
	DashArray  []float64 `json:"dashArray,omitempty"`
	DashOffset float64   `json:"dashOffset,omitempty"`
	LineCap    string    `json:"lineCap,omitempty"`
	LineJoin   string    `json:"lineJoin,omitempty"`
	MiterLimit float64   `json:"miterLimit,omitempty"`
}

type Effect struct {
	ID         string           `json:"id"`
	Type       string           `json:"type"`
	Color      string           `json:"color"`
	Opacity    float64          `json:"opacity"`
	X          float64          `json:"x"`
	Y          float64          `json:"y"`
	Blur       float64          `json:"blur"`
	Spread     float64          `json:"spread"`
	BlendMode  string           `json:"blendMode,omitempty"`
	Hidden     bool             `json:"hidden,omitempty"`
	ShowBehind bool             `json:"showBehind,omitempty"`
	Source     string           `json:"source,omitempty"`
	Keyframes  *EffectKeyframes `json:"keyframes,omitempty"`
}

func spacingFits(size, gap float64) bool {
	return within(gap, -MaxPatternOverlap*size, MaxPatternSpacing) && size+gap >= 1
}

func (p Pattern) sourceIssue() string {
	switch p.Source {
	case PatternPreset:
		if !patternPresets[p.PresetID] {
			return CodeUnknown
		}
		if p.AssetID != "" || !validColor(p.Color, true) || !validColor(p.Background, false) {
			return CodeInvalid
		}
	case PatternAsset:
		if strings.TrimSpace(p.AssetID) == "" || p.PresetID != "" || p.Color != "" || p.Background != "" {
			return CodeInvalid
		}
	default:
		return CodeUnknown
	}
	return ""
}

func (p Pattern) issue() string {
	if code := p.sourceIssue(); code != "" {
		return code
	}
	if !patternTiles[p.Tile] || !blendModes[p.BlendMode] {
		return CodeUnknown
	}
	if !within(p.Size, MinPatternSize, MaxPatternSize) {
		return CodeOutOfRange
	}
	spacing := PatternPoint{}
	if p.Spacing != nil {
		spacing = *p.Spacing
	}
	if !spacingFits(p.Size, spacing.X) || !spacingFits(p.Size, spacing.Y) || !within(p.Offset, 0, 1) {
		return CodeOutOfRange
	}
	if p.Offset != 0 && !brickTiles[p.Tile] {
		return CodeInvalid
	}
	origin := PatternPoint{}
	if p.Origin != nil {
		origin = *p.Origin
	}
	if !within(origin.X, 0, 1) || !within(origin.Y, 0, 1) || !within(p.Angle, -MaxPatternAngle, MaxPatternAngle) || !within(p.Opacity, 0, 1) {
		return CodeOutOfRange
	}
	return ""
}

func (m GradientMap) issue() string {
	if len(m.Stops) < 2 {
		return CodeInvalid
	}
	last := 0.0
	for _, stop := range m.Stops {
		if !within(stop.At, 0, 1) {
			return CodeOutOfRange
		}
		if stop.At < last || !validColor(stop.Color, true) {
			return CodeInvalid
		}
		last = stop.At
	}
	if !mapInterpolations[m.Interpolation] {
		return CodeUnknown
	}
	if !within(m.Intensity, 0, 1) {
		return CodeOutOfRange
	}
	return ""
}

func (p Paint) issue() string {
	if !blendModes[p.BlendMode] {
		return CodeUnknown
	}
	if !within(p.Opacity, 0, 1) {
		return CodeOutOfRange
	}
	switch p.Type {
	case PaintSolid:
		if !validColor(p.Color, true) || p.Gradient != nil || p.Pattern != nil {
			return CodeInvalid
		}
	case PaintGradient:
		if p.Color != "" || p.Gradient == nil || !p.Gradient.valid() || p.Pattern != nil {
			return CodeInvalid
		}
	case PaintPattern:
		if p.Color != "" || p.Gradient != nil || p.Pattern == nil {
			return CodeInvalid
		}
		return p.Pattern.issue()
	default:
		return CodeUnknown
	}
	return ""
}

func (s StrokePaint) dashIssue() string {
	if !lineCaps[s.LineCap] || !lineJoins[s.LineJoin] {
		return CodeUnknown
	}
	if s.MiterLimit != 0 && !within(s.MiterLimit, MinMiterLimit, MaxMiterLimit) {
		return CodeOutOfRange
	}
	if len(s.DashArray) > MaxDashValues {
		return CodeInvalid
	}
	visible := false
	for _, v := range s.DashArray {
		if !within(v, 0, MaxDashValue) {
			return CodeInvalid
		}
		if v > 0 {
			visible = true
		}
	}
	if len(s.DashArray) > 0 && !visible {
		return CodeInvalid
	}
	if !within(s.DashOffset, -MaxDashOffset, MaxDashOffset) {
		return CodeOutOfRange
	}
	return ""
}

func (s StrokePaint) styled() bool {
	return len(s.DashArray) > 0 || s.DashOffset != 0 || s.LineCap != "" || s.LineJoin != "" || s.MiterLimit != 0
}

func (s StrokePaint) issue(traced bool) string {
	if code := s.Paint.issue(); code != "" {
		return code
	}
	if !strokeAligns[s.Align] {
		return CodeUnknown
	}
	if !within(s.Width, 0, MaxPaintStrokeSize) {
		return CodeOutOfRange
	}
	if code := s.dashIssue(); code != "" {
		return code
	}
	if traced && (s.Type != PaintSolid || s.Align != StrokeOutside || s.BlendMode != "" || s.styled()) {
		return CodeInvalid
	}
	return ""
}

func (e Effect) issue(seen map[string]bool) string {
	if !validToken(e.ID) || seen[e.ID] || e.Keyframes != nil {
		return CodeInvalid
	}
	seen[e.ID] = true
	if !effectTypes[e.Type] || !blendModes[e.BlendMode] || !glowSources[e.Source] {
		return CodeUnknown
	}
	switch {
	case !validColor(e.Color, true):
		return CodeInvalid
	case e.BlendMode != "" && outerEffects[e.Type]:
		return CodeInvalid
	case e.ShowBehind && e.Type != EffectDropShadow:
		return CodeInvalid
	case e.Source != "" && e.Type != EffectInnerGlow:
		return CodeInvalid
	case !within(e.Opacity, 0, 1) || !within(e.Blur, 0, MaxEffectBlur) || !within(e.Spread, -MaxEffectSpread, MaxEffectSpread):
		return CodeOutOfRange
	case !within(e.X, -MaxEffectOffset, MaxEffectOffset) || !within(e.Y, -MaxEffectOffset, MaxEffectOffset):
		return CodeOutOfRange
	}
	return ""
}

func (l Layer) takesFills() bool {
	return l.Type == LayerText || l.Type == LayerIcon || (l.Type == LayerShape && !l.strokedShape())
}

func (l Layer) tracesOutline() bool {
	return l.Type == LayerImage || l.Type == LayerIcon
}

func (l Layer) legacyStroked() bool {
	return l.Stroke != "" || l.StrokeWidth != 0 || l.Dash || len(l.DashArray) > 0 || l.DashOffset != 0 || l.LineCap != "" || l.LineJoin != "" || l.MiterLimit != 0
}

func (l Layer) fillsIssue() string {
	if len(l.Fills) == 0 {
		return ""
	}
	if !l.takesFills() || l.Fill != "" || l.Gradient != nil || l.Pattern != nil {
		return CodeInvalid
	}
	for _, p := range l.Fills {
		if code := p.issue(); code != "" {
			return code
		}
	}
	return ""
}

func (l Layer) strokesIssue() string {
	if len(l.Strokes) == 0 {
		return ""
	}
	if l.strokedShape() || l.ArrowStart || l.ArrowEnd || l.legacyStroked() {
		return CodeInvalid
	}
	for _, s := range l.Strokes {
		if code := s.issue(l.tracesOutline()); code != "" {
			return code
		}
	}
	return ""
}

func (l Layer) effectsIssue() string {
	if len(l.Effects) == 0 {
		return ""
	}
	if l.Shadow != nil {
		return CodeInvalid
	}
	seen := make(map[string]bool, len(l.Effects))
	for _, e := range l.Effects {
		if code := e.issue(seen); code != "" {
			return code
		}
	}
	return ""
}

func (l Layer) paintIssue() string {
	if l.Pattern != nil {
		if !l.takesFills() || l.Gradient != nil {
			return CodeInvalid
		}
		if code := l.Pattern.issue(); code != "" {
			return code
		}
	}
	if l.GradientMap != nil {
		if l.Type != LayerImage {
			return CodeInvalid
		}
		if code := l.GradientMap.issue(); code != "" {
			return code
		}
	}
	if code := l.fillsIssue(); code != "" {
		return code
	}
	if code := l.strokesIssue(); code != "" {
		return code
	}
	return l.effectsIssue()
}
