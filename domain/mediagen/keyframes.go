package mediagen

import (
	"math"
	"strconv"
	"strings"
)

type Easing string

const (
	EaseLinear              Easing = "linear"
	EaseHold                Easing = "hold"
	EaseIn                  Easing = "easeIn"
	EaseOut                 Easing = "easeOut"
	EaseInOut               Easing = "easeInOut"
	MaxKeyframesPerProperty        = 32
	MaxTimelineKeyframes           = 400
	MinKeyframeScale               = 0.05
	MaxKeyframeScale               = 5.0
	MaxKeyframeRotation            = 3600.0
	MinKeyframePosition            = -1.0
	MaxKeyframePosition            = 2.0
	maxAnimatedBox                 = 4.0
)

type Keyframe struct {
	AtMS   int64   `json:"atMs"`
	Value  float64 `json:"value"`
	Easing Easing  `json:"easing"`
}

type Keyframes struct {
	X        []Keyframe `json:"x,omitempty"`
	Y        []Keyframe `json:"y,omitempty"`
	Scale    []Keyframe `json:"scale,omitempty"`
	Rotation []Keyframe `json:"rotation,omitempty"`
	Opacity  []Keyframe `json:"opacity,omitempty"`
}

func (e Easing) Known() bool {
	switch e {
	case EaseLinear, EaseHold, EaseIn, EaseOut, EaseInOut:
		return true
	}
	return false
}

func (e Easing) Apply(p float64) float64 {
	switch e {
	case EaseHold:
		return 0
	case EaseIn:
		return p * p * p
	case EaseOut:
		return 1 - math.Pow(1-p, 3)
	case EaseInOut:
		if p < 0.5 {
			return 4 * p * p * p
		}
		return 1 - math.Pow(-2*p+2, 3)/2
	}
	return p
}

func ValueAt(frames []Keyframe, atMS float64) float64 {
	if len(frames) == 0 {
		return 0
	}
	if atMS <= float64(frames[0].AtMS) {
		return frames[0].Value
	}
	for i := 0; i < len(frames)-1; i++ {
		from, to := frames[i], frames[i+1]
		if atMS < float64(to.AtMS) {
			p := (atMS - float64(from.AtMS)) / float64(to.AtMS-from.AtMS)
			return from.Value + (to.Value-from.Value)*from.Easing.Apply(p)
		}
	}
	return frames[len(frames)-1].Value
}

func Extent(frames []Keyframe) (float64, float64) {
	low, high := math.Inf(1), math.Inf(-1)
	for _, f := range frames {
		low, high = math.Min(low, f.Value), math.Max(high, f.Value)
	}
	return low, high
}

func (k *Keyframes) Count() int {
	if k == nil {
		return 0
	}
	return len(k.X) + len(k.Y) + len(k.Scale) + len(k.Rotation) + len(k.Opacity)
}

func (k *Keyframes) MaxScale() float64 {
	if k == nil || len(k.Scale) == 0 {
		return 1
	}
	_, high := Extent(k.Scale)
	return math.Max(high, ValueAt(k.Scale, 0))
}

func propertyIssue(frames []Keyframe, low, high float64) string {
	if len(frames) > MaxKeyframesPerProperty {
		return CodeTooMany
	}
	for i, f := range frames {
		switch {
		case !f.Easing.Known():
			return CodeUnknown
		case f.AtMS < -MaxVideoMS || f.AtMS > MaxVideoMS:
			return CodeOutOfRange
		case math.IsNaN(f.Value) || f.Value < low || f.Value > high:
			return CodeOutOfRange
		case i > 0 && f.AtMS <= frames[i-1].AtMS:
			return CodeOutOfRange
		}
	}
	return ""
}

func KeyframesIssue(k *Keyframes, box Transform) string {
	if k == nil {
		return ""
	}
	if k.Count() == 0 {
		return CodeRequired
	}
	checks := []struct {
		frames    []Keyframe
		low, high float64
	}{
		{k.X, MinKeyframePosition, MaxKeyframePosition},
		{k.Y, MinKeyframePosition, MaxKeyframePosition},
		{k.Scale, MinKeyframeScale, MaxKeyframeScale},
		{k.Rotation, -MaxKeyframeRotation, MaxKeyframeRotation},
		{k.Opacity, 0, 1},
	}
	for _, c := range checks {
		if code := propertyIssue(c.frames, c.low, c.high); code != "" {
			return code
		}
	}
	if scale := k.MaxScale(); box.W*scale > maxAnimatedBox || box.H*scale > maxAnimatedBox {
		return CodeOutOfRange
	}
	return ""
}

func keyframesPart(k *Keyframes) string {
	if k == nil {
		return "-"
	}
	property := func(name string, frames []Keyframe) string {
		parts := make([]string, 0, len(frames))
		for _, f := range frames {
			parts = append(parts, strconv.FormatInt(f.AtMS, 10)+"="+strconv.FormatFloat(f.Value, 'f', 4, 64)+":"+string(f.Easing))
		}
		return name + "[" + strings.Join(parts, " ") + "]"
	}
	return property("x", k.X) + property("y", k.Y) + property("s", k.Scale) + property("r", k.Rotation) + property("o", k.Opacity)
}
