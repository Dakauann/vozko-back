package studio

import "vozko/domain/mediagen"

const (
	WaveBend     = "bend"
	WaveWidth    = "width"
	MaxLineSeed  = 9999
	lineTrim     = "trim"
	lineWave     = "wave"
	lineWiggle   = "wiggle"
	lineTaper    = "taper"
	maxLineTurns = 3600.0
)

type LineTrim struct {
	Start      float64 `json:"start"`
	End        float64 `json:"end"`
	Offset     float64 `json:"offset"`
	Sequential bool    `json:"sequential,omitempty"`
}

type LineWave struct {
	Mode       string  `json:"mode"`
	Amount     float64 `json:"amount"`
	Wavelength float64 `json:"wavelength"`
	Speed      float64 `json:"speed"`
	Phase      float64 `json:"phase"`
	Travel     float64 `json:"travel,omitempty"`
}

type LineWiggle struct {
	Size   float64 `json:"size"`
	Detail float64 `json:"detail"`
	Rate   float64 `json:"rate"`
	Seed   int     `json:"seed"`
	Travel float64 `json:"travel,omitempty"`
}

type LineTaper struct {
	StartLength float64 `json:"startLength"`
	EndLength   float64 `json:"endLength"`
	StartWidth  float64 `json:"startWidth"`
	EndWidth    float64 `json:"endWidth"`
	StartEase   float64 `json:"startEase"`
	EndEase     float64 `json:"endEase"`
}

type LineAnimation struct {
	Trim   *LineTrim   `json:"trim,omitempty"`
	Wave   *LineWave   `json:"wave,omitempty"`
	Wiggle *LineWiggle `json:"wiggle,omitempty"`
	Taper  *LineTaper  `json:"taper,omitempty"`
}

type LineKeyframes struct {
	TrimStart        []mediagen.Keyframe `json:"trimStart,omitempty"`
	TrimEnd          []mediagen.Keyframe `json:"trimEnd,omitempty"`
	TrimOffset       []mediagen.Keyframe `json:"trimOffset,omitempty"`
	WaveAmount       []mediagen.Keyframe `json:"waveAmount,omitempty"`
	WaveLength       []mediagen.Keyframe `json:"waveLength,omitempty"`
	WaveSpeed        []mediagen.Keyframe `json:"waveSpeed,omitempty"`
	WavePhase        []mediagen.Keyframe `json:"wavePhase,omitempty"`
	WiggleSize       []mediagen.Keyframe `json:"wiggleSize,omitempty"`
	WiggleDetail     []mediagen.Keyframe `json:"wiggleDetail,omitempty"`
	WiggleRate       []mediagen.Keyframe `json:"wiggleRate,omitempty"`
	TaperStartLength []mediagen.Keyframe `json:"taperStartLength,omitempty"`
	TaperEndLength   []mediagen.Keyframe `json:"taperEndLength,omitempty"`
	TaperStartWidth  []mediagen.Keyframe `json:"taperStartWidth,omitempty"`
	TaperEndWidth    []mediagen.Keyframe `json:"taperEndWidth,omitempty"`
	TaperStartEase   []mediagen.Keyframe `json:"taperStartEase,omitempty"`
	TaperEndEase     []mediagen.Keyframe `json:"taperEndEase,omitempty"`
}

type lineRange struct{ low, high float64 }

var (
	lineWaveModes  = map[string]bool{WaveBend: true, WaveWidth: true}
	lineFraction   = lineRange{0, 1}
	lineTurns      = lineRange{-maxLineTurns, maxLineTurns}
	linePixels     = lineRange{0, 500}
	lineEase       = lineRange{-1, 1}
	lineWavelength = lineRange{8, 4000}
	lineWaveSpeed  = lineRange{-20, 20}
	lineDetail     = lineRange{0.05, 10}
	lineRate       = lineRange{0, 30}
	lineSeedRange  = lineRange{0, MaxLineSeed}
	wiggleTravel   = lineRange{0, lineRate.high * float64(MaxVideoMS) / 1000}
)

type lineLane struct {
	group  string
	r      lineRange
	frames []mediagen.Keyframe
	value  func(*LineAnimation) float64
}

func (k *LineKeyframes) lanes() []lineLane {
	return []lineLane{
		{lineTrim, lineFraction, k.TrimStart, func(a *LineAnimation) float64 { return a.Trim.Start }},
		{lineTrim, lineFraction, k.TrimEnd, func(a *LineAnimation) float64 { return a.Trim.End }},
		{lineTrim, lineTurns, k.TrimOffset, func(a *LineAnimation) float64 { return a.Trim.Offset }},
		{lineWave, linePixels, k.WaveAmount, func(a *LineAnimation) float64 { return a.Wave.Amount }},
		{lineWave, lineWavelength, k.WaveLength, func(a *LineAnimation) float64 { return a.Wave.Wavelength }},
		{lineWave, lineWaveSpeed, k.WaveSpeed, func(a *LineAnimation) float64 { return a.Wave.Speed }},
		{lineWave, lineTurns, k.WavePhase, func(a *LineAnimation) float64 { return a.Wave.Phase }},
		{lineWiggle, linePixels, k.WiggleSize, func(a *LineAnimation) float64 { return a.Wiggle.Size }},
		{lineWiggle, lineDetail, k.WiggleDetail, func(a *LineAnimation) float64 { return a.Wiggle.Detail }},
		{lineWiggle, lineRate, k.WiggleRate, func(a *LineAnimation) float64 { return a.Wiggle.Rate }},
		{lineTaper, lineFraction, k.TaperStartLength, func(a *LineAnimation) float64 { return a.Taper.StartLength }},
		{lineTaper, lineFraction, k.TaperEndLength, func(a *LineAnimation) float64 { return a.Taper.EndLength }},
		{lineTaper, lineFraction, k.TaperStartWidth, func(a *LineAnimation) float64 { return a.Taper.StartWidth }},
		{lineTaper, lineFraction, k.TaperEndWidth, func(a *LineAnimation) float64 { return a.Taper.EndWidth }},
		{lineTaper, lineEase, k.TaperStartEase, func(a *LineAnimation) float64 { return a.Taper.StartEase }},
		{lineTaper, lineEase, k.TaperEndEase, func(a *LineAnimation) float64 { return a.Taper.EndEase }},
	}
}

func (k *LineKeyframes) count() int {
	total := 0
	for _, lane := range k.lanes() {
		total += len(lane.frames)
	}
	return total
}

func (k *LineKeyframes) issue() string {
	for _, lane := range k.lanes() {
		if code := keysIssue(lane.frames, lane.r.low, lane.r.high); code != "" {
			return code
		}
	}
	return ""
}

func (r lineRange) holds(v float64) bool { return within(v, r.low, r.high) }

func (a *LineAnimation) on(group string) bool {
	switch group {
	case lineTrim:
		return a.Trim != nil
	case lineWave:
		return a.Wave != nil
	case lineWiggle:
		return a.Wiggle != nil
	case lineTaper:
		return a.Taper != nil
	}
	return false
}

func (a *LineAnimation) issue() string {
	if a.Trim == nil && a.Wave == nil && a.Wiggle == nil && a.Taper == nil {
		return CodeRequired
	}
	if a.Wave != nil && !lineWaveModes[a.Wave.Mode] {
		return CodeUnknown
	}
	if a.Wiggle != nil && !lineSeedRange.holds(float64(a.Wiggle.Seed)) {
		return CodeInvalid
	}
	if a.Wave != nil && !(a.Wave.Travel >= 0 && a.Wave.Travel < 1) {
		return CodeOutOfRange
	}
	if a.Wiggle != nil && !wiggleTravel.holds(a.Wiggle.Travel) {
		return CodeOutOfRange
	}
	for _, lane := range (&LineKeyframes{}).lanes() {
		if a.on(lane.group) && !lane.r.holds(lane.value(a)) {
			return CodeOutOfRange
		}
	}
	return ""
}

func (l Layer) lineColor() string {
	if l.Stroke != "" {
		return l.Stroke
	}
	if l.strokedShape() {
		return l.Fill
	}
	return ""
}

func (l Layer) takesLine() bool {
	return l.Type == LayerShape && l.paintsStroke() && l.StrokeWidth > 0 && l.lineColor() != "" && l.Shadow == nil && len(l.Effects) == 0
}

func (c Clip) lineIssue() string {
	var keys LineKeyframes
	if c.Keyframes != nil {
		keys = c.Keyframes.LineKeyframes
	}
	if c.Line == nil {
		if keys.count() > 0 {
			return CodeInvalid
		}
		return ""
	}
	if c.Type != ClipOverlay || c.Layer == nil || !c.Layer.takesLine() {
		return CodeInvalid
	}
	if code := c.Line.issue(); code != "" {
		return code
	}
	for _, lane := range keys.lanes() {
		if len(lane.frames) > 0 && !c.Line.on(lane.group) {
			return CodeInvalid
		}
	}
	return ""
}
