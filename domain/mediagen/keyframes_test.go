package mediagen

import (
	"math"
	"testing"
)

type keyframeVector struct {
	name   string
	frames []Keyframe
	atMS   float64
	want   float64
}

func keyframeVectors() []keyframeVector {
	ramp := func(easing Easing) []Keyframe {
		return []Keyframe{{AtMS: 1000, Value: 0, Easing: easing}, {AtMS: 3000, Value: 100, Easing: EaseLinear}}
	}
	return []keyframeVector{
		{"holds the first value before it", ramp(EaseLinear), 0, 0},
		{"holds the last value after it", ramp(EaseLinear), 5000, 100},
		{"linear midpoint", ramp(EaseLinear), 2000, 50},
		{"linear quarter", ramp(EaseLinear), 1500, 25},
		{"hold keeps the value until the next key", ramp(EaseHold), 2999, 0},
		{"hold lands on the next key", ramp(EaseHold), 3000, 100},
		{"ease in starts slow", ramp(EaseIn), 1500, 1.5625},
		{"ease out starts fast", ramp(EaseOut), 1500, 57.8125},
		{"ease in out is symmetric at the middle", ramp(EaseInOut), 2000, 50},
		{"ease in out first quarter", ramp(EaseInOut), 1500, 6.25},
		{"ease in out last quarter", ramp(EaseInOut), 2500, 93.75},
		{"back in pulls back before leaving", ramp(EaseBackIn), 1500, -6.41365625},
		{"back in last quarter", ramp(EaseBackIn), 2500, 18.25903125},
		{"back out overshoots and settles", ramp(EaseBackOut), 2500, 106.41365625},
		{"back in out pulls back and overshoots", ramp(EaseBackInOut), 2500, 109.968184375},
		{"back in out first quarter", ramp(EaseBackInOut), 1500, -9.968184375},
		{"elastic rings past the target", ramp(EaseElastic), 1500, 91.1611652352},
		{"elastic last quarter", ramp(EaseElastic), 2500, 100.5524271728},
		{"bounce first quarter", ramp(EaseBounce), 1500, 47.265625},
		{"bounce last quarter", ramp(EaseBounce), 2500, 97.265625},
		{"spring overshoots early", ramp(EaseSpring), 1500, 102.1143579132},
		{"spring settles back", ramp(EaseSpring), 2500, 97.2042262475},
		{"every easing lands on the next key", ramp(EaseSpring), 3000, 100},
		{"an emphasized entrance curve rushes out", ramp("cubic-bezier(0.05,0.7,0.1,1)"), 1500, 83.1529746487},
		{"an emphasized entrance curve at the middle", ramp("cubic-bezier(0.05,0.7,0.1,1)"), 2000, 95.0247475324},
		{"a bezier with handles past 1 overshoots", ramp("cubic-bezier(0.34,1.56,0.64,1)"), 2000, 108.7400670219},
		{"a bezier with handles past 1 settles back", ramp("cubic-bezier(0.34,1.56,0.64,1)"), 2500, 105.9646859964},
		{"an emphasized exit curve starts slow", ramp("cubic-bezier(0.3, 0, 0.8, 0.15)"), 2500, 40.5585508922},
		{"a single key is constant", []Keyframe{{AtMS: 500, Value: 7, Easing: EaseIn}}, 9000, 7},
		{"the outgoing easing of each key shapes its segment", []Keyframe{
			{AtMS: 0, Value: 0, Easing: EaseLinear}, {AtMS: 1000, Value: 10, Easing: EaseHold}, {AtMS: 2000, Value: 20, Easing: EaseLinear},
		}, 1500, 10},
		{"negative times work for keys before the clip", []Keyframe{{AtMS: -1000, Value: 0, Easing: EaseLinear}, {AtMS: 1000, Value: 10, Easing: EaseLinear}}, 0, 5},
	}
}

func TestKeyframeValuesMatchTheSharedVectors(t *testing.T) {
	for _, v := range keyframeVectors() {
		if got := ValueAt(v.frames, v.atMS); math.Abs(got-v.want) > 1e-9 {
			t.Errorf("%s: got %v want %v", v.name, got, v.want)
		}
	}
}

func TestCustomCurvesAreCheckedAndMeasured(t *testing.T) {
	for _, good := range []Easing{"cubic-bezier(0.05,0.7,0.1,1)", "cubic-bezier( 0.3 , 0 , 0.8 , 0.15 )"} {
		if !good.Known() {
			t.Errorf("%s refused", good)
		}
	}
	for _, bad := range []Easing{"cubic-bezier(1.2,0,0.5,1)", "cubic-bezier(0.2,3,0.5,1)", "cubic-bezier(0.2,0,0.5)", "bezier(0,0,1,1)", "cubic-bezier(a,0,1,1)", "cubic-bezier(-0.1,0,1,1)"} {
		if bad.Known() {
			t.Errorf("%s accepted", bad)
		}
	}
	curve := Easing("cubic-bezier(0.34,1.56,0.64,1)")
	low, high := curve.Reach()
	if low != 0 || high <= 1.09 {
		t.Fatalf("reach = %v %v", low, high)
	}
	for i := 0; i <= 100; i++ {
		if v := curve.Apply(float64(i) / 100); v > high {
			t.Fatalf("%v passes the reach %v", v, high)
		}
	}
	if low, high := Easing("cubic-bezier(0.2,0,0,1)").Reach(); low != 0 || high != 1 {
		t.Fatalf("a curve inside the box reaches %v %v", low, high)
	}
}

func TestTheAnimatedBoxCoversTheOvershootOfTheEasing(t *testing.T) {
	k := &Keyframes{Scale: []Keyframe{{AtMS: 0, Value: 1, Easing: EaseElastic}, {AtMS: 1000, Value: 2, Easing: EaseLinear}}}
	peak := k.MaxScale()
	if peak < 2.37 {
		t.Fatalf("max scale = %v, want the elastic overshoot", peak)
	}
	for ms := 0.0; ms <= 1000; ms += 5 {
		if v := ValueAt(k.Scale, ms); v > peak {
			t.Fatalf("value %v at %vms passes the max scale %v", v, ms, peak)
		}
	}
	if code := KeyframesIssue(k, Transform{X: 0.5, Y: 0.5, W: 1.8, H: 1.8, Opacity: 1}); code != CodeOutOfRange {
		t.Fatalf("an overshoot past the box limit must be refused, got %q", code)
	}
}

func animatedClip(k *Keyframes) Clip {
	return Clip{MediaID: "m", DurationMS: 4000, Fit: FitCover, Transform: Transform{X: 0.5, Y: 0.5, W: 0.5, H: 0.5, Opacity: 1}, Volume: 1, Keyframes: k}
}

func TestKeyframesAreValidated(t *testing.T) {
	ok := &Keyframes{
		X:        []Keyframe{{AtMS: 0, Value: -0.5, Easing: EaseOut}, {AtMS: 1000, Value: 0.5, Easing: EaseLinear}},
		Scale:    []Keyframe{{AtMS: 0, Value: 0.2, Easing: EaseInOut}, {AtMS: 4000, Value: 2, Easing: EaseLinear}},
		Rotation: []Keyframe{{AtMS: 0, Value: -720, Easing: EaseLinear}},
		Opacity:  []Keyframe{{AtMS: 0, Value: 0, Easing: EaseHold}, {AtMS: 500, Value: 1, Easing: EaseLinear}},
	}
	if code := animatedClip(ok).issue(4000, true); code != "" {
		t.Fatalf("valid keyframes refused: %s", code)
	}
	cases := map[string]*Keyframes{
		"unsorted":        {Y: []Keyframe{{AtMS: 1000, Value: 0.1, Easing: EaseLinear}, {AtMS: 1000, Value: 0.2, Easing: EaseLinear}}},
		"unknown easing":  {Y: []Keyframe{{AtMS: 0, Value: 0.1, Easing: "wiggle"}}},
		"opacity range":   {Opacity: []Keyframe{{AtMS: 0, Value: 1.5, Easing: EaseLinear}}},
		"position range":  {X: []Keyframe{{AtMS: 0, Value: 3, Easing: EaseLinear}}},
		"scale range":     {Scale: []Keyframe{{AtMS: 0, Value: 0, Easing: EaseLinear}}},
		"oversized box":   {Scale: []Keyframe{{AtMS: 0, Value: 9, Easing: EaseLinear}}},
		"rotation range":  {Rotation: []Keyframe{{AtMS: 0, Value: 5000, Easing: EaseLinear}}},
		"time range":      {X: []Keyframe{{AtMS: MaxVideoMS + 1, Value: 0.5, Easing: EaseLinear}}},
		"not a number":    {X: []Keyframe{{AtMS: 0, Value: math.NaN(), Easing: EaseLinear}}},
		"empty animation": {},
		"too many on one": {X: manyKeyframes(MaxKeyframesPerProperty + 1)},
	}
	for name, k := range cases {
		if code := animatedClip(k).issue(4000, true); code == "" {
			t.Errorf("%s accepted", name)
		}
	}
	audio := animatedClip(ok)
	if code := audio.issue(4000, false); code != CodeUnexpected {
		t.Errorf("keyframes on audio: %q", code)
	}
}

func manyKeyframes(n int) []Keyframe {
	frames := make([]Keyframe, n)
	for i := range frames {
		frames[i] = Keyframe{AtMS: int64(i * 10), Value: 0.5, Easing: EaseLinear}
	}
	return frames
}

func TestATimelineCapsItsKeyframes(t *testing.T) {
	track := Track{}
	for i := 0; i < MaxTimelineKeyframes/MaxKeyframesPerProperty+1; i++ {
		c := animatedClip(&Keyframes{X: manyKeyframes(MaxKeyframesPerProperty)})
		c.StartMS = int64(i) * 1000
		c.DurationMS = 1000
		track.Clips = append(track.Clips, c)
	}
	timeline := Timeline{DurationMS: int64(len(track.Clips)) * 1000, Background: "#000000", Visual: []Track{track}}
	if code := timeline.issue(); code != CodeTooMany {
		t.Fatalf("got %q", code)
	}
}

func TestKeyframesChangeTheFingerprint(t *testing.T) {
	still := Timeline{DurationMS: 4000, Background: "#000000", Visual: []Track{{Clips: []Clip{animatedClip(nil)}}}}
	moving := Timeline{DurationMS: 4000, Background: "#000000", Visual: []Track{{Clips: []Clip{animatedClip(&Keyframes{X: []Keyframe{{AtMS: 0, Value: 0.1, Easing: EaseLinear}}})}}}}
	eased := Timeline{DurationMS: 4000, Background: "#000000", Visual: []Track{{Clips: []Clip{animatedClip(&Keyframes{X: []Keyframe{{AtMS: 0, Value: 0.1, Easing: EaseIn}}})}}}}
	a, b, c := clipNumbers(still.Visual[0].Clips[0]), clipNumbers(moving.Visual[0].Clips[0]), clipNumbers(eased.Visual[0].Clips[0])
	if a == b || b == c {
		t.Fatalf("fingerprints collide: %q %q %q", a, b, c)
	}
}
