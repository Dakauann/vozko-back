package studio

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"vozko/domain/mediagen"
)

func codeOf(t *testing.T, err error) map[string]string {
	t.Helper()
	var invalid *ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("expected a validation error, got %v", err)
	}
	return invalid.Codes()
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func box() Transform { return Transform{X: 0.5, Y: 0.5, W: 0.5, H: 0.2, Opacity: 1} }

func imageDoc() LegacyImageDocument {
	return LegacyImageDocument{
		Schema: SchemaImage, Version: DocumentVersion,
		Canvas: Canvas{Width: 1080, Height: 1350, Background: "#ffffff"},
		Layers: []Layer{
			{ID: "photo", Type: LayerImage, AssetID: "m-1", Transform: box(), Crop: &Crop{X: 0.1, Y: 0.1, W: 0.8, H: 0.8}, Filters: &Filters{Brightness: 0.1}},
			{ID: "title", Type: LayerText, Text: "Oferta da semana", FontID: "montserrat", FontSize: 0.06, FontWeight: 800, Fill: "#111111", Align: "center", Transform: box()},
			{ID: "arrow", Type: LayerShape, Shape: ShapeArrow, Stroke: "#ff0000", StrokeWidth: 8, ArrowEnd: true, Transform: box()},
			{ID: "star", Type: LayerIcon, IconID: "star", Fill: "#ffcc00", Transform: box()},
		},
	}
}

func videoDoc() VideoDocument {
	full := mediagen.FullFrame()
	text := Layer{ID: "headline", Type: LayerText, Text: "Todos os canais", FontID: "inter", FontSize: 0.05, Fill: "#ffffff", Transform: box()}
	return VideoDocument{
		Schema: SchemaVideo, Version: DocumentVersion, DurationMS: 6_000,
		Canvas: VideoCanvas{Aspect: mediagen.AspectStory, Background: "#000000"},
		Tracks: []Track{
			{ID: "main", Kind: TrackVisual, Clips: []Clip{
				{ID: "c1", Type: ClipImage, AssetID: "m-1", DurationMS: 3_000, Fit: mediagen.FitCover, Transform: full},
				{ID: "c2", Type: ClipVideo, AssetID: "m-2", StartMS: 3_000, DurationMS: 3_000, TrimInMS: 1_200, Fit: mediagen.FitContain, Transform: full},
			}},
			{ID: "text", Kind: TrackVisual, Clips: []Clip{
				{ID: "o1", Type: ClipOverlay, StartMS: 500, DurationMS: 2_000, FadeInMS: 200, Layer: &text, Transform: mediagen.Transform{X: 0.5, Y: 0.2, W: 0.8, H: 0.1, Opacity: 1}},
			}},
			{ID: "music", Kind: TrackAudio, Clips: []Clip{{ID: "a1", Type: ClipAudio, AssetID: "song", DurationMS: 6_000, Volume: 0.3, FadeOutMS: 1_500}}},
			{ID: "muted", Kind: TrackAudio, Muted: true, Clips: []Clip{{ID: "a2", Type: ClipAudio, AssetID: "voice", DurationMS: 2_000, Volume: 1}}},
		},
	}
}

func TestValidDocumentsAreAccepted(t *testing.T) {
	if err := ValidateDocument(KindImage, mustJSON(t, imageDoc())); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDocument(KindVideo, mustJSON(t, videoDoc())); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownFieldsAndOtherSchemasAreRefused(t *testing.T) {
	raw := strings.Replace(string(mustJSON(t, imageDoc())), `"schema"`, `"script":"x","schema"`, 1)
	if err := ValidateDocument(KindImage, json.RawMessage(raw)); !errors.Is(err, ErrInvalidDocument) {
		t.Fatalf("got %v", err)
	}
	if codeOf(t, ValidateDocument(KindVideo, mustJSON(t, imageDoc())))[FieldDocument] == "" {
		t.Fatal("an image document passed as video")
	}
	if codeOf(t, ValidateDocument("gif", mustJSON(t, imageDoc())))[FieldKind] != CodeUnknown {
		t.Fatal("unknown kind")
	}
}

func TestImageLayersFollowTheirTypeRules(t *testing.T) {
	cases := map[string]struct {
		change func(*LegacyImageDocument)
		code   string
	}{
		"unknown font":      {func(d *LegacyImageDocument) { d.Layers[1].FontID = "comic-sans" }, CodeUnknown},
		"empty text":        {func(d *LegacyImageDocument) { d.Layers[1].Text = "  " }, CodeInvalid},
		"bad weight":        {func(d *LegacyImageDocument) { d.Layers[1].FontWeight = 450 }, CodeOutOfRange},
		"image without one": {func(d *LegacyImageDocument) { d.Layers[0].AssetID = "" }, CodeRequired},
		"crop outside":      {func(d *LegacyImageDocument) { d.Layers[0].Crop.W = 0.95 }, CodeOutOfRange},
		"unknown shape":     {func(d *LegacyImageDocument) { d.Layers[2].Shape = "blob" }, CodeUnknown},
		"bad color":         {func(d *LegacyImageDocument) { d.Layers[2].Stroke = "red" }, CodeInvalid},
		"duplicate id":      {func(d *LegacyImageDocument) { d.Layers[3].ID = "photo" }, CodeDuplicate},
		"bad id":            {func(d *LegacyImageDocument) { d.Layers[3].ID = "Star!" }, CodeInvalid},
		"too transparent":   {func(d *LegacyImageDocument) { d.Layers[3].Transform.Opacity = 1.5 }, CodeOutOfRange},
		"no icon":           {func(d *LegacyImageDocument) { d.Layers[3].IconID = "" }, CodeInvalid},
	}
	for name, c := range cases {
		doc := imageDoc()
		c.change(&doc)
		if got := codeOf(t, ValidateDocument(KindImage, mustJSON(t, doc)))[FieldLayers]; got != c.code {
			t.Errorf("%s: code %q", name, got)
		}
	}
	tiny := imageDoc()
	tiny.Canvas.Width = 50
	if codeOf(t, ValidateDocument(KindImage, mustJSON(t, tiny)))[FieldCanvas] != CodeOutOfRange {
		t.Fatal("tiny canvas")
	}
}

func TestImageLayersCarryEffectsBlendingAndGroups(t *testing.T) {
	doc := imageDoc()
	doc.Canvas.Gradient = &Gradient{From: "#ff0000", To: "#0000ff", Angle: 45}
	doc.Layers[0].BlendMode = "multiply"
	doc.Layers[0].Frame = ShapeEllipse
	doc.Layers[0].GroupID = "inner"
	doc.Layers[1].Clip = true
	doc.Layers[1].Highlight = &Highlight{Color: "#ffee00", Radius: 0.3}
	doc.Layers[1].Curve = -0.5
	doc.Layers[1].Gradient = &Gradient{From: "#111111", To: "#eeeeee"}
	doc.Layers[1].GroupID = "inner"
	doc.Layers[2].Gradient = &Gradient{Kind: "radial", From: "#ffffff", Via: "#ff8800", To: "#000000", CX: 0.5, CY: 0.4, Radius: 1}
	doc.Groups = []Group{{ID: "outer", Name: "Cabeçalho"}, {ID: "inner", ParentID: "outer"}}
	if err := ValidateDocument(KindImage, mustJSON(t, doc)); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(d *LegacyImageDocument){
		"blend":     func(d *LegacyImageDocument) { d.Layers[0].BlendMode = "dissolve" },
		"frame":     func(d *LegacyImageDocument) { d.Layers[0].Frame = ShapeArrow },
		"curve":     func(d *LegacyImageDocument) { d.Layers[1].Curve = 1.5 },
		"highlight": func(d *LegacyImageDocument) { d.Layers[1].Highlight = &Highlight{Color: "yellow"} },
		"gradient":  func(d *LegacyImageDocument) { d.Layers[1].Gradient = &Gradient{From: "#111111"} },
		"canvas gradient": func(d *LegacyImageDocument) {
			d.Canvas.Gradient = &Gradient{From: "#111111", To: "#222222", Angle: 999}
		},
		"gradient kind": func(d *LegacyImageDocument) {
			d.Layers[2].Gradient = &Gradient{Kind: "conic", From: "#111111", To: "#222222"}
		},
		"radial radius": func(d *LegacyImageDocument) {
			d.Layers[2].Gradient = &Gradient{Kind: "radial", From: "#111111", To: "#222222", CX: 0.5, CY: 0.5}
		},
		"radial center": func(d *LegacyImageDocument) {
			d.Layers[2].Gradient = &Gradient{Kind: "radial", From: "#111111", To: "#222222", CX: 1.5, CY: 0.5, Radius: 1}
		},
		"via color": func(d *LegacyImageDocument) {
			d.Layers[2].Gradient = &Gradient{From: "#111111", To: "#222222", Via: "orange"}
		},
		"unknown parent":  func(d *LegacyImageDocument) { d.Groups = []Group{{ID: "inner", ParentID: "ghost"}} },
		"cycle":           func(d *LegacyImageDocument) { d.Groups = []Group{{ID: "a", ParentID: "b"}, {ID: "b", ParentID: "a"}} },
		"self parent":     func(d *LegacyImageDocument) { d.Groups = []Group{{ID: "a", ParentID: "a"}} },
		"duplicate group": func(d *LegacyImageDocument) { d.Groups = []Group{{ID: "a"}, {ID: "a"}} },
		"group token":     func(d *LegacyImageDocument) { d.Groups = []Group{{ID: "Not A Token"}} },
	}
	for name, mutate := range cases {
		bad := doc
		bad.Layers = append([]Layer(nil), doc.Layers...)
		bad.Groups = append([]Group(nil), doc.Groups...)
		mutate(&bad)
		if err := ValidateDocument(KindImage, mustJSON(t, bad)); err == nil {
			t.Errorf("%s: an invalid document was accepted", name)
		}
	}
}

func TestImageScaffoldsNameOneOfTheirOwnLayersAsTheBase(t *testing.T) {
	doc := imageDoc()
	doc.Layers[0].GroupID = "card"
	doc.Layers[1].GroupID = "card"
	doc.Groups = []Group{{ID: "card", BaseID: "photo"}}
	if err := ValidateDocument(KindImage, mustJSON(t, doc)); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(d *LegacyImageDocument){
		"missing base": func(d *LegacyImageDocument) { d.Groups[0].BaseID = "ghost" },
		"base outside": func(d *LegacyImageDocument) { d.Groups[0].BaseID = "arrow" },
		"base token":   func(d *LegacyImageDocument) { d.Groups[0].BaseID = "Not A Token" },
		"base of two":  func(d *LegacyImageDocument) { d.Groups = append(d.Groups, Group{ID: "other", BaseID: "photo"}) },
		"nested base only": func(d *LegacyImageDocument) {
			d.Layers[0].GroupID = "inner"
			d.Groups = append(d.Groups, Group{ID: "inner", ParentID: "card"})
		},
	}
	for name, mutate := range cases {
		bad := doc
		bad.Layers = append([]Layer(nil), doc.Layers...)
		bad.Groups = append([]Group(nil), doc.Groups...)
		mutate(&bad)
		if got := codeOf(t, ValidateDocument(KindImage, mustJSON(t, bad)))[FieldLayers]; got != CodeInvalid {
			t.Errorf("%s: code %q", name, got)
		}
	}
}

func TestVideoTracksKeepTheTimelineRules(t *testing.T) {
	cases := map[string]struct {
		change func(*VideoDocument)
		code   string
	}{
		"overlap":            {func(d *VideoDocument) { d.Tracks[0].Clips[1].StartMS = 2_500 }, CodeOverlap},
		"past the end":       {func(d *VideoDocument) { d.Tracks[0].Clips[1].DurationMS = 4_000 }, CodeOutOfRange},
		"audio on visual":    {func(d *VideoDocument) { d.Tracks[0].Clips[0].Type = ClipAudio }, CodeUnknown},
		"visual on audio":    {func(d *VideoDocument) { d.Tracks[2].Clips[0].Type = ClipImage }, CodeInvalid},
		"overlay with asset": {func(d *VideoDocument) { d.Tracks[1].Clips[0].AssetID = "m-9" }, CodeInvalid},
		"bad overlay layer":  {func(d *VideoDocument) { d.Tracks[1].Clips[0].Layer.FontID = "x" }, CodeUnknown},
		"duplicate clip id":  {func(d *VideoDocument) { d.Tracks[2].Clips[0].ID = "c1" }, CodeInvalid},
		"too loud":           {func(d *VideoDocument) { d.Tracks[2].Clips[0].Volume = 3 }, CodeOutOfRange},
	}
	for name, c := range cases {
		doc := videoDoc()
		c.change(&doc)
		if got := codeOf(t, ValidateDocument(KindVideo, mustJSON(t, doc)))[FieldTracks]; got != c.code {
			t.Errorf("%s: code %q", name, got)
		}
	}
	unknown := videoDoc()
	unknown.Canvas.Aspect = "panorama"
	if codeOf(t, ValidateDocument(KindVideo, mustJSON(t, unknown)))[FieldCanvas] != CodeUnknown {
		t.Fatal("unknown aspect")
	}
	wide := videoDoc()
	wide.Canvas.Aspect = mediagen.AspectLandscape
	if err := ValidateDocument(KindVideo, mustJSON(t, wide)); err != nil {
		t.Fatalf("landscape refused: %v", err)
	}
}

func TestADocumentHasASizeLimit(t *testing.T) {
	raw := append(mustJSON(t, imageDoc()), []byte(strings.Repeat(" ", MaxDocumentBytes))...)
	if codeOf(t, ValidateDocument(KindImage, raw))[FieldDocument] != CodeTooLarge {
		t.Fatal("an oversized document was accepted")
	}
}

func TestAProjectIsCreatedAndChangedOnlyWithValidContent(t *testing.T) {
	p, err := NewProject("ws", "u-1", KindImage, "  Post   de   segunda ", mustJSON(t, imageDoc()))
	if err != nil || p.Name != "Post de segunda" || p.Version != 1 {
		t.Fatalf("project %+v err %v", p, err)
	}
	if _, err := NewProject("ws", "", KindImage, "x", mustJSON(t, imageDoc())); !errors.Is(err, ErrCreatorRequired) {
		t.Fatalf("got %v", err)
	}
	broken := imageDoc()
	broken.Layers[1].FontID = "x"
	if err := p.Apply(Change{Document: mustJSON(t, broken)}); err == nil {
		t.Fatal("an invalid document was applied")
	}
	name := "Novo nome"
	if err := p.Apply(Change{Name: &name}); err != nil || p.Name != name {
		t.Fatalf("rename %v", err)
	}
	if err := p.Apply(Change{}); err == nil {
		t.Fatal("an empty change was applied")
	}
}
