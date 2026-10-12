package studio

import (
	"encoding/json"
	"os"
	"testing"

	"vozko/domain/mediagen"
)

func layerStyleFixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/editor_layer_styles.json")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func layerStyleDoc(t *testing.T) VideoDocument {
	t.Helper()
	var doc VideoDocument
	if err := json.Unmarshal(layerStyleFixture(t), &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestTheEditorLayerStyleDocumentPassesTheContract(t *testing.T) {
	if err := ValidateDocument(KindVideo, layerStyleFixture(t)); err != nil {
		t.Fatalf("the editor saved layer styles the backend refuses: %v", err)
	}
}

func TestLayerStylesAreRefusedWhereTheEditorRefusesThem(t *testing.T) {
	video := func(d *VideoDocument) *Clip { return &d.Tracks[0].Clips[0] }
	title := func(d *VideoDocument) *Layer { return d.Tracks[1].Clips[0].Layer }
	cases := map[string]func(*VideoDocument){
		"an inner glow on a video":     func(d *VideoDocument) { video(d).Effects[1].Type = EffectInnerGlow },
		"a centered stroke on a video": func(d *VideoDocument) { video(d).Strokes[0].Align = StrokeCenter },
		"a gradient stroke on a video": func(d *VideoDocument) {
			s := &video(d).Strokes[0]
			s.Type = PaintGradient
			s.Color = ""
			s.Gradient = &Gradient{From: "#000000", To: "#ffffff"}
		},
		"a dashed stroke on a video":       func(d *VideoDocument) { video(d).Strokes[0].DashArray = []float64{2, 1} },
		"two effects with one id":          func(d *VideoDocument) { video(d).Effects[1].ID = "fx-glow" },
		"an effect key past its range":     func(d *VideoDocument) { video(d).Effects[0].Keyframes.Opacity[1].Value = 1.5 },
		"effect keys out of order":         func(d *VideoDocument) { video(d).Effects[0].Keyframes.Blur[1].AtMS = 0 },
		"an effect key with a bad curve":   func(d *VideoDocument) { video(d).Effects[0].Keyframes.Opacity[0].Easing = "wobbly" },
		"styles on a text clip":            func(d *VideoDocument) { d.Tracks[1].Clips[0].Effects = video(d).Effects },
		"an overlay effect key off range":  func(d *VideoDocument) { title(d).Effects[0].Keyframes.Spread[1].Value = 500 },
		"an overlay effect id missing":     func(d *VideoDocument) { title(d).Effects[0].ID = "" },
		"a shadow next to an effect list":  func(d *VideoDocument) { title(d).Shadow = &Shadow{Color: "#000000", Blur: 4} },
		"a fill next to a fill list":       func(d *VideoDocument) { title(d).Fill = "#ffffff" },
		"an outline next to a stroke list": func(d *VideoDocument) { title(d).StrokeWidth = 2; title(d).Stroke = "#000000" },
	}
	for name, change := range cases {
		doc := layerStyleDoc(t)
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

func TestEffectKeysStayOutOfImageDocuments(t *testing.T) {
	layer := Layer{ID: "l", Type: LayerShape, Shape: ShapeRect, Transform: box(), Fill: "#ffffff", Effects: []Effect{{
		ID: "fx", Type: EffectOuterGlow, Color: "#ffffff", Opacity: 1, Blur: 8,
		Keyframes: &EffectKeyframes{Opacity: []mediagen.Keyframe{{AtMS: 0, Value: 1, Easing: mediagen.EaseLinear}}},
	}}}
	if layer.issue() == "" {
		t.Fatal("an image layer accepted effect keys, which only make sense in time")
	}
	if layer.withoutEffectKeys().issue() != "" {
		t.Fatal("the same layer without keys was refused")
	}
}

func TestMediaEffectTypesMatchTheEditor(t *testing.T) {
	types := MediaEffectTypes()
	if len(types) != 2 || types[0] != EffectDropShadow || types[1] != EffectOuterGlow {
		t.Fatalf("media effect types drifted: %v", types)
	}
}
