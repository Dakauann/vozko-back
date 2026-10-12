package studio

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func repeatDoc(repeat *Repeat) LegacyImageDocument {
	return LegacyImageDocument{
		Schema: SchemaImage, Version: DocumentVersion,
		Canvas: Canvas{Width: 1000, Height: 1000, Background: "#ffffff"},
		Layers: []Layer{
			{ID: "a", Type: LayerShape, Shape: ShapeEllipse, Fill: "#ff0000", Transform: box(), GroupID: "g"},
		},
		Groups: []Group{{ID: "g", Repeat: repeat}},
	}
}

func TestRepeatGroupsAreAccepted(t *testing.T) {
	for name, repeat := range map[string]*Repeat{
		"radial":     {Mode: RepeatRadial, Count: 12, Shown: 9, Radius: 200, StartAngle: -90, Sweep: 180, Direction: "ccw", Upright: true, ReverseOverlap: true},
		"big radial": {Mode: RepeatRadial, Count: 5000, Radius: 10},
		"grid":       {Mode: RepeatGrid, Rows: 4, Columns: 6, GapX: 12, GapY: -5, Layout: "brick-row", Offset: 0.5, FlipRows: "horizontal", FlipColumns: "both"},
		"mirror":     {Mode: RepeatMirror, Angle: 0, Gap: -20, Side: "start"},
	} {
		if err := ValidateDocument(KindImage, mustJSON(t, repeatDoc(repeat))); err != nil {
			t.Errorf("%s refused: %v", name, err)
		}
	}
}

func TestRepeatGroupsAreChecked(t *testing.T) {
	cases := map[string]*Repeat{
		"an unknown mode":                {Mode: "spiral"},
		"a radial of one":                {Mode: RepeatRadial, Count: 1, Radius: 10},
		"more shown than placed":         {Mode: RepeatRadial, Count: 4, Shown: 5, Radius: 10},
		"a radius of zero":               {Mode: RepeatRadial, Count: 4},
		"a start angle past 180":         {Mode: RepeatRadial, Count: 4, Radius: 10, StartAngle: 200},
		"a sweep past a turn":            {Mode: RepeatRadial, Count: 4, Radius: 10, Sweep: 400},
		"an unknown direction":           {Mode: RepeatRadial, Count: 4, Radius: 10, Direction: "up"},
		"grid fields on a radial":        {Mode: RepeatRadial, Count: 4, Radius: 10, Rows: 2},
		"a one cell grid":                {Mode: RepeatGrid, Rows: 1, Columns: 1},
		"a grid without rows":            {Mode: RepeatGrid, Columns: 3},
		"a brick offset on a plain grid": {Mode: RepeatGrid, Rows: 2, Columns: 2, Offset: 0.5},
		"an unknown flip":                {Mode: RepeatGrid, Rows: 2, Columns: 2, FlipRows: "diagonal"},
		"a gap past 4096":                {Mode: RepeatGrid, Rows: 2, Columns: 2, GapX: 5000},
		"radial fields on a grid":        {Mode: RepeatGrid, Rows: 2, Columns: 2, Upright: true},
		"a mirror axis past 180":         {Mode: RepeatMirror, Angle: 190},
		"an unknown side":                {Mode: RepeatMirror, Angle: 90, Side: "middle"},
		"grid fields on a mirror":        {Mode: RepeatMirror, Angle: 90, Columns: 2},
	}
	for name, repeat := range cases {
		if codeOf(t, ValidateDocument(KindImage, mustJSON(t, repeatDoc(repeat))))[FieldLayers] == "" {
			t.Errorf("%s accepted", name)
		}
	}
	raw := strings.Replace(string(mustJSON(t, repeatDoc(&Repeat{Mode: RepeatRadial, Count: 4, Radius: 10}))), `"count":4`, `"count":2.5`, 1)
	if codeOf(t, ValidateDocument(KindImage, json.RawMessage(raw)))[FieldDocument] == "" {
		t.Fatal("a fractional count was accepted")
	}
}

func TestRepeatsDoNotNestAndNeedLayers(t *testing.T) {
	nested := repeatDoc(&Repeat{Mode: RepeatMirror, Angle: 90})
	nested.Layers[0].GroupID = "inner"
	nested.Layers = append(nested.Layers, Layer{ID: "b", Type: LayerShape, Shape: ShapeRect, Fill: "#000000", Transform: box(), GroupID: "inner"})
	nested.Groups = []Group{{ID: "g", Repeat: &Repeat{Mode: RepeatGrid, Rows: 2, Columns: 2}}, {ID: "inner", ParentID: "g", Repeat: &Repeat{Mode: RepeatMirror, Angle: 90}}}
	if codeOf(t, ValidateDocument(KindImage, mustJSON(t, nested)))[FieldLayers] == "" {
		t.Fatal("a repeat inside a repeat was accepted")
	}
	nested.Groups[0].Repeat = nil
	if err := ValidateDocument(KindImage, mustJSON(t, nested)); err != nil {
		t.Fatalf("a repeat inside a plain group was refused: %v", err)
	}
}

func TestTheEditorRepeatDocumentPassesTheContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/editor_repeat.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateDocument(KindImage, raw); err != nil {
		t.Fatalf("the editor saved a repeat the backend refuses: %v", err)
	}
}
