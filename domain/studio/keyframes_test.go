package studio

import (
	"strconv"
	"testing"

	"vozko/domain/mediagen"
)

func slide() *mediagen.Keyframes {
	return &mediagen.Keyframes{
		X:       []mediagen.Keyframe{{AtMS: 0, Value: -0.2, Easing: mediagen.EaseOut}, {AtMS: 600, Value: 0.5, Easing: mediagen.EaseLinear}},
		Opacity: []mediagen.Keyframe{{AtMS: 0, Value: 0, Easing: mediagen.EaseLinear}, {AtMS: 300, Value: 1, Easing: mediagen.EaseLinear}},
	}
}

func TestKeyframesAreKeptAndReachTheRenderTimeline(t *testing.T) {
	doc := videoDoc()
	doc.Tracks[0].Clips[0].Keyframes = slide()
	doc.Tracks[1].Clips[0].Keyframes = &mediagen.Keyframes{Scale: []mediagen.Keyframe{{AtMS: 0, Value: 1, Easing: mediagen.EaseInOut}, {AtMS: 1000, Value: 1.4, Easing: mediagen.EaseLinear}}}
	p, err := NewProject("ws", "u-1", KindVideo, "Reels", mustJSON(t, doc))
	if err != nil {
		t.Fatal(err)
	}
	req, err := p.VideoRequest(map[string]string{"o1": "raster-1"})
	if err != nil {
		t.Fatal(err)
	}
	if k := req.Video.Visual[0].Clips[0].Keyframes; k == nil || len(k.X) != 2 || k.X[0].Easing != mediagen.EaseOut {
		t.Fatalf("visual keyframes %+v", k)
	}
	if k := req.Video.Visual[1].Clips[0].Keyframes; k == nil || len(k.Scale) != 2 {
		t.Fatalf("overlay keyframes %+v", k)
	}
	if err := req.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestKeyframesKeepTheirRules(t *testing.T) {
	tooMany := videoDoc()
	fillWithKeyframes(&tooMany)
	if got := codeOf(t, ValidateDocument(KindVideo, mustJSON(t, tooMany)))[FieldTracks]; got != CodeTooMany {
		t.Fatalf("a document over the keyframe cap: %q", got)
	}
	cases := map[string]func(*VideoDocument){
		"on audio": func(d *VideoDocument) { d.Tracks[2].Clips[0].Keyframes = slide() },
		"unknown easing": func(d *VideoDocument) {
			d.Tracks[1].Clips[0].Keyframes = &mediagen.Keyframes{Y: []mediagen.Keyframe{{AtMS: 0, Value: 0.5, Easing: "bounce"}}}
		},
		"out of range": func(d *VideoDocument) {
			d.Tracks[1].Clips[0].Keyframes = &mediagen.Keyframes{Opacity: []mediagen.Keyframe{{AtMS: 0, Value: 2, Easing: mediagen.EaseLinear}}}
		},
	}
	for name, change := range cases {
		doc := videoDoc()
		change(&doc)
		if codeOf(t, ValidateDocument(KindVideo, mustJSON(t, doc)))[FieldTracks] == "" {
			t.Errorf("%s accepted", name)
		}
	}
}

func fillWithKeyframes(d *VideoDocument) {
	many := make([]mediagen.Keyframe, mediagen.MaxKeyframesPerProperty)
	for i := range many {
		many[i] = mediagen.Keyframe{AtMS: int64(i), Value: 0.5, Easing: mediagen.EaseLinear}
	}
	full := &mediagen.Keyframes{X: many, Y: many, Scale: many, Rotation: many, Opacity: many}
	for i := range many {
		full.Scale[i].Value = 1
	}
	for _, track := range d.Tracks[:2] {
		for i := range track.Clips {
			track.Clips[i].Keyframes = full
		}
	}
	for i := 0; i < 2; i++ {
		extra := d.Tracks[1].Clips[0]
		extra.ID = "extra" + string(rune('a'+i))
		extra.StartMS = 2_600 + int64(i)*200
		extra.DurationMS = 200
		d.Tracks[1].Clips = append(d.Tracks[1].Clips, extra)
	}
}

func TestAVideoDocumentStaysWithinWhatTheRendererAccepts(t *testing.T) {
	visual, audio := videoDoc(), videoDoc()
	for i := 0; i < mediagen.MaxVisualTracks; i++ {
		visual.Tracks = append(visual.Tracks, Track{ID: "v" + string(rune('a'+i)), Kind: TrackVisual})
	}
	for i := 0; i < mediagen.MaxAudioTracks; i++ {
		audio.Tracks = append(audio.Tracks, Track{ID: "a" + string(rune('a'+i)), Kind: TrackAudio})
	}
	for name, doc := range map[string]VideoDocument{"visual": visual, "audio": audio} {
		if got := codeOf(t, ValidateDocument(KindVideo, mustJSON(t, doc)))[FieldTracks]; got != CodeTooMany {
			t.Errorf("%s tracks past the renderer limit: %q", name, got)
		}
	}
	clips := videoDoc()
	clips.DurationMS = mediagen.MaxVideoMS
	for i := 0; len(clips.Tracks[1].Clips)+4 <= mediagen.MaxTimelineClips; i++ {
		extra := clips.Tracks[1].Clips[0]
		extra.ID = "x" + strconv.Itoa(i)
		extra.StartMS = 3_000 + int64(i)*200
		extra.DurationMS = 100
		extra.FadeInMS = 0
		clips.Tracks[1].Clips = append(clips.Tracks[1].Clips, extra)
	}
	if got := codeOf(t, ValidateDocument(KindVideo, mustJSON(t, clips)))[FieldTracks]; got != CodeTooMany {
		t.Errorf("clips past the renderer limit: %q", got)
	}
}
