package studio

import (
	"fmt"
	"math"
	"os"
	"strings"
	"testing"

	"vozko/domain/mediagen"
)

func strokeOp(patch func(*PixelOp)) PixelOp {
	op := PixelOp{Kind: PixelStroke, ID: "s1", Mode: PixelPaint, Color: "#e4572e", Size: 24, Hardness: 0.8, Opacity: 1, Points: []float64{10, 10, 40, 30}}
	if patch != nil {
		patch(&op)
	}
	return op
}

func areaOp(patch func(*PixelOp)) PixelOp {
	op := PixelOp{Kind: PixelArea, ID: "a1", Mode: PixelErase, Opacity: 1, Parts: []AreaPart{{Points: []float64{0, 0, 50, 0, 50, 50}}}}
	if patch != nil {
		patch(&op)
	}
	return op
}

func pixels(ops ...PixelOp) PixelSurface {
	return PixelSurface{Width: 100, Height: 100, Ops: ops}
}

func paintLayer(surface PixelSurface) Layer {
	return Layer{ID: "ink", Type: LayerPaint, Transform: Transform{X: 0.5, Y: 0.5, W: 1, H: 1, Opacity: 1}, Surface: &surface}
}

func photoLayer() Layer {
	return Layer{ID: "photo", Type: LayerImage, AssetID: "asset", Transform: box()}
}

func textLayer() Layer {
	return Layer{ID: "title", Type: LayerText, Text: "oi", FontID: "inter", FontSize: 0.05, Fill: "#111111", Transform: box()}
}

func TestPixelSurfacesAcceptStrokesAndAreasWithTheirOwnFields(t *testing.T) {
	paint := pixels(
		strokeOp(nil),
		strokeOp(func(o *PixelOp) {
			o.ID, o.Mode, o.Color, o.Pressure, o.Points = "s2", PixelErase, "", true, []float64{1, 2, 0.5, 3, 4, 1}
		}),
		areaOp(nil),
	)
	if code := paint.issue(SurfacePaint); code != "" {
		t.Fatalf("paint surface refused: %s", code)
	}
	mask := pixels(
		strokeOp(func(o *PixelOp) { o.Mode, o.Color = PixelHide, "" }),
		areaOp(func(o *PixelOp) {
			o.Mode, o.Invert, o.Feather = PixelReveal, true, 250
			o.Parts = []AreaPart{{Points: []float64{0, 0, 9, 0, 9, 9}}, {Mode: PixelSubtract, Points: []float64{1, 1, 2, 1, 2, 2}}}
		}),
	)
	if code := mask.issue(SurfaceMask); code != "" {
		t.Fatalf("mask surface refused: %s", code)
	}
}

func TestPixelSurfacesAcceptAnyNumberOfOperations(t *testing.T) {
	ops := make([]PixelOp, 3000)
	for i := range ops {
		ops[i] = strokeOp(func(o *PixelOp) { o.ID, o.Points = fmt.Sprintf("s%d", i), []float64{float64(i % 100), 5} })
	}
	if code := pixels(ops...).issue(SurfacePaint); code != "" {
		t.Fatalf("3000 operations refused: %s", code)
	}
}

func TestPixelSurfacesRefuseWhatTheEditorRefuses(t *testing.T) {
	cases := []struct {
		name    string
		surface PixelSurface
		kind    SurfaceKind
		code    string
	}{
		{"a surface wider than 4096", PixelSurface{Width: 4097, Height: 100}, SurfacePaint, CodeOutOfRange},
		{"an empty surface side", PixelSurface{Width: 0, Height: 10}, SurfacePaint, CodeOutOfRange},
		{"an unknown kind", pixels(strokeOp(func(o *PixelOp) { o.Kind = "smudge" })), SurfacePaint, CodeUnknown},
		{"a mask mode on paint", pixels(strokeOp(func(o *PixelOp) { o.Mode, o.Color = PixelHide, "" })), SurfacePaint, CodeUnknown},
		{"a paint mode on a mask", pixels(strokeOp(nil)), SurfaceMask, CodeUnknown},
		{"paint without a color", pixels(strokeOp(func(o *PixelOp) { o.Color = "" })), SurfacePaint, CodeInvalid},
		{"a color on an erase", pixels(strokeOp(func(o *PixelOp) { o.Mode = PixelErase })), SurfacePaint, CodeInvalid},
		{"a color with alpha", pixels(strokeOp(func(o *PixelOp) { o.Color = "#e4572e80" })), SurfacePaint, CodeInvalid},
		{"a brush of zero", pixels(strokeOp(func(o *PixelOp) { o.Size = 0 })), SurfacePaint, CodeOutOfRange},
		{"a brush past 1000", pixels(strokeOp(func(o *PixelOp) { o.Size = 1001 })), SurfacePaint, CodeOutOfRange},
		{"a hardness past 1", pixels(strokeOp(func(o *PixelOp) { o.Hardness = 1.5 })), SurfacePaint, CodeOutOfRange},
		{"a zero opacity", pixels(strokeOp(func(o *PixelOp) { o.Opacity = 0 })), SurfacePaint, CodeOutOfRange},
		{"an odd point list", pixels(strokeOp(func(o *PixelOp) { o.Points = []float64{1, 2, 3} })), SurfacePaint, CodeInvalid},
		{"a pressure list off stride", pixels(strokeOp(func(o *PixelOp) { o.Pressure, o.Points = true, []float64{1, 2, 0.5, 3} })), SurfacePaint, CodeInvalid},
		{"pressure past 1", pixels(strokeOp(func(o *PixelOp) { o.Pressure, o.Points = true, []float64{1, 2, 1.5} })), SurfacePaint, CodeOutOfRange},
		{"no points", pixels(strokeOp(func(o *PixelOp) { o.Points = nil })), SurfacePaint, CodeInvalid},
		{"a point far outside", pixels(strokeOp(func(o *PixelOp) { o.Points = []float64{1, 9000} })), SurfacePaint, CodeOutOfRange},
		{"a non finite point", pixels(strokeOp(func(o *PixelOp) { o.Points = []float64{1, math.NaN()} })), SurfacePaint, CodeOutOfRange},
		{"area fields on a stroke", pixels(strokeOp(func(o *PixelOp) { o.Feather = 4 })), SurfacePaint, CodeInvalid},
		{"stroke fields on an area", pixels(areaOp(func(o *PixelOp) { o.Size = 4 })), SurfacePaint, CodeInvalid},
		{"an area without parts", pixels(areaOp(func(o *PixelOp) { o.Parts = nil })), SurfacePaint, CodeInvalid},
		{"an area starting with a subtraction", pixels(areaOp(func(o *PixelOp) {
			o.Parts = []AreaPart{{Mode: PixelSubtract, Points: []float64{0, 0, 5, 0, 5, 5}}}
		})), SurfacePaint, CodeInvalid},
		{"a part with two points", pixels(areaOp(func(o *PixelOp) { o.Parts = []AreaPart{{Points: []float64{0, 0, 5, 0}}} })), SurfacePaint, CodeInvalid},
		{"an unknown part mode", pixels(areaOp(func(o *PixelOp) {
			o.Parts = []AreaPart{{Points: []float64{0, 0, 5, 0, 5, 5}}, {Mode: "add", Points: []float64{0, 0, 5, 0, 5, 5}}}
		})), SurfacePaint, CodeUnknown},
		{"a feather past 250", pixels(areaOp(func(o *PixelOp) { o.Feather = 251 })), SurfacePaint, CodeOutOfRange},
		{"a bad op id", pixels(strokeOp(func(o *PixelOp) { o.ID = "Bad Id" })), SurfacePaint, CodeInvalid},
		{"a repeated op id", pixels(strokeOp(nil), strokeOp(nil)), SurfacePaint, CodeDuplicate},
	}
	for _, c := range cases {
		if code := c.surface.issue(c.kind); code != c.code {
			t.Errorf("%s: got %q, want %q", c.name, code, c.code)
		}
	}
}

func TestPaintLayersCarryOnlyTheirOwnFields(t *testing.T) {
	layer := paintLayer(pixels(strokeOp(nil)))
	if code := layer.issue(); code != "" {
		t.Fatalf("paint layer refused: %s", code)
	}
	allowed := layer
	allowed.BlendMode, allowed.FlipX, allowed.Clip, allowed.Name, allowed.Hidden, allowed.Locked = "multiply", true, true, "Tinta", true, true
	if code := allowed.issue(); code != "" {
		t.Fatalf("paint layer with its own fields refused: %s", code)
	}
	for name, patch := range map[string]func(*Layer){
		"a fill":     func(l *Layer) { l.Fill = "#ffffff" },
		"an asset":   func(l *Layer) { l.AssetID = "x" },
		"effects":    func(l *Layer) { l.Effects = []Effect{{ID: "e", Type: EffectDropShadow, Color: "#000000", Opacity: 1}} },
		"a mask":     func(l *Layer) { mask := pixels(); l.Mask = &mask },
		"a star tip": func(l *Layer) { l.Points = 5 },
	} {
		wrong := paintLayer(pixels(strokeOp(nil)))
		patch(&wrong)
		if code := wrong.issue(); code != CodeInvalid {
			t.Errorf("a paint layer with %s: got %q", name, code)
		}
	}
	empty := layer
	empty.Effects = []Effect{}
	if code := empty.issue(); code != "" {
		t.Fatalf("an empty list counted as set: %s", code)
	}
	missing := layer
	missing.Surface = nil
	if code := missing.issue(); code != CodeRequired {
		t.Fatalf("a paint layer without a surface: got %q", code)
	}
	broken := paintLayer(pixels(strokeOp(func(o *PixelOp) { o.Size = 0 })))
	if code := broken.issue(); code != CodeOutOfRange {
		t.Fatalf("a paint layer with a bad stroke: got %q", code)
	}
}

func TestMasksOnlyOnPhotosAndSurfacesOnlyOnPaintLayers(t *testing.T) {
	mask := pixels(strokeOp(func(o *PixelOp) { o.Mode, o.Color = PixelHide, "" }))
	photo := photoLayer()
	photo.Mask = &mask
	if code := photo.issue(); code != "" {
		t.Fatalf("a masked photo refused: %s", code)
	}
	text := textLayer()
	text.Mask = &mask
	if code := text.issue(); code != CodeInvalid {
		t.Fatalf("a mask on text: got %q", code)
	}
	painted := pixels(strokeOp(nil))
	photo.Mask = &painted
	if code := photo.issue(); code == "" {
		t.Fatal("a paint mode in a mask was accepted")
	}
	surface := pixels()
	text = textLayer()
	text.Surface = &surface
	if code := text.issue(); code != CodeInvalid {
		t.Fatalf("a surface on text: got %q", code)
	}
}

func TestVideoOverlaysRefusePixels(t *testing.T) {
	full := mediagen.FullFrame()
	empty := pixels()
	masked := photoLayer()
	masked.Mask = &empty
	for name, layer := range map[string]Layer{"a paint layer": paintLayer(pixels()), "a masked photo": masked} {
		doc := videoDoc()
		overlay := layer
		doc.Tracks[1].Clips = []Clip{{ID: "o1", Type: ClipOverlay, StartMS: 500, DurationMS: 2_000, Layer: &overlay, Transform: full}}
		if codeOf(t, ValidateDocument(KindVideo, mustJSON(t, doc)))[FieldTracks] != CodeInvalid {
			t.Errorf("%s on a video overlay was accepted", name)
		}
	}
}

func TestPaintedImageDocumentsValidateEndToEnd(t *testing.T) {
	doc := imageDoc()
	doc.Layers = append(doc.Layers, paintLayer(PixelSurface{Width: 1000, Height: 1000, Ops: []PixelOp{strokeOp(nil)}}))
	if err := ValidateDocument(KindImage, mustJSON(t, doc)); err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string]string{
		"an unknown surface field": strings.Replace(string(mustJSON(t, doc)), `"ops":[`, `"extra":1,"ops":[`, 1),
		"a fractional side":        strings.Replace(string(mustJSON(t, doc)), `"surface":{"width":1000`, `"surface":{"width":10.5`, 1),
	} {
		if codeOf(t, ValidateDocument(KindImage, []byte(raw)))[FieldDocument] != CodeInvalid {
			t.Errorf("%s was accepted", name)
		}
	}
	doc.Layers[len(doc.Layers)-1].Surface.Ops[0].Size = 0
	if codeOf(t, ValidateDocument(KindImage, mustJSON(t, doc)))[FieldLayers] != CodeOutOfRange {
		t.Fatal("a zero brush was accepted")
	}
}

func TestTheEditorPixelDocumentPassesTheContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/editor_pixels.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateDocument(KindImage, raw); err != nil {
		t.Fatal(err)
	}
}
