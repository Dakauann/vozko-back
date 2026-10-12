package studio

import (
	"encoding/json"
	"os"
	"testing"

	"vozko/domain/mediagen"
)

func styleFixture(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile("testdata/editor_styles.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func fixtureStyle(doc map[string]any, index int) map[string]any {
	return doc["styles"].([]any)[index].(map[string]any)
}

func fixtureCanvas(doc map[string]any, artboard int) map[string]any {
	return doc["artboards"].([]any)[artboard].(map[string]any)["canvas"].(map[string]any)
}

func styleCodes(t *testing.T, doc map[string]any) map[string]string {
	t.Helper()
	return codeOf(t, ValidateDocument(KindImage, mustJSON(t, doc)))
}

func TestTheEditorStyleDocumentPassesTheContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/editor_styles.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateDocument(KindImage, raw); err != nil {
		t.Fatalf("the editor saved styles the backend refuses: %v", err)
	}
}

func TestStyleDefinitionsAreChecked(t *testing.T) {
	cases := map[string]struct {
		edit func(map[string]any)
		code string
	}{
		"a repeated id":             {func(d map[string]any) { fixtureStyle(d, 1)["id"] = "red" }, CodeDuplicate},
		"an unknown kind":           {func(d map[string]any) { fixtureStyle(d, 0)["kind"] = "grid" }, CodeUnknown},
		"a blank name":              {func(d map[string]any) { fixtureStyle(d, 0)["name"] = " " }, CodeInvalid},
		"a color and a gradient":    {func(d map[string]any) { fixtureStyle(d, 0)["gradient"] = fixtureStyle(d, 1)["gradient"] }, CodeInvalid},
		"a color style without one": {func(d map[string]any) { delete(fixtureStyle(d, 0), "color") }, CodeInvalid},
		"a broken color":            {func(d map[string]any) { fixtureStyle(d, 0)["color"] = "red" }, CodeInvalid},
		"a font on a color":         {func(d map[string]any) { fixtureStyle(d, 0)["fontId"] = "inter" }, CodeInvalid},
		"a role on a color":         {func(d map[string]any) { fixtureStyle(d, 0)["role"] = "heading" }, CodeInvalid},
		"a tiny font size":          {func(d map[string]any) { fixtureStyle(d, 2)["fontSizePx"] = 0.5 }, CodeOutOfRange},
		"an unknown font":           {func(d map[string]any) { fixtureStyle(d, 2)["fontId"] = "comic-sans" }, CodeUnknown},
		"an unknown role":           {func(d map[string]any) { fixtureStyle(d, 2)["role"] = "caption" }, CodeUnknown},
		"an empty text style": {func(d map[string]any) {
			d["styles"].([]any)[3] = map[string]any{"id": "empty", "kind": "text", "name": "Vazio"}
		}, CodeInvalid},
		"an effect without shadow": {func(d map[string]any) { delete(fixtureStyle(d, 4), "shadow") }, CodeInvalid},
		"a huge blur": {func(d map[string]any) {
			fixtureStyle(d, 4)["shadow"] = map[string]any{"color": "#000000", "blur": 900, "x": 0, "y": 0}
		}, CodeInvalid},
		"a long description": {func(d map[string]any) {
			fixtureStyle(d, 0)["description"] = string(make([]byte, 501))
		}, CodeInvalid},
	}
	for name, c := range cases {
		doc := styleFixture(t)
		c.edit(doc)
		if got := styleCodes(t, doc)[FieldStyles]; got != c.code {
			t.Errorf("%s: code %q, want %q", name, got, c.code)
		}
	}
}

func TestStyleLinksAreChecked(t *testing.T) {
	cases := map[string]func(map[string]any){
		"a missing style":         func(d map[string]any) { fixtureLayer(d, 0, 0)["textStyleId"] = "ghost" },
		"the wrong kind":          func(d map[string]any) { fixtureLayer(d, 0, 0)["textStyleId"] = "red" },
		"a text style on a shape": func(d map[string]any) { fixtureLayer(d, 0, 1)["textStyleId"] = "h1" },
		"a gradient stroke":       func(d map[string]any) { fixtureLayer(d, 0, 1)["strokeStyleId"] = "sunset" },
		"a gradient icon": func(d map[string]any) {
			icon := fixtureLayer(d, 0, 2)
			icon["fillStyleId"] = "sunset"
			icon["fill"] = "#ff7a00"
			icon["gradient"] = map[string]any{"from": "#ff7a00", "to": "#e4003a", "angle": 90, "kind": "linear"}
		},
		"an effect background":   func(d map[string]any) { fixtureCanvas(d, 0)["backgroundStyleId"] = "soft" },
		"a broken id":            func(d map[string]any) { fixtureLayer(d, 0, 0)["fillStyleId"] = "bad id" },
		"a different weight":     func(d map[string]any) { fixtureLayer(d, 0, 0)["fontWeight"] = 700 },
		"a feed size on a story": func(d map[string]any) { fixtureLayer(d, 1, 0)["fontSize"] = 72.0 / 1080 },
		"a different icon color": func(d map[string]any) { fixtureLayer(d, 0, 2)["fill"] = "#000000" },
		"a softer shadow": func(d map[string]any) {
			fixtureLayer(d, 0, 0)["shadow"] = map[string]any{"color": "#00000059", "blur": 12, "x": 0, "y": 8}
		},
		"a different background": func(d map[string]any) { fixtureCanvas(d, 0)["background"] = "#000000" },
	}
	for name, edit := range cases {
		doc := styleFixture(t)
		edit(doc)
		if got := styleCodes(t, doc)[FieldLayers]; got != CodeInvalid {
			t.Errorf("%s: code %q, want invalid", name, got)
		}
	}
}

func TestStylesStayOutOfLegacyDocumentsAndVideo(t *testing.T) {
	icon := Layer{ID: "l", Type: LayerIcon, IconID: "star", Fill: "#e4003a", FillStyleID: "red", Transform: box()}
	legacy := LegacyImageDocument{Schema: SchemaImage, Version: DocumentVersion, Canvas: Canvas{Width: 1080, Height: 1080, Background: "#ffffff"}, Layers: []Layer{icon}}
	if codeOf(t, ValidateDocument(KindImage, mustJSON(t, legacy)))[FieldLayers] != CodeInvalid {
		t.Fatal("a style link in a legacy document was accepted")
	}
	video := VideoDocument{Schema: SchemaVideo, Version: DocumentVersion, Canvas: VideoCanvas{Aspect: "square", Background: "#000000"}, DurationMS: 2000, Tracks: []Track{{ID: "t1", Kind: TrackVisual, Clips: []Clip{{
		ID: "c1", Type: ClipOverlay, DurationMS: 1000, Volume: 1, Transform: mediagen.Transform{X: 0.5, Y: 0.5, W: 0.5, H: 0.5, Opacity: 1}, Layer: &icon,
	}}}}}
	if codeOf(t, ValidateDocument(KindVideo, mustJSON(t, video)))[FieldTracks] != CodeInvalid {
		t.Fatal("a style link inside a video overlay was accepted")
	}
}
