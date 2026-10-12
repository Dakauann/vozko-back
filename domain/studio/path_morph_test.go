package studio

import (
	"encoding/json"
	"os"
	"testing"

	"vozko/domain/mediagen"
)

func morphFixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/editor_morph.json")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func morphDoc(t *testing.T) VideoDocument {
	t.Helper()
	var doc VideoDocument
	if err := json.Unmarshal(morphFixture(t), &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func morphClipOf(d *VideoDocument) *Clip {
	return &d.Tracks[1].Clips[0]
}

func morphMaskOf(d *VideoDocument) *Mask {
	return &d.Tracks[0].Clips[0].Masks[0]
}

func TestTheEditorMorphDocumentPassesTheContract(t *testing.T) {
	if err := ValidateDocument(KindVideo, morphFixture(t)); err != nil {
		t.Fatalf("the editor saved path keys the backend refuses: %v", err)
	}
}

func TestPathKeysAreRefusedWhereTheEditorRefusesThem(t *testing.T) {
	cases := map[string]func(*VideoDocument){
		"keys on a rectangle": func(d *VideoDocument) { morphClipOf(d).Layer.Shape = ShapeRect; morphClipOf(d).Layer.Path = "" },
		"keys on an image clip": func(d *VideoDocument) {
			d.Tracks[0].Clips[0].Keyframes = &ClipKeyframes{MorphKeyframes: morphClipOf(d).Keyframes.MorphKeyframes}
		},
		"a key that is not a path":      func(d *VideoDocument) { morphClipOf(d).Keyframes.Path[1].Path = "M0 0 Q1" },
		"a key with an unknown curve":   func(d *VideoDocument) { morphClipOf(d).Keyframes.Path[0].Easing = "wobbly" },
		"keys out of order":             func(d *VideoDocument) { morphClipOf(d).Keyframes.Path[2].AtMS = 500 },
		"a key past the longest film":   func(d *VideoDocument) { morphClipOf(d).Keyframes.Path[2].AtMS = MaxVideoMS + 1 },
		"a first vertex far away":       func(d *VideoDocument) { morphClipOf(d).Keyframes.Path[0].FirstVertex = &VertexPin{X: 1001, Y: 0} },
		"mask path keys on an ellipse":  func(d *VideoDocument) { m := morphMaskOf(d); m.Shape = "ellipse"; m.Path = "" },
		"a mask key that is not a path": func(d *VideoDocument) { morphMaskOf(d).Keyframes.Path[0].Path = "L0 0" },
		"mask keys out of order":        func(d *VideoDocument) { morphMaskOf(d).Keyframes.Path[1].AtMS = 0 },
	}
	for name, change := range cases {
		doc := morphDoc(t)
		change(&doc)
		raw, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateDocument(KindVideo, raw); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestPathKeysNeverReachASoundClip(t *testing.T) {
	keys := &ClipKeyframes{MorphKeyframes: MorphKeyframes{Path: []MorphKeyframe{{AtMS: 0, Path: "M0 0 L1 1", Easing: mediagen.EaseLinear}}}}
	if keys.soundOnly() {
		t.Fatal("path keys counted as sound keys")
	}
}
