package studio

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"vozko/domain/mediagen"
)

func readFixture(t *testing.T, name string, into any) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	if into != nil {
		if err := json.Unmarshal(raw, into); err != nil {
			t.Fatal(err)
		}
	}
	return raw
}

func pathOpsVideo(t *testing.T) VideoDocument {
	var doc VideoDocument
	readFixture(t, "editor_path_ops.json", &doc)
	return doc
}

func badgeOf(d *VideoDocument) *Layer {
	return d.Tracks[0].Clips[0].Layer
}

func opKeys(values ...float64) []mediagen.Keyframe {
	frames := make([]mediagen.Keyframe, len(values))
	for i, v := range values {
		frames[i] = mediagen.Keyframe{AtMS: int64(i) * 500, Value: v, Easing: mediagen.EaseLinear}
	}
	return frames
}

func TestTheEditorPathOpsDocumentsPassTheContract(t *testing.T) {
	if err := ValidateDocument(KindVideo, readFixture(t, "editor_path_ops.json", nil)); err != nil {
		t.Fatalf("the editor saved video path operators the backend refuses: %v", err)
	}
	if err := ValidateDocument(KindImage, readFixture(t, "editor_path_ops_image.json", nil)); err != nil {
		t.Fatalf("the editor saved image path operators the backend refuses: %v", err)
	}
}

func TestPathOpsAreChecked(t *testing.T) {
	cases := map[string]func(*VideoDocument){
		"a field of another kind":      func(d *VideoDocument) { badgeOf(d).PathOps[1].Amount = 3 },
		"a text field of another kind": func(d *VideoDocument) { badgeOf(d).PathOps[1].Join = JoinRound },
		"an amount out of range":       func(d *VideoDocument) { badgeOf(d).PathOps[2].Amount = 5000 },
		"no copies":                    func(d *VideoDocument) { badgeOf(d).PathOps[2].Copies = 0 },
		"an offset without a join":     func(d *VideoDocument) { badgeOf(d).PathOps[2].Join = "" },
		"negative ridges":              func(d *VideoDocument) { badgeOf(d).PathOps[1].Ridges = -1 },
		"an unknown kind":              func(d *VideoDocument) { badgeOf(d).PathOps[3].Kind = "spiral" },
		"unknown points":               func(d *VideoDocument) { badgeOf(d).PathOps[1].Points = "wavy" },
		"a merge with a broken path":   func(d *VideoDocument) { badgeOf(d).PathOps[5].Path = "Z" },
		"a merge without a path":       func(d *VideoDocument) { badgeOf(d).PathOps[5].Path = "" },
		"an unknown merge mode":        func(d *VideoDocument) { badgeOf(d).PathOps[5].Mode = "flatten" },
		"an unknown fill rule":         func(d *VideoDocument) { badgeOf(d).PathOps[5].FillRule = "odd" },
		"a merge size of zero":         func(d *VideoDocument) { badgeOf(d).PathOps[5].H = 0 },
		"a bad id":                     func(d *VideoDocument) { badgeOf(d).PathOps[0].ID = "Bad Id" },
		"a repeated id":                func(d *VideoDocument) { badgeOf(d).PathOps[1].ID = "op-round" },
		"a twist center far away":      func(d *VideoDocument) { badgeOf(d).PathOps[3].CenterX = 3 },
		"a key on a field not owned":   func(d *VideoDocument) { badgeOf(d).PathOps[2].Keyframes.Radius = opKeys(3) },
		"a key out of range":           func(d *VideoDocument) { badgeOf(d).PathOps[2].Keyframes.Amount = opKeys(0, 3000) },
		"keys out of order":            func(d *VideoDocument) { badgeOf(d).PathOps[2].Keyframes.Amount[1].AtMS = 0 },
		"operators on a line": func(d *VideoDocument) {
			layer := badgeOf(d)
			layer.Shape, layer.Points, layer.Inner = ShapeLine, 0, 0
		},
		"operators on a text": func(d *VideoDocument) {
			d.Tracks[0].Clips[0].Layer = &Layer{ID: "t", Type: LayerText, Text: "Oi", FontID: "inter", FontSize: 0.05, Transform: box(), PathOps: []PathOp{{ID: "r", Kind: PathOpRoundCorners, Radius: 4}}}
		},
	}
	for name, change := range cases {
		d := pathOpsVideo(t)
		change(&d)
		if codeOf(t, ValidateDocument(KindVideo, mustJSON(t, d)))[FieldTracks] == "" {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestPathOpsHaveAFloorButNoCeilingOnCounts(t *testing.T) {
	d := pathOpsVideo(t)
	badgeOf(&d).PathOps[2].Copies = 400
	badgeOf(&d).PathOps[1].Ridges = 5000
	badgeOf(&d).PathOps[2].Keyframes.Copies = opKeys(1, 900)
	if err := ValidateDocument(KindVideo, mustJSON(t, d)); err != nil {
		t.Fatalf("many copies and ridges were refused: %v", err)
	}
}

func TestKeyedPathOpsTakeAnyShapeThatTakesOperators(t *testing.T) {
	d := pathOpsVideo(t)
	badgeOf(&d).Shadow = &Shadow{Color: "#000000", Blur: 4}
	if err := ValidateDocument(KindVideo, mustJSON(t, d)); err != nil {
		t.Fatalf("keyed operators on a shadowed shape were refused: %v", err)
	}
}

func TestPathOpsAreNotKeyedInImages(t *testing.T) {
	var doc ImageDocument
	readFixture(t, "editor_path_ops_image.json", &doc)
	doc.Artboards[0].Layers[0].PathOps[1].Keyframes = &PathOpKeyframes{Amount: opKeys(-10)}
	if codeOf(t, ValidateDocument(KindImage, mustJSON(t, doc)))[FieldLayers] != CodeInvalid {
		t.Fatal("a keyed operator was accepted in an image")
	}
}

func TestStaticPathOpsTakeEveryKindOnAPlainShape(t *testing.T) {
	doc := imageDoc()
	doc.Layers = append(doc.Layers, Layer{ID: "sq", Type: LayerShape, Shape: ShapeRect, Fill: "#ff0000", Transform: box(), PathOps: []PathOp{
		{ID: "a", Kind: PathOpOffset, Amount: 12, Copies: 1, CopyOffset: 12, Join: JoinRound, MiterLimit: 4},
		{ID: "b", Kind: PathOpPuckerBloat, Amount: 30},
		{ID: "c", Kind: PathOpRoundCorners, Radius: 24},
		{ID: "d", Kind: PathOpZigZag, Size: 10, Ridges: 6, Points: ZigZagCorner},
		{ID: "e", Kind: PathOpTwist, Angle: 90, CenterX: 0.5, CenterY: 0.5},
		{ID: "f", Kind: PathOpMerge, Mode: MergeSubtract, Path: "M0 0 L1 0 L1 1 L0 1 Z", X: 0.5, Y: 0.5, W: 0.5, H: 0.5},
	}})
	if err := ValidateDocument(KindImage, mustJSON(t, doc)); err != nil {
		t.Fatalf("static operators were refused: %v", err)
	}
}

func TestPathOpFieldsAreStrict(t *testing.T) {
	raw := strings.Replace(string(readFixture(t, "editor_path_ops.json", nil)), `"kind": "roundCorners",`, `"kind": "roundCorners", "speed": 2,`, 1)
	if codeOf(t, ValidateDocument(KindVideo, []byte(raw)))[FieldDocument] == "" {
		t.Fatal("an unknown path operator field was accepted")
	}
	whole := strings.Replace(string(readFixture(t, "editor_path_ops.json", nil)), `"ridges": 3`, `"ridges": 2.5`, 1)
	if codeOf(t, ValidateDocument(KindVideo, []byte(whole)))[FieldDocument] == "" {
		t.Fatal("fractional ridges were accepted")
	}
}
