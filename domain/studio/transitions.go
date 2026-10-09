package studio

import (
	"sort"

	"vozko/domain/mediagen"
)

type TransitionKind string

const (
	TransitionDissolve     TransitionKind = "dissolve"
	TransitionDipToColor   TransitionKind = "dipToColor"
	TransitionPush         TransitionKind = "push"
	TransitionSlide        TransitionKind = "slide"
	TransitionWipe         TransitionKind = "wipe"
	TransitionIris         TransitionKind = "iris"
	TransitionClockWipe    TransitionKind = "clockWipe"
	TransitionSplit        TransitionKind = "split"
	TransitionZoom         TransitionKind = "zoom"
	TransitionWhip         TransitionKind = "whip"
	TransitionBlurDissolve TransitionKind = "blurDissolve"
)

const (
	AlignCenter = "center"
	AlignStart  = "start"
	AlignEnd    = "end"

	MinTransitionMS = 100
	MaxTransitionMS = 4_000
)

type option string

const (
	optionDirection   option = "direction"
	optionMoving      option = "moving"
	optionColor       option = "color"
	optionAngle       option = "angle"
	optionShape       option = "shape"
	optionOriginX     option = "originX"
	optionOriginY     option = "originY"
	optionClockwise   option = "clockwise"
	optionOrientation option = "orientation"
	optionReverse     option = "reverse"
	optionAmount      option = "amount"
)

type transitionSpec struct {
	sequential bool
	options    map[option]bool
}

func optionSet(options ...option) map[option]bool {
	set := make(map[option]bool, len(options))
	for _, o := range options {
		set[o] = true
	}
	return set
}

var transitionSpecs = map[TransitionKind]transitionSpec{
	TransitionDissolve:     {options: optionSet()},
	TransitionDipToColor:   {sequential: true, options: optionSet(optionColor)},
	TransitionPush:         {options: optionSet(optionDirection)},
	TransitionSlide:        {options: optionSet(optionDirection, optionMoving)},
	TransitionWipe:         {options: optionSet(optionAngle)},
	TransitionIris:         {options: optionSet(optionShape, optionOriginX, optionOriginY, optionReverse)},
	TransitionClockWipe:    {options: optionSet(optionAngle, optionOriginX, optionOriginY, optionClockwise)},
	TransitionSplit:        {options: optionSet(optionOrientation, optionReverse)},
	TransitionZoom:         {sequential: true, options: optionSet(optionOriginX, optionOriginY, optionAmount)},
	TransitionWhip:         {sequential: true, options: optionSet(optionDirection, optionAmount)},
	TransitionBlurDissolve: {options: optionSet(optionAmount)},
}

var (
	transitionDirections = map[string]bool{"left": true, "right": true, "up": true, "down": true}
	transitionMoves      = map[string]bool{"in": true, "out": true}
	irisShapes           = map[string]bool{"circle": true, "box": true, "diamond": true}
	splitOrientations    = map[string]bool{"horizontal": true, "vertical": true}
	transitionAlignments = map[string]bool{AlignCenter: true, AlignStart: true, AlignEnd: true}
)

type Transition struct {
	ID          string          `json:"id"`
	FromClipID  string          `json:"fromClipId"`
	ToClipID    string          `json:"toClipId"`
	Kind        TransitionKind  `json:"kind"`
	DurationMS  int64           `json:"durationMs"`
	Alignment   string          `json:"alignment"`
	Easing      mediagen.Easing `json:"easing"`
	Direction   *string         `json:"direction,omitempty"`
	Moving      *string         `json:"moving,omitempty"`
	Color       *string         `json:"color,omitempty"`
	Angle       *float64        `json:"angle,omitempty"`
	Shape       *string         `json:"shape,omitempty"`
	OriginX     *float64        `json:"originX,omitempty"`
	OriginY     *float64        `json:"originY,omitempty"`
	Clockwise   *bool           `json:"clockwise,omitempty"`
	Orientation *string         `json:"orientation,omitempty"`
	Reverse     *bool           `json:"reverse,omitempty"`
	Amount      *float64        `json:"amount,omitempty"`
}

func validOption[T any](value *T, allowed bool, valid func(T) bool) bool {
	return value == nil || (allowed && valid(*value))
}

func (t Transition) optionsValid(spec transitionSpec) bool {
	unit := func(v float64) bool { return within(v, 0, 1) }
	in := func(set map[string]bool) func(string) bool { return func(v string) bool { return set[v] } }
	anyBool := func(bool) bool { return true }
	return validOption(t.Direction, spec.options[optionDirection], in(transitionDirections)) &&
		validOption(t.Moving, spec.options[optionMoving], in(transitionMoves)) &&
		validOption(t.Color, spec.options[optionColor], func(v string) bool { return len(v) == 7 && validColor(v, true) }) &&
		validOption(t.Angle, spec.options[optionAngle], func(v float64) bool { return within(v, 0, 359) }) &&
		validOption(t.Shape, spec.options[optionShape], in(irisShapes)) &&
		validOption(t.OriginX, spec.options[optionOriginX], unit) &&
		validOption(t.OriginY, spec.options[optionOriginY], unit) &&
		validOption(t.Clockwise, spec.options[optionClockwise], anyBool) &&
		validOption(t.Orientation, spec.options[optionOrientation], in(splitOrientations)) &&
		validOption(t.Reverse, spec.options[optionReverse], anyBool) &&
		validOption(t.Amount, spec.options[optionAmount], unit)
}

func transitionCap(alignment string, fromMS, toMS int64) int64 {
	switch alignment {
	case AlignStart:
		return toMS / 2
	case AlignEnd:
		return fromMS / 2
	}
	return 2 * min(fromMS/2, toMS/2)
}

func touching(track Track, fromID, toID string) (Clip, Clip, bool) {
	clips := append([]Clip(nil), track.Clips...)
	sort.SliceStable(clips, func(i, j int) bool { return clips[i].StartMS < clips[j].StartMS })
	for i := 1; i < len(clips); i++ {
		from, to := clips[i-1], clips[i]
		if from.ID == fromID && to.ID == toID && from.StartMS+from.DurationMS == to.StartMS {
			return from, to, true
		}
	}
	return Clip{}, Clip{}, false
}

func (t Transition) issue(track Track) string {
	spec, known := transitionSpecs[t.Kind]
	if !known {
		return CodeUnknown
	}
	from, to, ok := touching(track, t.FromClipID, t.ToClipID)
	switch {
	case !ok:
		return CodeInvalid
	case t.DurationMS < MinTransitionMS || t.DurationMS > MaxTransitionMS:
		return CodeOutOfRange
	case !transitionAlignments[t.Alignment] || (spec.sequential && t.Alignment != AlignCenter):
		return CodeInvalid
	case !t.Easing.Known() || t.Easing == mediagen.EaseHold:
		return CodeUnknown
	case !t.optionsValid(spec):
		return CodeInvalid
	case t.DurationMS > transitionCap(t.Alignment, from.DurationMS, to.DurationMS):
		return CodeOutOfRange
	}
	return ""
}

func (track Track) transitionsIssue(ids map[string]bool) string {
	if len(track.Transitions) > 0 && track.Kind != TrackVisual {
		return CodeInvalid
	}
	cuts := map[string]bool{}
	for _, t := range track.Transitions {
		if !validToken(t.ID) || ids[t.ID] {
			return CodeInvalid
		}
		ids[t.ID] = true
		cut := t.FromClipID + ">" + t.ToClipID
		if cuts[cut] {
			return CodeInvalid
		}
		cuts[cut] = true
		if code := t.issue(track); code != "" {
			return code
		}
	}
	return ""
}
