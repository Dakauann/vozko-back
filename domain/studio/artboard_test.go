package studio

import (
	"os"
	"strings"
	"testing"
)

func artboard(id string, x float64, layers ...Layer) Artboard {
	return Artboard{ID: id, X: x, Canvas: Canvas{Width: 1080, Height: 1080, Background: "#ffffff"}, Layers: layers}
}

func project(artboards ...Artboard) ImageDocument {
	return ImageDocument{Schema: SchemaImage, Version: ImageDocumentVersion, Artboards: artboards}
}

func TestAnImageProjectHoldsSeveralArtboards(t *testing.T) {
	legacy := imageDoc()
	story := artboard("story", 1180)
	story.Name = "Story"
	story.Canvas.Height = 1920
	doc := project(Artboard{ID: "feed", Canvas: legacy.Canvas, Layers: legacy.Layers}, story)
	if err := ValidateDocument(KindImage, mustJSON(t, doc)); err != nil {
		t.Fatal(err)
	}
}

func TestASingleCanvasProjectStaysValid(t *testing.T) {
	if err := ValidateDocument(KindImage, mustJSON(t, imageDoc())); err != nil {
		t.Fatal(err)
	}
}

func TestArtboardsFollowTheirRules(t *testing.T) {
	shared := Layer{ID: "logo", Type: LayerShape, Shape: ShapeRect, Fill: "#000000", Transform: box()}
	cases := map[string]struct {
		doc   ImageDocument
		field string
		code  string
	}{
		"no artboard":            {project(), FieldArtboards, CodeRequired},
		"bad artboard id":        {project(artboard("Feed!", 0)), FieldArtboards, CodeInvalid},
		"same artboard twice":    {project(artboard("feed", 0), artboard("feed", 1200)), FieldArtboards, CodeDuplicate},
		"artboard far away":      {project(artboard("feed", MaxArtboardCoordinate+1)), FieldArtboards, CodeOutOfRange},
		"layer on two artboards": {project(artboard("feed", 0, shared), artboard("story", 1200, shared)), FieldLayers, CodeDuplicate},
		"tiny artboard": {func() ImageDocument {
			small := artboard("feed", 0)
			small.Canvas.Width = 40
			return project(small)
		}(), FieldCanvas, CodeOutOfRange},
		"long name": {func() ImageDocument {
			named := artboard("feed", 0)
			named.Name = strings.Repeat("a", maxTokenRunes*2+1)
			return project(named)
		}(), FieldArtboards, CodeInvalid},
	}
	for name, c := range cases {
		if got := codeOf(t, ValidateDocument(KindImage, mustJSON(t, c.doc)))[c.field]; got != c.code {
			t.Errorf("%s: %s = %q, want %q", name, c.field, got, c.code)
		}
	}
	twinGroups := project(artboard("feed", 0), artboard("story", 1200))
	twinGroups.Artboards[0].Groups = []Group{{ID: "g"}}
	twinGroups.Artboards[1].Groups = []Group{{ID: "g"}}
	if codeOf(t, ValidateDocument(KindImage, mustJSON(t, twinGroups)))[FieldLayers] != CodeDuplicate {
		t.Fatal("a group id must be unique across the project")
	}
}

func TestAnArtboardDocumentRefusesTheOldCanvasFields(t *testing.T) {
	raw := strings.Replace(string(mustJSON(t, project(artboard("feed", 0)))), `"artboards"`, `"canvas":{"width":1080,"height":1080,"background":""},"artboards"`, 1)
	if codeOf(t, ValidateDocument(KindImage, []byte(raw)))[FieldDocument] != CodeInvalid {
		t.Fatal("unknown fields on a version 2 document must be refused")
	}
}

func TestADocumentSavedByTheEditorPassesTheContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/editor_artboards.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateDocument(KindImage, raw); err != nil {
		t.Fatalf("the editor saved a document the backend refuses: %v", err)
	}
	var doc ImageDocument
	if err := decodeStrict(raw, &doc); err != nil || len(doc.Artboards) != 3 {
		t.Fatalf("artboards = %d, err = %v", len(doc.Artboards), err)
	}
}
