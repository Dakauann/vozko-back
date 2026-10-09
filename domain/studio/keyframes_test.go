package studio

import (
	"encoding/json"
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

func TestKeyframesAreKeptInTheDocument(t *testing.T) {
	doc := videoDoc()
	doc.Tracks[0].Clips[0].Keyframes = slide()
	doc.Tracks[1].Clips[0].Keyframes = &mediagen.Keyframes{Scale: []mediagen.Keyframe{{AtMS: 0, Value: 1, Easing: mediagen.EaseInOut}, {AtMS: 1000, Value: 1.4, Easing: mediagen.EaseLinear}}}
	p, err := NewProject("ws", "u-1", KindVideo, "Reels", mustJSON(t, doc))
	if err != nil {
		t.Fatal(err)
	}
	var saved VideoDocument
	if err := json.Unmarshal(p.Document, &saved); err != nil {
		t.Fatal(err)
	}
	if k := saved.Tracks[0].Clips[0].Keyframes; k == nil || len(k.X) != 2 || k.X[0].Easing != mediagen.EaseOut {
		t.Fatalf("visual keyframes %+v", k)
	}
	if k := saved.Tracks[1].Clips[0].Keyframes; k == nil || len(k.Scale) != 2 {
		t.Fatalf("overlay keyframes %+v", k)
	}
}

func TestKeyframesKeepTheirRules(t *testing.T) {
	cases := map[string]func(*VideoDocument){
		"on audio": func(d *VideoDocument) { d.Tracks[2].Clips[0].Keyframes = slide() },
		"more keys on one property than a clip holds": func(d *VideoDocument) {
			many := make([]mediagen.Keyframe, mediagen.MaxKeyframesPerProperty+1)
			for i := range many {
				many[i] = mediagen.Keyframe{AtMS: int64(i), Value: 0.5, Easing: mediagen.EaseLinear}
			}
			d.Tracks[1].Clips[0].Keyframes = &mediagen.Keyframes{X: many}
		},
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

func TestAVideoDocumentTakesAnyNumberOfTracksClipsAndKeyframes(t *testing.T) {
	doc := videoDoc()
	doc.DurationMS = mediagen.MaxVideoMS
	fillWithKeyframes(&doc)
	for i := 0; i < 300; i++ {
		extra := doc.Tracks[1].Clips[0]
		extra.ID = "x" + strconv.Itoa(i)
		extra.StartMS = 3_000 + int64(i)*200
		extra.DurationMS = 200
		extra.FadeInMS = 0
		extra.Keyframes = nil
		doc.Tracks[1].Clips = append(doc.Tracks[1].Clips, extra)
	}
	for i := 0; i < 40; i++ {
		kind := TrackVisual
		if i%4 == 0 {
			kind = TrackAudio
		}
		doc.Tracks = append(doc.Tracks, Track{ID: "t" + strconv.Itoa(i), Kind: kind})
	}
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatalf("a large timeline was refused: %v", err)
	}
}

func TestAnImageDocumentTakesAnyNumberOfLayersAndGroups(t *testing.T) {
	doc := imageDoc()
	for i := 0; i < 600; i++ {
		doc.Layers = append(doc.Layers, Layer{ID: "l" + strconv.Itoa(i), Type: LayerShape, Shape: ShapeRect, Fill: "#222222", GroupID: "g" + strconv.Itoa(i/2), Transform: box()})
	}
	for i := 0; i < 300; i++ {
		doc.Groups = append(doc.Groups, Group{ID: "g" + strconv.Itoa(i)})
	}
	if err := ValidateDocument(KindImage, mustJSON(t, doc)); err != nil {
		t.Fatalf("a layered design was refused: %v", err)
	}
}
