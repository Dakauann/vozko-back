package studio

import "vozko/domain/mediagen"

const (
	RepeatLinear = "linear"
	RepeatAbove  = "above"
	RepeatBelow  = "below"

	MinClipRepeatCopies   = 1
	MaxClipRepeatShift    = 10000.0
	MaxClipRepeatStep     = 4096.0
	MinClipRepeatScale    = 0.01
	MaxClipRepeatScale    = 10.0
	MaxClipRepeatRotation = 3600.0

	clipRepeatCopies       = "copies"
	clipRepeatShift        = "shift"
	clipRepeatX            = "x"
	clipRepeatY            = "y"
	clipRepeatScale        = "scale"
	clipRepeatRotation     = "rotation"
	clipRepeatStartOpacity = "startOpacity"
	clipRepeatEndOpacity   = "endOpacity"
	clipRepeatRadius       = "radius"
	clipRepeatStartAngle   = "startAngle"
	clipRepeatSweep        = "sweep"
	clipRepeatGapX         = "gapX"
	clipRepeatGapY         = "gapY"
	clipRepeatAngle        = "angle"
	clipRepeatGap          = "gap"
)

var (
	clipRepeatModes  = map[string]bool{RepeatLinear: true, RepeatRadial: true, RepeatGrid: true, RepeatMirror: true}
	clipRepeatOrders = map[string]bool{"": true, RepeatAbove: true, RepeatBelow: true}
	repeatGapRange   = ValueRange{-MaxRepeatGap, MaxRepeatGap}
	clipRepeatRanges = map[string]ValueRange{
		clipRepeatCopies:       {MinClipRepeatCopies, unbounded},
		clipRepeatShift:        {-MaxClipRepeatShift, MaxClipRepeatShift},
		clipRepeatX:            {-MaxClipRepeatStep, MaxClipRepeatStep},
		clipRepeatY:            {-MaxClipRepeatStep, MaxClipRepeatStep},
		clipRepeatScale:        {MinClipRepeatScale, MaxClipRepeatScale},
		clipRepeatRotation:     {-MaxClipRepeatRotation, MaxClipRepeatRotation},
		clipRepeatStartOpacity: {0, 1},
		clipRepeatEndOpacity:   {0, 1},
		clipRepeatRadius:       {1, MaxRepeatRadius},
		clipRepeatStartAngle:   {-MaxRepeatAngle, MaxRepeatAngle},
		clipRepeatSweep:        {1, MaxRepeatSweep},
		clipRepeatGapX:         repeatGapRange,
		clipRepeatGapY:         repeatGapRange,
		clipRepeatAngle:        {0, MaxRepeatAngle},
		clipRepeatGap:          repeatGapRange,
	}
	clipRepeatKeyedByMode = map[string]map[string]bool{
		RepeatLinear: {clipRepeatCopies: true, clipRepeatShift: true, clipRepeatX: true, clipRepeatY: true, clipRepeatScale: true, clipRepeatRotation: true, clipRepeatStartOpacity: true, clipRepeatEndOpacity: true},
		RepeatRadial: {clipRepeatRadius: true, clipRepeatStartAngle: true, clipRepeatSweep: true, clipRepeatStartOpacity: true, clipRepeatEndOpacity: true},
		RepeatGrid:   {clipRepeatGapX: true, clipRepeatGapY: true, clipRepeatStartOpacity: true, clipRepeatEndOpacity: true},
		RepeatMirror: {clipRepeatAngle: true, clipRepeatGap: true, clipRepeatStartOpacity: true, clipRepeatEndOpacity: true},
	}
)

type ClipRepeatKeyframes struct {
	Copies       []mediagen.Keyframe `json:"copies,omitempty"`
	Shift        []mediagen.Keyframe `json:"shift,omitempty"`
	X            []mediagen.Keyframe `json:"x,omitempty"`
	Y            []mediagen.Keyframe `json:"y,omitempty"`
	Scale        []mediagen.Keyframe `json:"scale,omitempty"`
	Rotation     []mediagen.Keyframe `json:"rotation,omitempty"`
	StartOpacity []mediagen.Keyframe `json:"startOpacity,omitempty"`
	EndOpacity   []mediagen.Keyframe `json:"endOpacity,omitempty"`
	Radius       []mediagen.Keyframe `json:"radius,omitempty"`
	StartAngle   []mediagen.Keyframe `json:"startAngle,omitempty"`
	Sweep        []mediagen.Keyframe `json:"sweep,omitempty"`
	GapX         []mediagen.Keyframe `json:"gapX,omitempty"`
	GapY         []mediagen.Keyframe `json:"gapY,omitempty"`
	Angle        []mediagen.Keyframe `json:"angle,omitempty"`
	Gap          []mediagen.Keyframe `json:"gap,omitempty"`
}

type ClipRepeat struct {
	Repeat
	Mode         string               `json:"mode"`
	Copies       int                  `json:"copies,omitempty"`
	Shift        float64              `json:"shift,omitempty"`
	X            float64              `json:"x,omitempty"`
	Y            float64              `json:"y,omitempty"`
	Scale        *float64             `json:"scale,omitempty"`
	Rotation     float64              `json:"rotation,omitempty"`
	StartOpacity *float64             `json:"startOpacity,omitempty"`
	EndOpacity   *float64             `json:"endOpacity,omitempty"`
	Order        string               `json:"order,omitempty"`
	Keyframes    *ClipRepeatKeyframes `json:"keyframes,omitempty"`
}

type clipRepeatLane struct {
	field  string
	frames []mediagen.Keyframe
}

func ClipRepeatFieldRange(field string) (ValueRange, bool) {
	r, known := clipRepeatRanges[field]
	return r, known
}

func (k *ClipRepeatKeyframes) lanes() []clipRepeatLane {
	if k == nil {
		return nil
	}
	return []clipRepeatLane{
		{clipRepeatCopies, k.Copies},
		{clipRepeatShift, k.Shift},
		{clipRepeatX, k.X},
		{clipRepeatY, k.Y},
		{clipRepeatScale, k.Scale},
		{clipRepeatRotation, k.Rotation},
		{clipRepeatStartOpacity, k.StartOpacity},
		{clipRepeatEndOpacity, k.EndOpacity},
		{clipRepeatRadius, k.Radius},
		{clipRepeatStartAngle, k.StartAngle},
		{clipRepeatSweep, k.Sweep},
		{clipRepeatGapX, k.GapX},
		{clipRepeatGapY, k.GapY},
		{clipRepeatAngle, k.Angle},
		{clipRepeatGap, k.Gap},
	}
}

func holdsPtr(value *float64, field string) bool {
	return value == nil || clipRepeatRanges[field].Holds(*value)
}

func (c ClipRepeat) imageRepeat() Repeat {
	r := c.Repeat
	r.Mode = c.Mode
	return r
}

func (c ClipRepeat) linearSet() bool {
	return c.Copies != 0 || c.Shift != 0 || c.X != 0 || c.Y != 0 || (c.Scale != nil && *c.Scale != 0) || c.Rotation != 0
}

func (c ClipRepeat) linearIssue() string {
	r := c.Repeat
	switch {
	case r.radialSet() || r.gridSet() || r.mirrorSet():
		return CodeInvalid
	case c.Copies < MinClipRepeatCopies:
		return CodeOutOfRange
	case !clipRepeatRanges[clipRepeatShift].Holds(c.Shift) || !clipRepeatRanges[clipRepeatX].Holds(c.X) || !clipRepeatRanges[clipRepeatY].Holds(c.Y):
		return CodeOutOfRange
	case !holdsPtr(c.Scale, clipRepeatScale) || !clipRepeatRanges[clipRepeatRotation].Holds(c.Rotation):
		return CodeOutOfRange
	}
	return ""
}

func (c ClipRepeat) shapeIssue() string {
	if c.Mode == RepeatLinear {
		return c.linearIssue()
	}
	if c.linearSet() {
		return CodeInvalid
	}
	return c.imageRepeat().issue()
}

func (c *ClipRepeat) issue() string {
	if c == nil {
		return ""
	}
	switch {
	case !clipRepeatModes[c.Mode] || !clipRepeatOrders[c.Order]:
		return CodeUnknown
	case !holdsPtr(c.StartOpacity, clipRepeatStartOpacity) || !holdsPtr(c.EndOpacity, clipRepeatEndOpacity):
		return CodeOutOfRange
	}
	if code := c.shapeIssue(); code != "" {
		return code
	}
	keyed := clipRepeatKeyedByMode[c.Mode]
	for _, lane := range c.Keyframes.lanes() {
		if len(lane.frames) == 0 {
			continue
		}
		if !keyed[lane.field] {
			return CodeInvalid
		}
		r := clipRepeatRanges[lane.field]
		if code := keysIssue(lane.frames, r.Low, r.High); code != "" {
			return code
		}
	}
	return ""
}
