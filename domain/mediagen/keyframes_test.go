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
		"unknown easing":  {Y: []Keyframe{{AtMS: 0, Value: 0.1, Easing: "bounce"}}},
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
