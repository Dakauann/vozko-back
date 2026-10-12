package studio

import (
	"encoding/json"
	"os"
	"testing"
)

func solidPaint(color string) Paint { return Paint{Type: PaintSolid, Color: color, Opacity: 1} }

func outsideStroke(color string, width float64) StrokePaint {
	return StrokePaint{Paint: solidPaint(color), Width: width, Align: StrokeOutside}
}

func dotsPattern() *Pattern {
	return &Pattern{Source: PatternPreset, PresetID: "dots", Color: "#111111", Tile: "grid", Size: 32, Opacity: 1}
}

func paintedDoc() LegacyImageDocument {
	glow := Effect{ID: "glow", Type: EffectOuterGlow, Color: "#ffffff", Opacity: 0.75, Blur: 16}
	inner := Effect{ID: "inner", Type: EffectInnerGlow, Color: "#ffffff", Opacity: 0.6, Blur: 10, BlendMode: "screen", Source: "center"}
	drop := Effect{ID: "drop", Type: EffectDropShadow, Color: "#000000", Opacity: 0.4, Y: 6, Blur: 12, ShowBehind: true}
	carve := Effect{ID: "carve", Type: EffectInnerShadow, Color: "#000000", Opacity: 0.35, Y: 2, Blur: 6}
	brick := &Pattern{Source: PatternPreset, PresetID: "dots", Color: "#111111", Tile: "brick-row", Size: 32, Opacity: 1, Offset: 0.5, Spacing: &PatternPoint{X: 4, Y: 4}, Origin: &PatternPoint{X: 0.25}, Angle: 15, BlendMode: "multiply"}
	dashed := outsideStroke("#e4003a", 6)
	dashed.DashArray = []float64{2, 1}
	dashed.LineJoin = JoinRound
	return LegacyImageDocument{
		Schema: SchemaImage, Version: DocumentVersion,
		Canvas: Canvas{Width: 1080, Height: 1080, Background: "#ffffff"},
		Layers: []Layer{
			{ID: "card", Type: LayerShape, Shape: ShapeRect, Fill: "#3b82f6", Transform: box(), Pattern: brick},
			{
				ID: "photo", Type: LayerImage, AssetID: "m-1", Transform: box(),
				GradientMap: &GradientMap{Stops: []GradientStop{{At: 0, Color: "#1b1464"}, {At: 1, Color: "#ffd166ff"}}, Intensity: 0.8, Interpolation: "srgb"},
				Strokes:     []StrokePaint{outsideStroke("#ffffff", 12)},
				Effects:     []Effect{{ID: "fx-1", Type: EffectDropShadow, Color: "#000000", Opacity: 0.4, Y: 6, Blur: 12}},
			},
			{
				ID: "title", Type: LayerText, Text: "Oferta", FontID: "inter", FontSize: 0.05, Transform: box(),
				Fills: []Paint{
					solidPaint("#ffffff"),
					{Type: PaintGradient, Gradient: &Gradient{From: "#ffffff00", To: "#ffd16680", Angle: 90}, Opacity: 0.6, BlendMode: "overlay"},
					{Type: PaintPattern, Pattern: &Pattern{Source: PatternAsset, AssetID: "m-2", Size: 64, Opacity: 1}, Opacity: 1},
				},
				Strokes: []StrokePaint{dashed, {Paint: Paint{Type: PaintGradient, Gradient: &Gradient{From: "#000000", To: "#333333"}, Opacity: 1}, Width: 14, Align: StrokeCenter}},
				Effects: []Effect{glow, inner, drop, carve},
			},
			{ID: "star", Type: LayerIcon, IconID: "star", Transform: box(), Fills: []Paint{{Type: PaintPattern, Pattern: dotsPattern(), Opacity: 1}}, Strokes: []StrokePaint{outsideStroke("#000000", 3)}},
		},
	}
}

func layerByID(d *LegacyImageDocument, id string) *Layer {
	for i := range d.Layers {
		if d.Layers[i].ID == id {
			return &d.Layers[i]
		}
	}
	return nil
}

func TestPaintedLayersAreAccepted(t *testing.T) {
	if err := ValidateDocument(KindImage, mustJSON(t, paintedDoc())); err != nil {
		t.Fatal(err)
	}
}

func TestPatternRules(t *testing.T) {
	asset := Pattern{Source: PatternAsset, AssetID: "m-1", Size: 120, Opacity: 0.8, Tile: "brick-row", Offset: 0.5}
	if code := asset.issue(); code != "" {
		t.Fatalf("an uploaded tile refused: %s", code)
	}
	cases := map[string]func(p *Pattern){
		"an unknown source":                  func(p *Pattern) { p.Source = "surface" },
		"an unknown preset":                  func(p *Pattern) { p.PresetID = "plaid" },
		"a preset with an asset":             func(p *Pattern) { p.AssetID = "m-1" },
		"a preset without ink":               func(p *Pattern) { p.Color = "" },
		"an unknown tile":                    func(p *Pattern) { p.Tile = "spiral" },
		"a tile under 2 px":                  func(p *Pattern) { p.Size = 1 },
		"a tile past 4096 px":                func(p *Pattern) { p.Size = 5000 },
		"tiles that overlap past 90 percent": func(p *Pattern) { p.Size = 100; p.Spacing = &PatternPoint{X: -95} },
		"a gap past 2048 px":                 func(p *Pattern) { p.Spacing = &PatternPoint{Y: 3000} },
		"a brick offset on a grid":           func(p *Pattern) { p.Offset = 0.5 },
		"a brick offset past one tile":       func(p *Pattern) { p.Tile = "brick-row"; p.Offset = 1.5 },
		"an angle past 180":                  func(p *Pattern) { p.Angle = 200 },
		"an origin past one tile":            func(p *Pattern) { p.Origin = &PatternPoint{X: 1.2} },
		"an opacity past 1":                  func(p *Pattern) { p.Opacity = 2 },
		"an unknown blend":                   func(p *Pattern) { p.BlendMode = "dissolve" },
		"a bad background":                   func(p *Pattern) { p.Background = "white" },
	}
	for name, change := range cases {
		p := dotsPattern()
		change(p)
		if p.issue() == "" {
			t.Errorf("%s accepted", name)
		}
	}
	for name, p := range map[string]Pattern{
		"an asset with a preset":     {Source: PatternAsset, AssetID: "m-1", PresetID: "dots", Size: 32, Opacity: 1},
		"an asset with ink":          {Source: PatternAsset, AssetID: "m-1", Color: "#ff0000", Size: 32, Opacity: 1},
		"an asset without its media": {Source: PatternAsset, AssetID: " ", Size: 32, Opacity: 1},
	} {
		if p.issue() == "" {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestGradientMapRules(t *testing.T) {
	cases := map[string]GradientMap{
		"one stop":              {Stops: []GradientStop{{At: 0, Color: "#000000"}}, Intensity: 1},
		"stops out of order":    {Stops: []GradientStop{{At: 0.6, Color: "#000000"}, {At: 0.4, Color: "#ffffff"}}, Intensity: 1},
		"a stop past 1":         {Stops: []GradientStop{{At: 0, Color: "#000000"}, {At: 1.2, Color: "#ffffff"}}, Intensity: 1},
		"a named color":         {Stops: []GradientStop{{At: 0, Color: "black"}, {At: 1, Color: "#ffffff"}}, Intensity: 1},
		"intensity past 1":      {Stops: []GradientStop{{At: 0, Color: "#000000"}, {At: 1, Color: "#ffffff"}}, Intensity: 1.5},
		"unknown interpolation": {Stops: []GradientStop{{At: 0, Color: "#000000"}, {At: 1, Color: "#ffffff"}}, Intensity: 1, Interpolation: "lab"},
	}
	for name, m := range cases {
		if m.issue() == "" {
			t.Errorf("%s accepted", name)
		}
	}
	hard := GradientMap{Stops: []GradientStop{{At: 0, Color: "#000000"}, {At: 0.5, Color: "#ff0000"}, {At: 0.5, Color: "#00ff00"}, {At: 1, Color: "#ffffff"}}, Intensity: 1, Reverse: true}
	if code := hard.issue(); code != "" {
		t.Fatalf("a hard edge refused: %s", code)
	}
}

func TestPaintListsAndEffectsAreChecked(t *testing.T) {
	gradient := &Gradient{From: "#000000", To: "#ffffff"}
	cases := map[string]func(d *LegacyImageDocument){
		"a pattern on an image": func(d *LegacyImageDocument) { layerByID(d, "photo").Pattern = dotsPattern() },
		"a pattern on a line": func(d *LegacyImageDocument) {
			d.Layers = append(d.Layers, Layer{ID: "line", Type: LayerShape, Shape: ShapeLine, Stroke: "#000000", StrokeWidth: 8, Transform: box(), Pattern: dotsPattern()})
		},
		"a pattern with a gradient":     func(d *LegacyImageDocument) { layerByID(d, "card").Gradient = gradient },
		"a pattern with fills":          func(d *LegacyImageDocument) { layerByID(d, "title").Pattern = dotsPattern() },
		"a broken pattern":              func(d *LegacyImageDocument) { layerByID(d, "card").Pattern.Size = 0 },
		"a gradient map on a shape":     func(d *LegacyImageDocument) { layerByID(d, "card").GradientMap = layerByID(d, "photo").GradientMap },
		"fills on an image":             func(d *LegacyImageDocument) { layerByID(d, "photo").Fills = []Paint{solidPaint("#000000")} },
		"fills with a legacy fill":      func(d *LegacyImageDocument) { layerByID(d, "title").Fill = "#000000" },
		"a solid paint without a color": func(d *LegacyImageDocument) { layerByID(d, "title").Fills[0].Color = "" },
		"a solid paint with a gradient": func(d *LegacyImageDocument) { layerByID(d, "title").Fills[0].Gradient = gradient },
		"a gradient paint with a color": func(d *LegacyImageDocument) { layerByID(d, "title").Fills[1].Color = "#000000" },
		"an unknown paint":              func(d *LegacyImageDocument) { layerByID(d, "title").Fills[0].Type = "noise" },
		"a paint opacity past 1":        func(d *LegacyImageDocument) { layerByID(d, "title").Fills[0].Opacity = 1.5 },
		"strokes with a legacy outline": func(d *LegacyImageDocument) { layerByID(d, "title").StrokeWidth = 2 },
		"strokes with a legacy dash":    func(d *LegacyImageDocument) { layerByID(d, "title").Dash = true },
		"strokes on an arrow": func(d *LegacyImageDocument) {
			d.Layers = append(d.Layers, Layer{ID: "arrow", Type: LayerShape, Shape: ShapeArrow, ArrowEnd: true, Transform: box(), Strokes: []StrokePaint{outsideStroke("#000000", 2)}})
		},
		"a stroke without an alignment":      func(d *LegacyImageDocument) { layerByID(d, "title").Strokes[0].Align = "" },
		"a stroke past 200 px":               func(d *LegacyImageDocument) { layerByID(d, "title").Strokes[0].Width = 300 },
		"a stroke with a bad dash":           func(d *LegacyImageDocument) { layerByID(d, "title").Strokes[0].DashArray = []float64{0, 0} },
		"an inside outline around a picture": func(d *LegacyImageDocument) { layerByID(d, "photo").Strokes[0].Align = StrokeInside },
		"a gradient outline around a picture": func(d *LegacyImageDocument) {
			layerByID(d, "photo").Strokes = []StrokePaint{{Paint: Paint{Type: PaintGradient, Gradient: gradient, Opacity: 1}, Width: 4, Align: StrokeOutside}}
		},
		"a dashed outline around an icon":    func(d *LegacyImageDocument) { layerByID(d, "star").Strokes[0].DashArray = []float64{2, 2} },
		"a blended outline around a picture": func(d *LegacyImageDocument) { layerByID(d, "photo").Strokes[0].BlendMode = "multiply" },
		"effects with a legacy shadow":       func(d *LegacyImageDocument) { layerByID(d, "title").Shadow = &Shadow{Color: "#000000", Blur: 2} },
		"a repeated effect id":               func(d *LegacyImageDocument) { layerByID(d, "title").Effects[1].ID = "glow" },
		"an effect id that is not a token":   func(d *LegacyImageDocument) { layerByID(d, "title").Effects[1].ID = "Glow Two" },
		"an unknown effect":                  func(d *LegacyImageDocument) { layerByID(d, "title").Effects[0].Type = "bevel" },
		"a blend on an outer effect":         func(d *LegacyImageDocument) { layerByID(d, "title").Effects[0].BlendMode = "screen" },
		"show behind on a glow":              func(d *LegacyImageDocument) { layerByID(d, "title").Effects[0].ShowBehind = true },
		"a source on a shadow":               func(d *LegacyImageDocument) { layerByID(d, "title").Effects[3].Source = "edge" },
		"an unknown glow source":             func(d *LegacyImageDocument) { layerByID(d, "title").Effects[1].Source = "corner" },
		"an effect blur past 200":            func(d *LegacyImageDocument) { layerByID(d, "title").Effects[0].Blur = 201 },
		"an effect spread past 100":          func(d *LegacyImageDocument) { layerByID(d, "title").Effects[0].Spread = -101 },
		"an effect offset past 500":          func(d *LegacyImageDocument) { layerByID(d, "title").Effects[2].X = 501 },
		"an effect without a color":          func(d *LegacyImageDocument) { layerByID(d, "title").Effects[2].Color = "" },
	}
	for name, change := range cases {
		d := paintedDoc()
		change(&d)
		if codeOf(t, ValidateDocument(KindImage, mustJSON(t, d)))[FieldLayers] == "" {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestUnknownFieldsInsideThePaintObjectsAreRefused(t *testing.T) {
	var raw map[string]any
	if err := json.Unmarshal(mustJSON(t, paintedDoc()), &raw); err != nil {
		t.Fatal(err)
	}
	raw["layers"].([]any)[0].(map[string]any)["pattern"].(map[string]any)["scale"] = 2
	if codeOf(t, ValidateDocument(KindImage, mustJSON(t, raw)))[FieldDocument] == "" {
		t.Fatal("an unknown pattern field was accepted")
	}
}

func TestPaintListsTurnOffColorKeys(t *testing.T) {
	listed := Layer{ID: "t", Type: LayerText, Fills: []Paint{solidPaint("#ffffff")}, Strokes: []StrokePaint{outsideStroke("#000000", 2)}}
	if listed.paintsFill() || listed.paintsStroke() {
		t.Fatal("a layer painted by lists must not take fill or stroke color keys")
	}
}

func TestTheEditorPaintDocumentPassesTheContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/editor_paint.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateDocument(KindImage, raw); err != nil {
		t.Fatalf("the editor saved a painted document the backend refuses: %v", err)
	}
}
