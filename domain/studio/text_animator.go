package studio

import (
	"slices"

	"vozko/domain/mediagen"
)

const (
	SelectorRange  = "range"
	SelectorWiggly = "wiggly"

	MaxAnimatorOffsetPX  = 4_000
	MaxAnimatorScale     = 1_000
	MaxAnimatorRotation  = 3_600
	MaxAnimatorBlur      = 100
	MaxAnimatorTracking  = 1_000
	MaxRangeIndex        = 100_000
	MinWiggleRate        = 0.1
	MaxWiggleRate        = 20
	MinWiggleLoopMS      = 100
	maxSelectorPercent   = 100
	maxSelectorAmountAbs = 100
)

type AnimatorProperties struct {
	X         *float64 `json:"x,omitempty"`
	Y         *float64 `json:"y,omitempty"`
	Scale     *float64 `json:"scale,omitempty"`
	Rotation  *float64 `json:"rotation,omitempty"`
	RotationX *float64 `json:"rotationX,omitempty"`
	RotationY *float64 `json:"rotationY,omitempty"`
	Opacity   *float64 `json:"opacity,omitempty"`
	Blur      *float64 `json:"blur,omitempty"`
	Tracking  *float64 `json:"tracking,omitempty"`
	Fill      *string  `json:"fill,omitempty"`
	Stroke    *string  `json:"stroke,omitempty"`
}

type TextSelector struct {
	Kind        string   `json:"kind"`
	Seed        int64    `json:"seed"`
	Amount      float64  `json:"amount"`
	Units       *string  `json:"units,omitempty"`
	Start       *float64 `json:"start,omitempty"`
	End         *float64 `json:"end,omitempty"`
	Offset      *float64 `json:"offset,omitempty"`
	Order       *string  `json:"order,omitempty"`
	Shape       *string  `json:"shape,omitempty"`
	EaseHigh    *float64 `json:"easeHigh,omitempty"`
	EaseLow     *float64 `json:"easeLow,omitempty"`
	Rate        *float64 `json:"rate,omitempty"`
	LoopMS      *int64   `json:"loopMs,omitempty"`
	Correlation *float64 `json:"correlation,omitempty"`
	OffsetMS    *int64   `json:"offsetMs,omitempty"`
}

type TextUnitAnimator struct {
	ID         string                         `json:"id"`
	Unit       TextUnit                       `json:"unit"`
	Properties *AnimatorProperties            `json:"properties"`
	Selector   *TextSelector                  `json:"selector"`
	Keyframes  map[string][]mediagen.Keyframe `json:"keyframes,omitempty"`
}

func SelectorKinds() []string { return []string{SelectorRange, SelectorWiggly} }

func RangeOrders() []string {
	return []string{"forward", "backward", "centerOut", "edgesIn", "random"}
}

func RangeShapes() []string {
	return []string{"square", "rampUp", "rampDown", "triangle", "round", "smooth"}
}

func RangeUnits() []string { return []string{"percent", "index"} }

func listedPtr(list []string, value *string) bool {
	return value != nil && slices.Contains(list, *value)
}

func withinPtr(value *float64, lo, hi float64) bool {
	return value != nil && within(*value, lo, hi)
}

func wholeWithinPtr(value *int64, lo, hi int64) bool {
	return value != nil && *value >= lo && *value <= hi
}

func (s TextSelector) rangeEdges() (float64, float64) {
	if s.Units != nil && *s.Units == "index" {
		return MaxRangeIndex, MaxRangeIndex
	}
	return maxSelectorPercent, maxSelectorPercent
}

func (s TextSelector) keyRange(key string) (float64, float64, bool) {
	if s.Kind == SelectorWiggly {
		return 0, maxSelectorPercent, key == "amount"
	}
	edge, offset := s.rangeEdges()
	switch key {
	case "start", "end":
		return 0, edge, true
	case "offset":
		return -offset, offset, true
	case "amount":
		return -maxSelectorAmountAbs, maxSelectorAmountAbs, true
	}
	return 0, 0, false
}

func (s TextSelector) issue() string {
	if !slices.Contains(SelectorKinds(), s.Kind) {
		return CodeUnknown
	}
	rangeOnly := s.Units != nil || s.Start != nil || s.End != nil || s.Offset != nil || s.Order != nil || s.Shape != nil || s.EaseHigh != nil || s.EaseLow != nil
	wigglyOnly := s.Rate != nil || s.LoopMS != nil || s.Correlation != nil || s.OffsetMS != nil
	if (s.Kind == SelectorRange && wigglyOnly) || (s.Kind == SelectorWiggly && rangeOnly) {
		return CodeInvalid
	}
	if s.Seed < 0 || s.Seed > MaxVfxSeed {
		return CodeOutOfRange
	}
	if s.Kind == SelectorWiggly {
		switch {
		case !withinPtr(s.Rate, MinWiggleRate, MaxWiggleRate) || !wholeWithinPtr(s.LoopMS, MinWiggleLoopMS, MaxVideoMS) || !withinPtr(s.Correlation, 0, maxSelectorPercent) || !within(s.Amount, 0, maxSelectorPercent):
			return CodeOutOfRange
		case s.OffsetMS != nil && !wholeWithinPtr(s.OffsetMS, 0, MaxVideoMS):
			return CodeOutOfRange
		}
		return ""
	}
	if !listedPtr(RangeUnits(), s.Units) || !listedPtr(RangeOrders(), s.Order) || !listedPtr(RangeShapes(), s.Shape) {
		return CodeUnknown
	}
	edge, offset := s.rangeEdges()
	switch {
	case !withinPtr(s.Start, 0, edge) || !withinPtr(s.End, 0, edge) || !withinPtr(s.Offset, -offset, offset):
		return CodeOutOfRange
	case !withinPtr(s.EaseHigh, -maxSelectorPercent, maxSelectorPercent) || !withinPtr(s.EaseLow, -maxSelectorPercent, maxSelectorPercent) || !within(s.Amount, -maxSelectorAmountAbs, maxSelectorAmountAbs):
		return CodeOutOfRange
	}
	return ""
}

func (p AnimatorProperties) issue(layer Layer) string {
	for _, color := range []struct {
		value  *string
		paints bool
	}{{p.Fill, layer.paintsFill()}, {p.Stroke, layer.paintsStroke()}} {
		if color.value != nil && (!hexColor(*color.value) || !color.paints) {
			return CodeInvalid
		}
	}
	for _, number := range []struct {
		value  *float64
		lo, hi float64
	}{
		{p.X, -MaxAnimatorOffsetPX, MaxAnimatorOffsetPX},
		{p.Y, -MaxAnimatorOffsetPX, MaxAnimatorOffsetPX},
		{p.Scale, 0, MaxAnimatorScale},
		{p.Rotation, -MaxAnimatorRotation, MaxAnimatorRotation},
		{p.RotationX, -MaxAnimatorRotation, MaxAnimatorRotation},
		{p.RotationY, -MaxAnimatorRotation, MaxAnimatorRotation},
		{p.Opacity, 0, maxSelectorPercent},
		{p.Blur, 0, MaxAnimatorBlur},
		{p.Tracking, -MaxAnimatorTracking, MaxAnimatorTracking},
	} {
		if number.value != nil && !within(*number.value, number.lo, number.hi) {
			return CodeOutOfRange
		}
	}
	return ""
}

func (a TextUnitAnimator) issue(layer Layer) string {
	if !slices.Contains(TextUnits(), a.Unit) {
		return CodeUnknown
	}
	if a.Properties == nil || a.Selector == nil {
		return CodeInvalid
	}
	if code := a.Properties.issue(layer); code != "" {
		return code
	}
	if code := a.Selector.issue(); code != "" {
		return code
	}
	for key, frames := range a.Keyframes {
		if len(frames) == 0 {
			continue
		}
		lo, hi, ok := a.Selector.keyRange(key)
		if !ok {
			return CodeInvalid
		}
		if code := keysIssue(frames, lo, hi); code != "" {
			return code
		}
	}
	return ""
}

func (c Clip) textAnimatorsIssue() string {
	if len(c.TextAnimators) == 0 {
		return ""
	}
	if !c.animatesText() {
		return CodeInvalid
	}
	seen := make(map[string]bool, len(c.TextAnimators))
	for _, animator := range c.TextAnimators {
		if !validToken(animator.ID) || seen[animator.ID] {
			return CodeInvalid
		}
		seen[animator.ID] = true
		if code := animator.issue(*c.Layer); code != "" {
			return code
		}
	}
	return ""
}
