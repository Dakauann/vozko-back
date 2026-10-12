package studio

import (
	"os"
	"testing"

	"vozko/domain/mediagen"
)

func greenKey() *ChromaKey {
	return &ChromaKey{Color: "#00ff00", Similarity: 0.4, Smoothness: 0.08, Spill: 0.1}
}

func TestChromaKeyOnVideoAndImageClips(t *testing.T) {
	doc := videoDoc()
	keyed := greenKey()
	keyed.Keyframes = &ChromaKeyframes{Similarity: []mediagen.Keyframe{{AtMS: 0, Value: 0.2, Easing: mediagen.EaseLinear}, {AtMS: 1000, Value: 0.5, Easing: mediagen.EaseLinear}}}
	doc.Tracks[0].Clips[0].ChromaKey = greenKey()
	doc.Tracks[0].Clips[1].ChromaKey = keyed
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatalf("chroma key refused: %v", err)
	}
	cases := map[string]func(*VideoDocument){
		"a key on a text":       func(d *VideoDocument) { d.Tracks[1].Clips[0].ChromaKey = greenKey() },
		"a key on a song":       func(d *VideoDocument) { d.Tracks[2].Clips[0].ChromaKey = greenKey() },
		"a named color":         func(d *VideoDocument) { k := greenKey(); k.Color = "green"; d.Tracks[0].Clips[0].ChromaKey = k },
		"an uppercase color":    func(d *VideoDocument) { k := greenKey(); k.Color = "#00FF00"; d.Tracks[0].Clips[0].ChromaKey = k },
		"a zero smoothness":     func(d *VideoDocument) { k := greenKey(); k.Smoothness = 0; d.Tracks[0].Clips[0].ChromaKey = k },
		"a spill past the edge": func(d *VideoDocument) { k := greenKey(); k.Spill = 2; d.Tracks[0].Clips[0].ChromaKey = k },
		"a key out of range": func(d *VideoDocument) {
			k := greenKey()
			k.Keyframes = &ChromaKeyframes{Spill: []mediagen.Keyframe{{AtMS: 0, Value: 3, Easing: mediagen.EaseLinear}}}
			d.Tracks[0].Clips[0].ChromaKey = k
		},
	}
	for name, change := range cases {
		d := videoDoc()
		change(&d)
		if codeOf(t, ValidateDocument(KindVideo, mustJSON(t, d)))[FieldTracks] == "" {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestChromaKeyOnImageLayers(t *testing.T) {
	photo := Layer{ID: "p", Type: LayerImage, AssetID: "img", Transform: box(), ChromaKey: greenKey()}
	if code := photo.issue(); code != "" {
		t.Fatalf("a keyed image layer refused: %s", code)
	}
	cases := map[string]Layer{
		"a keyed text": {ID: "t", Type: LayerText, Text: "Oi", FontID: "inter", FontSize: 0.05, Fill: "#000000", Transform: box(), ChromaKey: greenKey()},
		"an animated key on a still layer": func() Layer {
			l := photo
			l.ChromaKey = greenKey()
			l.ChromaKey.Keyframes = &ChromaKeyframes{Similarity: []mediagen.Keyframe{{AtMS: 0, Value: 0.3, Easing: mediagen.EaseLinear}}}
			return l
		}(),
		"a spill past one": func() Layer {
			l := photo
			l.ChromaKey = greenKey()
			l.ChromaKey.Spill = 2
			return l
		}(),
		"a named color": func() Layer {
			l := photo
			l.ChromaKey = greenKey()
			l.ChromaKey.Color = "green"
			return l
		}(),
	}
	for name, layer := range cases {
		if layer.issue() == "" {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestTheEditorKeyedImageLayerPassesTheContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/editor_layer_chroma.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateDocument(KindImage, raw); err != nil {
		t.Fatalf("the editor saved a keyed image layer the backend refuses: %v", err)
	}
}
