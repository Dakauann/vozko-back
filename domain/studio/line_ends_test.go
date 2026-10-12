package studio

import (
	"encoding/json"
	"testing"
)

func TestTheLineEndsDocumentPassesTheContract(t *testing.T) {
	if err := ValidateDocument(KindImage, readFixture(t, "editor_line_ends.json", nil)); err != nil {
		t.Fatalf("the editor saved line ends and polygons the backend refuses: %v", err)
	}
}

func TestLineEndKindsAreStrict(t *testing.T) {
	line := Layer{ID: "l", Type: LayerShape, Shape: ShapeLine, Transform: Transform{X: 0.5, Y: 0.5, W: 0.5, H: 0.05, Opacity: 1}, Stroke: "#111111", StrokeWidth: 8}
	cases := map[string]struct {
		edit func(*Layer)
		want string
	}{
		"a known kind on a head":  {func(l *Layer) { l.ArrowEnd, l.ArrowEndKind = true, LineEndDiamond }, ""},
		"an unknown kind":         {func(l *Layer) { l.ArrowStart, l.ArrowStartKind = true, "star" }, CodeUnknown},
		"a kind without its head": {func(l *Layer) { l.ArrowEndKind = LineEndDot }, CodeInvalid},
		"a start kind on the end": {func(l *Layer) { l.ArrowEnd, l.ArrowStartKind = true, LineEndBar }, CodeInvalid},
	}
	for name, tc := range cases {
		layer := line
		tc.edit(&layer)
		if got := layer.lineEndIssue(); got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
}

func TestPolygonsTakeSidesButNoInnerRadius(t *testing.T) {
	polygon := Layer{ID: "p", Type: LayerShape, Shape: ShapePolygon, Transform: Transform{X: 0.5, Y: 0.5, W: 0.5, H: 0.5, Opacity: 1}, Fill: "#111111", Points: 8}
	if got := polygon.shapeIssue(); got != "" {
		t.Fatalf("an octagon = %q", got)
	}
	polygon.Inner = 0.5
	if got := polygon.shapeIssue(); got != CodeInvalid {
		t.Fatalf("a polygon with an inner radius = %q", got)
	}
	polygon.Inner, polygon.Points = 0, 2
	if got := polygon.shapeIssue(); got != CodeOutOfRange {
		t.Fatalf("a two sided polygon = %q", got)
	}
	raw, _ := json.Marshal(Layer{ArrowStart: true, ArrowStartKind: LineEndRound})
	var back map[string]any
	_ = json.Unmarshal(raw, &back)
	if back["arrowStartKind"] != LineEndRound {
		t.Fatalf("the kind travels as arrowStartKind: %s", raw)
	}
}
