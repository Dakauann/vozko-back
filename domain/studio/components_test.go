package studio

import (
	"encoding/json"
	"os"
	"testing"

	"vozko/domain/mediagen"
)

func componentFixture(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile("testdata/editor_components.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func fixtureLayers(doc map[string]any, artboard int) []any {
	return doc["artboards"].([]any)[artboard].(map[string]any)["layers"].([]any)
}

func fixtureLayer(doc map[string]any, artboard, layer int) map[string]any {
	return fixtureLayers(doc, artboard)[layer].(map[string]any)
}

func componentCode(t *testing.T, doc map[string]any) map[string]string {
	t.Helper()
	return codeOf(t, ValidateDocument(KindImage, mustJSON(t, doc)))
}

func TestTheEditorComponentDocumentPassesTheContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/editor_components.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateDocument(KindImage, raw); err != nil {
		t.Fatalf("the editor saved components the backend refuses: %v", err)
	}
}

func TestInstancesNeedAFlaggedComponent(t *testing.T) {
	for name, edit := range map[string]func(map[string]any){
		"a missing component": func(d map[string]any) { fixtureLayer(d, 0, 1)["componentId"] = "nothing" },
		"a plain artboard":    func(d map[string]any) { fixtureLayer(d, 0, 1)["componentId"] = "post" },
		"a cleared flag":      func(d map[string]any) { delete(d["artboards"].([]any)[1].(map[string]any), "component") },
		"a nested instance": func(d map[string]any) {
			cta := d["artboards"].([]any)[1].(map[string]any)
			cta["layers"] = append(cta["layers"].([]any), map[string]any{"id": "nested", "type": "instance", "componentId": "badge", "transform": map[string]any{"x": 0.5, "y": 0.5, "w": 0.2, "h": 0.2, "rotation": 0, "opacity": 1}})
		},
	} {
		doc := componentFixture(t)
		edit(doc)
		if componentCode(t, doc)[FieldLayers] != CodeInvalid {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestInstancesCarryNoStyle(t *testing.T) {
	for name, edit := range map[string]func(map[string]any){
		"a fill on an instance":     func(d map[string]any) { fixtureLayer(d, 0, 1)["fill"] = "#ff0000" },
		"a text on an instance":     func(d map[string]any) { fixtureLayer(d, 0, 1)["text"] = "Oi" },
		"a component id on a photo": func(d map[string]any) { fixtureLayer(d, 0, 0)["componentId"] = "cta" },
		"overrides on a photo": func(d map[string]any) {
			fixtureLayer(d, 0, 0)["overrides"] = []any{map[string]any{"layerId": "pill", "fill": "#ff0000"}}
		},
		"a part id stored as a layer id": func(d map[string]any) { fixtureLayer(d, 0, 0)["id"] = "cta-1;pill" },
	} {
		doc := componentFixture(t)
		edit(doc)
		if componentCode(t, doc)[FieldLayers] != CodeInvalid {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestOverridesAreCheckedAgainstTheirMainLayer(t *testing.T) {
	cases := map[string]struct {
		entry map[string]any
		code  string
	}{
		"an unknown main layer":   {map[string]any{"layerId": "ghost", "fill": "#ff0000"}, CodeInvalid},
		"an empty override":       {map[string]any{"layerId": "pill"}, CodeInvalid},
		"text on a shape":         {map[string]any{"layerId": "pill", "text": "Oi"}, CodeInvalid},
		"a stroke without one":    {map[string]any{"layerId": "pill", "stroke": "#000000"}, CodeInvalid},
		"a broken color":          {map[string]any{"layerId": "label", "fill": "red"}, CodeInvalid},
		"an unknown font":         {map[string]any{"layerId": "label", "fontId": "comic-sans"}, CodeUnknown},
		"an odd weight":           {map[string]any{"layerId": "label", "fontWeight": 450}, CodeOutOfRange},
		"a blank text":            {map[string]any{"layerId": "label", "text": "   "}, CodeInvalid},
		"a crop past the picture": {map[string]any{"layerId": "shot", "crop": map[string]any{"x": 0.6, "y": 0, "w": 0.5, "h": 1}}, CodeOutOfRange},
	}
	for name, c := range cases {
		doc := componentFixture(t)
		target := fixtureLayer(doc, 0, 1)
		if c.entry["layerId"] == "shot" {
			target = fixtureLayer(doc, 0, 3)
		}
		target["overrides"] = []any{c.entry}
		if got := componentCode(t, doc)[FieldLayers]; got != c.code {
			t.Errorf("%s: code %q, want %q", name, got, c.code)
		}
	}
	doc := componentFixture(t)
	fixtureLayer(doc, 0, 1)["overrides"] = []any{map[string]any{"layerId": "pill", "fill": "#000000"}, map[string]any{"layerId": "pill", "hidden": true}}
	if componentCode(t, doc)[FieldLayers] != CodeDuplicate {
		t.Error("two overrides of the same layer were accepted")
	}
	doc = componentFixture(t)
	fixtureLayer(doc, 0, 1)["overrides"] = []any{map[string]any{"layerId": "label", "unknown": true}}
	if componentCode(t, doc)[FieldDocument] != CodeInvalid {
		t.Error("an unknown override field was accepted")
	}
}

func TestVideoOverlaysRefuseInstances(t *testing.T) {
	doc := VideoDocument{Schema: SchemaVideo, Version: DocumentVersion, Canvas: VideoCanvas{Aspect: "square", Background: "#000000"}, DurationMS: 2000, Tracks: []Track{{ID: "t1", Kind: TrackVisual, Clips: []Clip{{
		ID: "c1", Type: ClipOverlay, DurationMS: 1000, Volume: 1, Transform: mediagen.Transform{X: 0.5, Y: 0.5, W: 0.5, H: 0.5, Opacity: 1},
		Layer: &Layer{ID: "l1", Type: LayerInstance, ComponentID: "cta", Transform: Transform{X: 0.5, Y: 0.5, W: 1, H: 1, Opacity: 1}},
	}}}}}
	if codeOf(t, ValidateDocument(KindVideo, mustJSON(t, doc)))[FieldTracks] != CodeInvalid {
		t.Fatal("an instance inside a video overlay was accepted")
	}
}
