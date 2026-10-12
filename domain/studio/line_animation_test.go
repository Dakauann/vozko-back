package studio

import (
	"os"
	"strings"
	"testing"

	"vozko/domain/mediagen"
)

func lineDoc() VideoDocument {
	doc := videoDoc()
	stroke := Layer{ID: "rule", Type: LayerShape, Shape: ShapeLine, Stroke: "#ffffff", StrokeWidth: 8, Transform: box()}
	doc.Tracks[1].Clips = append(doc.Tracks[1].Clips, Clip{
		ID: "o2", Type: ClipOverlay, StartMS: 3_000, DurationMS: 2_000, Layer: &stroke,
		Transform: mediagen.Transform{X: 0.5, Y: 0.6, W: 0.8, H: 0.05, Opacity: 1},
		Line: &LineAnimation{
			Trim: &LineTrim{End: 1},
			Wave: &LineWave{Mode: WaveBend, Amount: 12, Wavelength: 120, Speed: 1},
		},
		Keyframes: &ClipKeyframes{LineKeyframes: LineKeyframes{TrimEnd: []mediagen.Keyframe{{AtMS: 0, Value: 0, Easing: mediagen.EaseOut}, {AtMS: 800, Value: 1, Easing: mediagen.EaseLinear}}}},
	})
	return doc
}

func lineClipOf(d *VideoDocument) *Clip {
	return &d.Tracks[1].Clips[1]
}

func TestLineAnimationOnStrokedOverlays(t *testing.T) {
	if err := ValidateDocument(KindVideo, mustJSON(t, lineDoc())); err != nil {
		t.Fatalf("a line animation was refused: %v", err)
	}
	cases := map[string]func(*VideoDocument){
		"an empty line":                  func(d *VideoDocument) { lineClipOf(d).Line = &LineAnimation{} },
		"a line on a text":               func(d *VideoDocument) { d.Tracks[1].Clips[0].Line = &LineAnimation{Trim: &LineTrim{End: 1}} },
		"a line on an image":             func(d *VideoDocument) { d.Tracks[0].Clips[0].Line = &LineAnimation{Trim: &LineTrim{End: 1}} },
		"a line on a song":               func(d *VideoDocument) { d.Tracks[2].Clips[0].Line = &LineAnimation{Trim: &LineTrim{End: 1}} },
		"a line with a shadow":           func(d *VideoDocument) { lineClipOf(d).Layer.Shadow = &Shadow{Color: "#000000", Blur: 4} },
		"a trim past the end":            func(d *VideoDocument) { lineClipOf(d).Line.Trim.End = 1.5 },
		"a wavelength too short":         func(d *VideoDocument) { lineClipOf(d).Line.Wave.Wavelength = 2 },
		"an unknown wave mode":           func(d *VideoDocument) { lineClipOf(d).Line.Wave.Mode = "zigzag" },
		"a seed out of range":            func(d *VideoDocument) { lineClipOf(d).Line.Wiggle = &LineWiggle{Size: 4, Detail: 2, Rate: 1, Seed: -1} },
		"a taper ease past one":          func(d *VideoDocument) { lineClipOf(d).Line.Taper = &LineTaper{StartEase: 2} },
		"a wave travel of a whole cycle": func(d *VideoDocument) { lineClipOf(d).Line.Wave.Travel = 1 },
		"a negative wave travel":         func(d *VideoDocument) { lineClipOf(d).Line.Wave.Travel = -0.25 },
		"a negative wiggle travel": func(d *VideoDocument) {
			lineClipOf(d).Line.Wiggle = &LineWiggle{Size: 4, Detail: 2, Rate: 1, Travel: -1}
		},
		"a wiggle travel past the longest film": func(d *VideoDocument) {
			lineClipOf(d).Line.Wiggle = &LineWiggle{Size: 4, Detail: 2, Rate: 1, Travel: 30*float64(MaxVideoMS)/1000 + 1}
		},
		"a key for a group that off": func(d *VideoDocument) {
			lineClipOf(d).Keyframes.WiggleSize = []mediagen.Keyframe{{Value: 4, Easing: mediagen.EaseLinear}}
		},
		"a key out of range": func(d *VideoDocument) { lineClipOf(d).Keyframes.TrimEnd[1].Value = 2 },
		"keys without a line": func(d *VideoDocument) {
			lineClipOf(d).Line = nil
		},
	}
	for name, change := range cases {
		d := lineDoc()
		change(&d)
		if codeOf(t, ValidateDocument(KindVideo, mustJSON(t, d)))[FieldTracks] == "" {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestLineFieldsAreStrict(t *testing.T) {
	raw := strings.Replace(string(mustJSON(t, lineDoc())), `"trim":{`, `"trim":{"speed":2,`, 1)
	if codeOf(t, ValidateDocument(KindVideo, []byte(raw)))[FieldDocument] == "" {
		t.Fatal("an unknown trim field was accepted")
	}
}

func TestWhichLayersTakeLineAnimation(t *testing.T) {
	line := Layer{Type: LayerShape, Shape: ShapeLine, StrokeWidth: 4, Fill: "#ffffff"}
	outlined := Layer{Type: LayerShape, Shape: ShapeEllipse, Fill: "#ffffff", Stroke: "#000000", StrokeWidth: 2}
	filled := Layer{Type: LayerShape, Shape: ShapeEllipse, Fill: "#ffffff"}
	listed := Layer{Type: LayerShape, Shape: ShapeRect, Strokes: []StrokePaint{outsideStroke("#000000", 2)}}
	if !line.takesLine() || !outlined.takesLine() || filled.takesLine() || listed.takesLine() {
		t.Fatal("the line rule drifted from the editor")
	}
}

func TestATravelCarriedFromASplitIsAccepted(t *testing.T) {
	d := lineDoc()
	lineClipOf(&d).Line.Wave.Travel = 0.75
	lineClipOf(&d).Line.Wiggle = &LineWiggle{Size: 4, Detail: 2, Rate: 1, Travel: 812.5}
	if err := ValidateDocument(KindVideo, mustJSON(t, d)); err != nil {
		t.Fatalf("a carried travel was refused: %v", err)
	}
}

func TestTheEditorLineDocumentPassesTheContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/editor_line.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateDocument(KindVideo, raw); err != nil {
		t.Fatalf("the editor saved a line animation the backend refuses: %v", err)
	}
}
