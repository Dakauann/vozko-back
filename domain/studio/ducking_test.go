package studio

import (
	"testing"

	"vozko/domain/mediagen"
)

func duckedDoc() VideoDocument {
	doc := videoDoc()
	doc.Tracks[2].Ducking = &DuckingRule{TriggerTrackIDs: []string{"muted"}, ReduceDB: 12, AttackMS: 250, ReleaseMS: 500, HoldMS: 300, Sensitivity: 0.5}
	doc.Tracks[2].GainDB = ptr(-3.0)
	return doc
}

func TestAudioTracksCarryGainAndDucking(t *testing.T) {
	doc := duckedDoc()
	doc.Tracks[2].Clips[0].Keyframes = &ClipKeyframes{GainDB: []mediagen.Keyframe{{AtMS: 0, Value: 0, Easing: mediagen.EaseLinear}, {AtMS: 1000, Value: -20, Easing: mediagen.EaseLinear}}}
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatalf("a ducked music track refused: %v", err)
	}
	cases := map[string]func(*VideoDocument){
		"ducking on a visual track":   func(d *VideoDocument) { d.Tracks[0].Ducking = d.Tracks[2].Ducking },
		"gain on a visual track":      func(d *VideoDocument) { d.Tracks[0].GainDB = ptr(-3.0) },
		"a gain past +6 dB":           func(d *VideoDocument) { d.Tracks[2].GainDB = ptr(9.0) },
		"ducking under itself":        func(d *VideoDocument) { d.Tracks[2].Ducking.TriggerTrackIDs = []string{"music"} },
		"ducking under a missing one": func(d *VideoDocument) { d.Tracks[2].Ducking.TriggerTrackIDs = []string{"nope"} },
		"no trigger":                  func(d *VideoDocument) { d.Tracks[2].Ducking.TriggerTrackIDs = nil },
		"a reduction past 40 dB":      func(d *VideoDocument) { d.Tracks[2].Ducking.ReduceDB = 50 },
		"a cycle": func(d *VideoDocument) {
			d.Tracks[3].Ducking = &DuckingRule{TriggerTrackIDs: []string{"music"}, ReduceDB: 12, AttackMS: 250, ReleaseMS: 500, HoldMS: 300, Sensitivity: 0.5}
		},
		"gain keys on a video clip": func(d *VideoDocument) {
			d.Tracks[0].Clips[1].Keyframes = &ClipKeyframes{GainDB: []mediagen.Keyframe{{AtMS: 0, Value: -6, Easing: mediagen.EaseLinear}}}
		},
		"visual keys on a song": func(d *VideoDocument) {
			d.Tracks[2].Clips[0].Keyframes = keys(mediagen.Keyframes{Opacity: []mediagen.Keyframe{{AtMS: 0, Value: 1, Easing: mediagen.EaseLinear}}})
		},
		"a gain key below silence": func(d *VideoDocument) {
			d.Tracks[2].Clips[0].Keyframes = &ClipKeyframes{GainDB: []mediagen.Keyframe{{AtMS: 0, Value: -90, Easing: mediagen.EaseLinear}}}
		},
	}
	for name, change := range cases {
		d := duckedDoc()
		change(&d)
		if codeOf(t, ValidateDocument(KindVideo, mustJSON(t, d)))[FieldTracks] == "" {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestBakedDuckingMayUseManyGainKeys(t *testing.T) {
	doc := duckedDoc()
	frames := make([]mediagen.Keyframe, 200)
	for i := range frames {
		frames[i] = mediagen.Keyframe{AtMS: int64(i * 20), Value: -12, Easing: mediagen.EaseLinear}
	}
	doc.Tracks[2].Clips[0].Keyframes = &ClipKeyframes{GainDB: frames}
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatalf("a baked lane refused: %v", err)
	}
}
