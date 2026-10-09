package studio

import (
	"strconv"
	"testing"
)

func TestPathDataFollowsTheSVGGrammar(t *testing.T) {
	valid := []string{
		"M0 0.58 L1 0.40 L1 1 L0 1 Z",
		"m0,0 l.5-.5 h.25 v.25 c.1 .1 .2 .2 .3 .3 s.1 .1 .2 .2 q.1 .1 .2 .2 t.1 .1 z",
		"M0 1 L0 0.5 A0.5 0.5 0 0 1 1 0.5 L1 1 Z",
		"M0 0 L1 0 1 1 0 1Z",
		"M0 0 L1e-1 2.5E-1",
		"M0.5.5 L1 1",
		"M0 0 A0.5 0.5 0 1 0 1 1",
		"M0 0 L" + strconv.Itoa(MaxPathCoordinate) + " 0",
	}
	for _, data := range valid {
		if !ValidPath(data) {
			t.Errorf("refused %q", data)
		}
	}
	invalid := []string{
		"", "   ", "L0 0", "Z", "M0", "M0 0 L1", "M0 0 C0 0 1 1", "M0 0 X1 1", "M0 0 L1 1 Z 1", "M0 0 L1 one", "M0 0 L1 1;", "M0 0 L1 1",
		"M0 0 A0.5 0.5 0 2 0 1 1", "M0 0 A0.5 0.5 0 0.5 0 1 1",
		"M0 0 L" + strconv.Itoa(MaxPathCoordinate+1) + " 0", "M0 0 L1e400 0",
	}
	for _, data := range invalid {
		if ValidPath(data) {
			t.Errorf("accepted %q", data)
		}
	}
}

func TestAPathCutsOutItsOverlapsWithTheEvenOddRule(t *testing.T) {
	doc := imageDoc()
	doc.Layers = append(doc.Layers,
		Layer{ID: "ring", Type: LayerShape, Shape: ShapePath, Path: "M0 0 L1 0 L1 1 L0 1 Z M0.3 0.3 L0.7 0.3 L0.7 0.7 L0.3 0.7 Z", FillRule: FillEvenOdd, Transform: box()},
		Layer{ID: "solid", Type: LayerShape, Shape: ShapePath, Path: "M0 0 L1 1 L0 1 Z", FillRule: FillNonZero, Transform: box()},
	)
	if err := ValidateDocument(KindImage, mustJSON(t, doc)); err != nil {
		t.Fatalf("refused: %v", err)
	}
	cases := map[string]struct {
		layer Layer
		code  string
	}{
		"fill rule on a rectangle": {Layer{Shape: ShapeRect, FillRule: FillEvenOdd}, CodeInvalid},
		"unknown fill rule":        {Layer{Shape: ShapePath, Path: "M0 0 L1 1", FillRule: "winding"}, CodeUnknown},
	}
	for name, c := range cases {
		doc := imageDoc()
		c.layer.ID, c.layer.Type, c.layer.Transform = "x", LayerShape, box()
		doc.Layers = append(doc.Layers, c.layer)
		if got := codeOf(t, ValidateDocument(KindImage, mustJSON(t, doc)))[FieldLayers]; got != c.code {
			t.Errorf("%s: got %q want %q", name, got, c.code)
		}
	}
}

func TestVectorPathsAndTunedStarsAreShapes(t *testing.T) {
	doc := imageDoc()
	doc.Layers = append(doc.Layers,
		Layer{ID: "band", Type: LayerShape, Shape: ShapePath, Path: "M0 0.58 L1 0.40 L1 1 L0 1 Z", Fill: "#e10600", Transform: box()},
		Layer{ID: "burst", Type: LayerShape, Shape: ShapeStar, Points: 16, Inner: 0.78, Fill: "#ffcc00", Transform: box()},
	)
	if err := ValidateDocument(KindImage, mustJSON(t, doc)); err != nil {
		t.Fatalf("refused: %v", err)
	}
	cases := map[string]struct {
		layer Layer
		code  string
	}{
		"path without data":   {Layer{Shape: ShapePath}, CodeInvalid},
		"broken path":         {Layer{Shape: ShapePath, Path: "L0 0 Z"}, CodeInvalid},
		"path on a rectangle": {Layer{Shape: ShapeRect, Path: "M0 0 L1 1"}, CodeInvalid},
		"points on a rect":    {Layer{Shape: ShapeRect, Points: 6}, CodeInvalid},
		"too few points":      {Layer{Shape: ShapeStar, Points: 2}, CodeOutOfRange},
		"too many points":     {Layer{Shape: ShapeStar, Points: 65}, CodeOutOfRange},
		"inner past the tip":  {Layer{Shape: ShapeStar, Inner: 1.2}, CodeOutOfRange},
	}
	for name, c := range cases {
		doc := imageDoc()
		c.layer.ID, c.layer.Type, c.layer.Transform = "x", LayerShape, box()
		doc.Layers = append(doc.Layers, c.layer)
		if got := codeOf(t, ValidateDocument(KindImage, mustJSON(t, doc)))[FieldLayers]; got != c.code {
			t.Errorf("%s: got %q want %q", name, got, c.code)
		}
	}
}

func TestStrokesTakeCapsJoinsMiterAndDashPatterns(t *testing.T) {
	doc := imageDoc()
	doc.Layers = append(doc.Layers,
		Layer{ID: "s", Type: LayerShape, Shape: ShapePath, Path: "M0 0 L1 1", Stroke: "#000000", StrokeWidth: 4, LineCap: CapSquare, LineJoin: JoinMiter, MiterLimit: 8, DashArray: []float64{4, 2, 0, 2}, DashOffset: 1.5, Transform: box()},
		Layer{ID: "t", Type: LayerText, Text: "Oi", FontID: "inter", FontSize: 0.1, Fill: "#000000", Stroke: "#ffffff", StrokeWidth: 3, LineJoin: JoinRound, DashArray: []float64{2}, Transform: box()},
	)
	if err := ValidateDocument(KindImage, mustJSON(t, doc)); err != nil {
		t.Fatalf("refused: %v", err)
	}
	cases := map[string]struct {
		layer Layer
		code  string
	}{
		"unknown cap":        {Layer{Shape: ShapeRect, LineCap: "flat"}, CodeUnknown},
		"unknown join":       {Layer{Shape: ShapeRect, LineJoin: "pointy"}, CodeUnknown},
		"miter too small":    {Layer{Shape: ShapeRect, MiterLimit: 0.5}, CodeOutOfRange},
		"miter too big":      {Layer{Shape: ShapeRect, MiterLimit: 30}, CodeOutOfRange},
		"all zero dashes":    {Layer{Shape: ShapeRect, DashArray: []float64{0, 0}}, CodeInvalid},
		"too many dashes":    {Layer{Shape: ShapeRect, DashArray: []float64{1, 2, 3, 4, 5, 6, 7, 8, 9}}, CodeInvalid},
		"negative dash":      {Layer{Shape: ShapeRect, DashArray: []float64{-1, 2}}, CodeInvalid},
		"offset out of band": {Layer{Shape: ShapeRect, DashOffset: 5000}, CodeOutOfRange},
	}
	for name, c := range cases {
		doc := imageDoc()
		c.layer.ID, c.layer.Type, c.layer.Fill, c.layer.Transform = "x", LayerShape, "#000000", box()
		doc.Layers = append(doc.Layers, c.layer)
		if got := codeOf(t, ValidateDocument(KindImage, mustJSON(t, doc)))[FieldLayers]; got != c.code {
			t.Errorf("%s: got %q want %q", name, got, c.code)
		}
	}
	icon := imageDoc()
	icon.Layers = append(icon.Layers, Layer{ID: "i", Type: LayerIcon, IconID: "star", Fill: "#000000", LineCap: CapButt, Transform: box()})
	if got := codeOf(t, ValidateDocument(KindImage, mustJSON(t, icon)))[FieldLayers]; got != CodeInvalid {
		t.Errorf("stroke style on an icon: got %q", got)
	}
}
