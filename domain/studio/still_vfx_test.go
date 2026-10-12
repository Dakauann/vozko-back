package studio

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"testing"

	"vozko/domain/mediagen"
)

type stillVfxFixture struct {
	Stills map[string][]string `json:"stills"`
	Valid  json.RawMessage     `json:"valid"`
}

func readStillVfxFixture(t *testing.T) stillVfxFixture {
	t.Helper()
	raw, err := os.ReadFile("testdata/editor_still_vfx.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture stillVfxFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func TestTheStillEffectsMatchTheEditor(t *testing.T) {
	fixture := readStillVfxFixture(t)
	kinds := StillVfxKinds()
	if len(kinds) != len(fixture.Stills) {
		t.Fatalf("the editor lists %d still effects, the backend %d", len(fixture.Stills), len(kinds))
	}
	for _, kind := range kinds {
		want := append([]string(nil), fixture.Stills[kind]...)
		got := StillVfxParams(kind)
		sort.Strings(want)
		sort.Strings(got)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: backend %v, editor %v", kind, got, want)
		}
	}
}

func TestTheEditorStillEffectDocumentPassesTheContract(t *testing.T) {
	if err := ValidateDocument(KindImage, readStillVfxFixture(t).Valid); err != nil {
		t.Fatalf("the editor saved still effects the backend refuses: %v", err)
	}
}

func stillPhoto(vfx ...Vfx) Layer {
	return Layer{ID: "photo", Type: LayerImage, AssetID: "media-photo", Transform: box(), Vfx: vfx}
}

func TestStillEffectsFollowTheirRules(t *testing.T) {
	keyedAmount := map[string][]mediagen.Keyframe{"amount": {{AtMS: 0, Value: 0.2, Easing: mediagen.EaseLinear}}}
	cases := map[string]struct {
		doc   ImageDocument
		field string
		code  string
	}{
		"a moving effect on a photo": {project(artboard("feed", 0, stillPhoto(Vfx{ID: "p", Kind: "particles"}))), FieldLayers, CodeUnknown},
		"a pace on a photo":          {project(artboard("feed", 0, stillPhoto(Vfx{ID: "g", Kind: "grain", Params: map[string]*float64{"rate": float(12)}}))), FieldLayers, CodeInvalid},
		"a flicker on a photo":       {project(artboard("feed", 0, stillPhoto(Vfx{ID: "f", Kind: "lensFlare", Params: map[string]*float64{"flicker": float(0.4)}}))), FieldLayers, CodeInvalid},
		"a time offset":              {project(artboard("feed", 0, stillPhoto(Vfx{ID: "g", Kind: "grain", OffsetMS: 20}))), FieldLayers, CodeInvalid},
		"keyframes":                  {project(artboard("feed", 0, stillPhoto(Vfx{ID: "g", Kind: "grain", Keyframes: keyedAmount}))), FieldLayers, CodeInvalid},
		"an amount out of range":     {project(artboard("feed", 0, stillPhoto(Vfx{ID: "g", Kind: "grain", Params: map[string]*float64{"amount": float(2)}}))), FieldLayers, CodeOutOfRange},
		"an effect on a shape":       {project(artboard("feed", 0, Layer{ID: "box", Type: LayerShape, Shape: ShapeRect, Fill: "#000000", Transform: box(), Vfx: []Vfx{{ID: "g", Kind: "grain"}}})), FieldLayers, CodeInvalid},
		"a shake on the artboard": {func() ImageDocument {
			board := artboard("feed", 0)
			board.Canvas.Vfx = []Vfx{{ID: "s", Kind: "shake"}}
			return project(board)
		}(), FieldCanvas, CodeUnknown},
	}
	for name, c := range cases {
		if got := codeOf(t, ValidateDocument(KindImage, mustJSON(t, c.doc)))[c.field]; got != c.code {
			t.Errorf("%s: %s = %q, want %q", name, c.field, got, c.code)
		}
	}
}

func TestStillEffectsLiveOnPhotosAndArtboards(t *testing.T) {
	board := artboard("feed", 0, stillPhoto(Vfx{ID: "g", Kind: "grain", Seed: 4, Params: map[string]*float64{"amount": float(0.5)}}))
	board.Canvas.Vfx = []Vfx{{ID: "v", Kind: "vignette", Colors: map[string]*string{"color": func() *string { c := "#101010"; return &c }()}}}
	if err := ValidateDocument(KindImage, mustJSON(t, project(board))); err != nil {
		t.Fatal(err)
	}
}

func TestAnOverlayClipLayerCarriesNoStillEffects(t *testing.T) {
	doc := videoDoc()
	layer := *doc.Tracks[1].Clips[0].Layer
	layer.Vfx = []Vfx{{ID: "g", Kind: "grain"}}
	doc.Tracks[1].Clips[0].Layer = &layer
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err == nil {
		t.Fatal("an overlay layer carried a still effect")
	}
}
