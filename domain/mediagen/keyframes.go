package mediagen

import (
	"math"
	"regexp"
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
	EaseBackIn              Easing = "backIn"
	EaseBackOut             Easing = "backOut"
	EaseBackInOut           Easing = "backInOut"
	EaseElastic             Easing = "elastic"
	EaseBounce              Easing = "bounce"
	EaseSpring              Easing = "spring"
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

const (
	backPull        = 1.70158
	backPullInOut   = backPull * 1.525
	elasticPeriod   = 2 * math.Pi / 3
	bounceGain      = 7.5625
	bounceStep      = 2.75
	SpringDamping   = 0.5
	SpringFrequency = 10.0
)

var easingReach = map[Easing][2]float64{
	EaseLinear:    {0, 1},
	EaseHold:      {0, 1},
	EaseIn:        {0, 1},
	EaseOut:       {0, 1},
	EaseInOut:     {0, 1},
	EaseBackIn:    {-0.101, 1},
	EaseBackOut:   {0, 1.101},
	EaseBackInOut: {-0.101, 1.101},
	EaseElastic:   {0, 1.374},
	EaseBounce:    {0, 1},
	EaseSpring:    {0, 1.161},
}

func Easings() []Easing {
	return []Easing{EaseLinear, EaseHold, EaseIn, EaseOut, EaseInOut, EaseBackIn, EaseBackOut, EaseBackInOut, EaseElastic, EaseBounce, EaseSpring}
}

const (
	MinBezierY = -1.0
	MaxBezierY = 2.0
)

var bezierPattern = regexp.MustCompile(`^cubic-bezier\(\s*(-?(?:\d+(?:\.\d+)?|\.\d+))\s*,\s*(-?(?:\d+(?:\.\d+)?|\.\d+))\s*,\s*(-?(?:\d+(?:\.\d+)?|\.\d+))\s*,\s*(-?(?:\d+(?:\.\d+)?|\.\d+))\s*\)$`)

type Bezier [4]float64

func (e Easing) Bezier() (Bezier, bool) {
	match := bezierPattern.FindStringSubmatch(string(e))
	if match == nil {
		return Bezier{}, false
	}
	var b Bezier
	for i := range b {
		v, err := strconv.ParseFloat(match[i+1], 64)
		if err != nil {
			return Bezier{}, false
		}
		b[i] = v
	}
	inX := func(x float64) bool { return x >= 0 && x <= 1 }
	inY := func(y float64) bool { return y >= MinBezierY && y <= MaxBezierY }
	if !inX(b[0]) || !inX(b[2]) || !inY(b[1]) || !inY(b[3]) {
		return Bezier{}, false
	}
	return b, true
}

func (e Easing) Known() bool {
	if _, ok := easingReach[e]; ok {
		return true
	}
	_, ok := e.Bezier()
	return ok
}

func (e Easing) Reach() (float64, float64) {
	if reach, ok := easingReach[e]; ok {
		return reach[0], reach[1]
	}
	if b, ok := e.Bezier(); ok {
		return b.reach()
	}
	return 0, 1
}

func coefficients(a, b float64) (float64, float64, float64) {
	c := 3 * a
	bb := 3*(b-a) - c
	return 1 - c - bb, bb, c
}

func (b Bezier) At(p float64) float64 {
	if p <= 0 {
		return 0
	}
	if p >= 1 {
		return 1
	}
	ax, bx, cx := coefficients(b[0], b[2])
	ay, by, cy := coefficients(b[1], b[3])
	xAt := func(t float64) float64 { return ((ax*t+bx)*t + cx) * t }
	t := p
	solved := false
	for i := 0; i < 16 && !solved; i++ {
		err := xAt(t) - p
		slope := (3*ax*t+2*bx)*t + cx
		if math.Abs(err) < 1e-12 {
			solved = true
		} else if math.Abs(slope) < 1e-6 {
			break
		} else {
			t -= err / slope
		}
	}
	if !solved {
		low, high := 0.0, 1.0
		t = p
		for i := 0; i < 100; i++ {
			x := xAt(t)
			if math.Abs(x-p) < 1e-12 {
				break
			}
			if p > x {
				low = t
			} else {
				high = t
			}
			t = (low + high) / 2
		}
	}
	return ((ay*t+by)*t + cy) * t
}

func turningPoints(a, b, c float64) []float64 {
	if math.Abs(a) < 1e-12 {
		if math.Abs(b) < 1e-12 {
			return nil
		}
		return []float64{-c / (2 * b)}
	}
	disc := 4*b*b - 12*a*c
	if disc < 0 {
		return nil
	}
	root := math.Sqrt(disc)
	return []float64{(-2*b + root) / (6 * a), (-2*b - root) / (6 * a)}
}

func (b Bezier) reach() (float64, float64) {
	ay, by, cy := coefficients(b[1], b[3])
	low, high := 0.0, 1.0
	for _, t := range turningPoints(ay, by, cy) {
		if t > 0 && t < 1 {
			y := ((ay*t+by)*t + cy) * t
			low, high = math.Min(low, y), math.Max(high, y)
		}
	}
	return math.Floor(low*1000) / 1000, math.Ceil(high*1000) / 1000
}

func bounceOut(p float64) float64 {
	switch {
	case p < 1/bounceStep:
		return bounceGain * p * p
	case p < 2/bounceStep:
		p -= 1.5 / bounceStep
		return bounceGain*p*p + 0.75
	case p < 2.5/bounceStep:
		p -= 2.25 / bounceStep
		return bounceGain*p*p + 0.9375
	}
	p -= 2.625 / bounceStep
	return bounceGain*p*p + 0.984375
}

func springRaw(p float64) float64 {
	damped := SpringFrequency * math.Sqrt(1-SpringDamping*SpringDamping)
	decay := SpringDamping * SpringFrequency
	return 1 - math.Exp(-decay*p)*(math.Cos(damped*p)+decay/damped*math.Sin(damped*p))
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
	case EaseBackIn:
		return (backPull+1)*p*p*p - backPull*p*p
	case EaseBackOut:
		return 1 + (backPull+1)*math.Pow(p-1, 3) + backPull*math.Pow(p-1, 2)
	case EaseBackInOut:
		if p < 0.5 {
			return math.Pow(2*p, 2) * ((backPullInOut+1)*2*p - backPullInOut) / 2
		}
		return (math.Pow(2*p-2, 2)*((backPullInOut+1)*(2*p-2)+backPullInOut) + 2) / 2
	case EaseElastic:
		if p <= 0 || p >= 1 {
			return math.Max(0, math.Min(1, p))
		}
		return math.Pow(2, -10*p)*math.Sin((10*p-0.75)*elasticPeriod) + 1
	case EaseBounce:
		return bounceOut(p)
	case EaseSpring:
		if p >= 1 {
			return 1
		}
		return springRaw(p) / springRaw(1)
	}
	if b, ok := e.Bezier(); ok {
		return b.At(p)
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

func CurveExtent(frames []Keyframe) (float64, float64) {
	low, high := Extent(frames)
	for i := 0; i < len(frames)-1; i++ {
		from, to := frames[i].Value, frames[i+1].Value
		lowReach, highReach := frames[i].Easing.Reach()
		a, b := from+(to-from)*lowReach, from+(to-from)*highReach
		low, high = math.Min(low, math.Min(a, b)), math.Max(high, math.Max(a, b))
	}
	return low, high
}

func (k *Keyframes) MaxScale() float64 {
	if k == nil || len(k.Scale) == 0 {
		return 1
	}
	_, high := CurveExtent(k.Scale)
	return high
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
