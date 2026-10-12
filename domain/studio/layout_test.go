package studio

import (
	"encoding/json"
	"os"
	"testing"
)

func layoutFixture(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile("testdata/editor_layout.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func layoutPart(doc map[string]any, kind, id string) map[string]any {
	for _, item := range doc["artboards"].([]any)[0].(map[string]any)[kind].([]any) {
		if entry := item.(map[string]any); entry["id"] == id {
			return entry
		}
	}
	return nil
}

func layoutOf(doc map[string]any, id string) map[string]any {
	return layoutPart(doc, "groups", id)["layout"].(map[string]any)
}

func TestTheEditorLayoutDocumentPassesTheContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/editor_layout.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateDocument(KindImage, raw); err != nil {
		t.Fatalf("the editor saved a layout the backend refuses: %v", err)
	}
}

func TestLayoutsAreChecked(t *testing.T) {
	cases := map[string]struct {
		edit func(map[string]any)
		code string
	}{
		"an unknown direction":       {func(d map[string]any) { layoutOf(d, "cta")["direction"] = "diagonal" }, CodeUnknown},
		"a missing width":            {func(d map[string]any) { delete(layoutOf(d, "cta"), "width") }, CodeUnknown},
		"an unknown alignment":       {func(d map[string]any) { layoutOf(d, "cta")["alignMain"] = "middle" }, CodeUnknown},
		"a huge gap":                 {func(d map[string]any) { layoutOf(d, "cta")["gap"] = 20000 }, CodeOutOfRange},
		"a gap beside an auto gap":   {func(d map[string]any) { layoutOf(d, "auto")["gap"] = 10 }, CodeInvalid},
		"a main alignment with auto": {func(d map[string]any) { layoutOf(d, "auto")["alignMain"] = "center" }, CodeInvalid},
		"a layout without a base":    {func(d map[string]any) { delete(layoutPart(d, "groups", "auto"), "baseId") }, CodeInvalid},
		"a negative padding": {func(d map[string]any) {
			layoutOf(d, "cta")["padding"] = map[string]any{"top": -1, "right": 0, "bottom": 0, "left": 0}
		}, CodeOutOfRange},
		"a turned base": {func(d map[string]any) {
			layoutPart(d, "layers", "row-bg")["transform"].(map[string]any)["rotation"] = 10
		}, CodeInvalid},
		"a repeated layout group": {func(d map[string]any) {
			layoutPart(d, "groups", "auto")["repeat"] = map[string]any{"mode": "mirror", "angle": 90}
		}, CodeInvalid},
		"an empty item": {func(d map[string]any) {
			layoutPart(d, "layers", "cta-icon")["layoutItem"] = map[string]any{}
		}, CodeInvalid},
		"an unknown item sizing": {func(d map[string]any) {
			layoutPart(d, "layers", "cta-icon")["layoutItem"] = map[string]any{"width": "stretch"}
		}, CodeUnknown},
		"a hugging icon": {func(d map[string]any) {
			layoutPart(d, "layers", "cta-icon")["layoutItem"] = map[string]any{"width": "hug"}
		}, CodeInvalid},
		"an absolute fill": {func(d map[string]any) {
			layoutPart(d, "layers", "badge")["layoutItem"] = map[string]any{"absolute": true, "width": "fill"}
		}, CodeInvalid},
		"a fill inside a hug": {func(d map[string]any) {
			layoutPart(d, "layers", "cta-icon")["layoutItem"] = map[string]any{"width": "fill"}
		}, CodeInvalid},
		"an item on the base": {func(d map[string]any) {
			layoutPart(d, "layers", "card")["layoutItem"] = map[string]any{"width": "fill"}
		}, CodeInvalid},
		"a hugging group": {func(d map[string]any) {
			layoutPart(d, "groups", "cta")["layoutItem"] = map[string]any{"height": "hug"}
		}, CodeInvalid},
		"a group filling its hug": {func(d map[string]any) {
			layoutPart(d, "groups", "cta")["layoutItem"] = map[string]any{"width": "fill"}
		}, CodeInvalid},
		"an item outside a layout": {func(d map[string]any) {
			layoutPart(d, "groups", "auto")["layoutItem"] = map[string]any{"width": "fixed"}
		}, CodeInvalid},
	}
	for name, c := range cases {
		doc := layoutFixture(t)
		c.edit(doc)
		if got := codeOf(t, ValidateDocument(KindImage, mustJSON(t, doc)))[FieldLayers]; got != c.code {
			t.Errorf("%s: code %q, want %q", name, got, c.code)
		}
	}
	doc := layoutFixture(t)
	layoutPart(doc, "groups", "cta")["layoutItem"] = map[string]any{"width": "fixed"}
	if err := ValidateDocument(KindImage, mustJSON(t, doc)); err != nil {
		t.Fatalf("a fixed nested group was refused: %v", err)
	}
}
