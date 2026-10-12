package studio

import (
	"testing"

	"vozko/domain/mediagen"
)

func ellipseMask(id string) Mask {
	return Mask{ID: id, Shape: "ellipse", Mode: "add", X: 0.5, Y: 0.5, W: 0.5, H: 0.5, Opacity: 1}
}

func TestMasksOnVisualClips(t *testing.T) {
	doc := videoDoc()
	star := Mask{ID: "s", Shape: "star", Mode: "subtract", X: 0.5, Y: 0.5, W: 0.4, H: 0.4, Points: 6, Inner: 0.4, Feather: 0.05, Opacity: 1, Invert: true}
	star.Keyframes = &MaskKeyframes{Opacity: []mediagen.Keyframe{{AtMS: 0, Value: 0, Easing: mediagen.EaseLinear}, {AtMS: 500, Value: 1, Easing: mediagen.EaseLinear}}}
	doc.Tracks[0].Clips[1].Masks = []Mask{ellipseMask("e"), star, {ID: "p", Shape: "path", Mode: "intersect", X: 0.5, Y: 0.5, W: 1, H: 1, Path: "M0 0 L1 0 L0.5 1 Z", Opacity: 1}}
	doc.Tracks[1].Clips[0].Matte = true
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatalf("masks refused: %v", err)
	}
	cases := map[string]func(*VideoDocument){
		"a mask on a song":      func(d *VideoDocument) { d.Tracks[2].Clips[0].Masks = []Mask{ellipseMask("e")} },
		"a matte on a song":     func(d *VideoDocument) { d.Tracks[2].Clips[0].Matte = true },
		"two masks with one id": func(d *VideoDocument) { d.Tracks[0].Clips[1].Masks = []Mask{ellipseMask("e"), ellipseMask("e")} },
		"an unknown shape": func(d *VideoDocument) {
			m := ellipseMask("e")
			m.Shape = "blob"
			d.Tracks[0].Clips[1].Masks = []Mask{m}
		},
		"a feather past the edge": func(d *VideoDocument) { m := ellipseMask("e"); m.Feather = 0.9; d.Tracks[0].Clips[1].Masks = []Mask{m} },
		"points on an ellipse":    func(d *VideoDocument) { m := ellipseMask("e"); m.Points = 6; d.Tracks[0].Clips[1].Masks = []Mask{m} },
		"a path without a path": func(d *VideoDocument) {
			m := ellipseMask("e")
			m.Shape = "path"
			d.Tracks[0].Clips[1].Masks = []Mask{m}
		},
		"a key out of range": func(d *VideoDocument) {
			m := ellipseMask("e")
			m.Keyframes = &MaskKeyframes{Opacity: []mediagen.Keyframe{{AtMS: 0, Value: 3, Easing: mediagen.EaseLinear}}}
			d.Tracks[0].Clips[1].Masks = []Mask{m}
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
