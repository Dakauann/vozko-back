package studio

import (
	"testing"

	"vozko/domain/mediagen"
)

func crossfadeDoc() VideoDocument {
	doc := cutDoc()
	doc.Tracks[1].Clips = []Clip{
		{ID: "s1", Type: ClipAudio, AssetID: "song", DurationMS: 3_000, Volume: 1},
		{ID: "s2", Type: ClipAudio, AssetID: "song", StartMS: 3_000, DurationMS: 3_000, TrimInMS: 2_000, Volume: 1},
	}
	doc.Tracks[1].Transitions = []Transition{{ID: "x1", FromClipID: "s1", ToClipID: "s2", Kind: TransitionConstantPower, DurationMS: 1_000, Alignment: AlignCenter, Easing: mediagen.EaseLinear}}
	return doc
}

func TestAudioCrossfadesSitOnAudioCutsOnly(t *testing.T) {
	doc := crossfadeDoc()
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatalf("a constant power crossfade refused: %v", err)
	}
	doc.Tracks[1].Transitions[0].Kind = TransitionConstantGain
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatalf("a constant gain crossfade refused: %v", err)
	}
	cases := map[string]func(*VideoDocument){
		"a picture transition on sound": func(d *VideoDocument) { d.Tracks[1].Transitions[0].Kind = TransitionDissolve },
		"a crossfade on pictures": func(d *VideoDocument) {
			tr := dissolve("x2")
			tr.Kind = TransitionConstantPower
			d.Tracks[0].Transitions = []Transition{tr}
		},
		"an option on a crossfade":     func(d *VideoDocument) { d.Tracks[1].Transitions[0].Amount = ptr(0.5) },
		"a crossfade past half a clip": func(d *VideoDocument) { d.Tracks[1].Transitions[0].DurationMS = 4_000 },
	}
	for name, change := range cases {
		d := crossfadeDoc()
		change(&d)
		if codeOf(t, ValidateDocument(KindVideo, mustJSON(t, d)))[FieldTracks] == "" {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestPanSitsOnSoundOnly(t *testing.T) {
	doc := videoDoc()
	doc.Tracks[2].Pan = ptr(-0.5)
	doc.Tracks[2].Clips[0].Pan = 0.25
	doc.Tracks[2].Clips[0].Keyframes = &ClipKeyframes{
		GainDB: []mediagen.Keyframe{{AtMS: 0, Value: -6, Easing: mediagen.EaseLinear}},
		Pan:    []mediagen.Keyframe{{AtMS: 0, Value: -1, Easing: mediagen.EaseLinear}, {AtMS: 1_000, Value: 1, Easing: mediagen.EaseInOut}},
	}
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatalf("a panned song refused: %v", err)
	}
	cases := map[string]func(*VideoDocument){
		"a track pan past the right": func(d *VideoDocument) { d.Tracks[2].Pan = ptr(1.5) },
		"a pan on a picture track":   func(d *VideoDocument) { d.Tracks[0].Pan = ptr(0.2) },
		"a clip pan past the left":   func(d *VideoDocument) { d.Tracks[2].Clips[0].Pan = -1.2 },
		"a pan on a video clip":      func(d *VideoDocument) { d.Tracks[0].Clips[1].Pan = 0.3 },
		"a pan key past the range":   func(d *VideoDocument) { d.Tracks[2].Clips[0].Keyframes.Pan[0].Value = -2 },
		"pan keys on a video clip": func(d *VideoDocument) {
			d.Tracks[0].Clips[1].Keyframes = &ClipKeyframes{Pan: []mediagen.Keyframe{{AtMS: 0, Value: 0.5, Easing: mediagen.EaseLinear}}}
		},
	}
	for name, change := range cases {
		d := doc
		d.Tracks = append([]Track(nil), doc.Tracks...)
		for i := range d.Tracks {
			d.Tracks[i].Clips = append([]Clip(nil), doc.Tracks[i].Clips...)
		}
		keys := *doc.Tracks[2].Clips[0].Keyframes
		keys.Pan = append([]mediagen.Keyframe(nil), keys.Pan...)
		d.Tracks[2].Clips[0].Keyframes = &keys
		change(&d)
		if codeOf(t, ValidateDocument(KindVideo, mustJSON(t, d)))[FieldTracks] == "" {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestPanKeysShareTheAudioKeyLimit(t *testing.T) {
	doc := videoDoc()
	frames := make([]mediagen.Keyframe, 200)
	for i := range frames {
		frames[i] = mediagen.Keyframe{AtMS: int64(i * 20), Value: 0.5, Easing: mediagen.EaseLinear}
	}
	doc.Tracks[2].Clips[0].Keyframes = &ClipKeyframes{Pan: frames}
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatalf("a long pan lane refused: %v", err)
	}
}
