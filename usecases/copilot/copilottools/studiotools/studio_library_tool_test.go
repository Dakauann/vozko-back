package studiotools

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/studio"
	"vozko/domain/studio/library"
	"vozko/usecases/copilot/copilottools"
)

func librarySearch(t *testing.T, kind copilot.StudioKind, locale string, args map[string]interface{}) libraryResult {
	t.Helper()
	cc := studioSession(kind, &fakeEditor{})
	cc.Locale = locale
	res := NewStudioLibrarySearchTool().Execute(context.Background(), cc, args)
	if res.Status != copilot.StatusOK {
		t.Fatalf("search %v = %+v", args, res)
	}
	return res.Data.(libraryResult)
}

func TestTheLibrarySearchAnswersInTheThreadLanguageWithoutTheEditor(t *testing.T) {
	editor := &fakeEditor{}
	cc := studioSession(copilot.StudioImage, editor)
	cc.Locale = "en"
	res := NewStudioLibrarySearchTool().Execute(context.Background(), cc, map[string]interface{}{"query": "heart"})
	got := res.Data.(libraryResult)
	if res.Status != copilot.StatusOK || len(got.Items) == 0 || got.Items[0].ItemID != "shape-heart" || got.Items[0].Name != "Heart" {
		t.Fatalf("result = %+v", res)
	}
	if len(editor.commands) > 0 {
		t.Fatal("the search runs on the server and never reaches the editor")
	}
	first := got.Items[0]
	if first.Kind != "layers" || first.Ratio != 1 || len(first.Colors) == 0 || got.Editor != "image" {
		t.Fatalf("item = %+v", first)
	}
	pt := librarySearch(t, copilot.StudioImage, "pt-BR", map[string]interface{}{"query": "coracao"})
	if pt.Items[0].ItemID != "shape-heart" || pt.Items[0].Name != "Coração" {
		t.Fatalf("pt = %+v", pt.Items[0])
	}
}

func TestTheLibrarySearchFollowsTheOpenEditor(t *testing.T) {
	video := librarySearch(t, copilot.StudioVideo, "pt", map[string]interface{}{"query": "nulo"})
	if video.Total == 0 || video.Items[0].ItemID != "control-null" {
		t.Fatalf("video = %+v", video)
	}
	image := librarySearch(t, copilot.StudioImage, "pt", map[string]interface{}{"query": "nulo"})
	for _, item := range image.Items {
		if item.ItemID == "control-null" || item.Category == "controls" {
			t.Fatalf("image = %+v", image)
		}
	}
	styles := librarySearch(t, copilot.StudioImage, "pt", map[string]interface{}{"query": "faixa de nome", "editor": "video"})
	if styles.Editor != "video" || styles.Items[0].ItemID != "text-style-lower-third" || styles.Items[0].Kind != "clips" {
		t.Fatalf("styles = %+v", styles)
	}
	if styles.Items[0].Slots[0].Slot != "text" || styles.Items[0].Slots[0].Sample != "Nome e cargo" {
		t.Fatalf("slots = %+v", styles.Items[0].Slots)
	}
}

func TestAnEmptyLibrarySearchListsTheCategories(t *testing.T) {
	got := librarySearch(t, copilot.StudioImage, "pt", nil)
	if len(got.Items) != 0 || len(got.Categories) == 0 {
		t.Fatalf("result = %+v", got)
	}
	for _, category := range got.Categories {
		if category.ID == "controls" || category.Count == 0 || category.Name == "" {
			t.Fatalf("category = %+v", category)
		}
	}
	icons := librarySearch(t, copilot.StudioImage, "pt", map[string]interface{}{"category": "icons", "limit": 3})
	if len(icons.Items) != 3 || icons.Total <= 3 {
		t.Fatalf("icons = %+v", icons)
	}
	for _, item := range icons.Items {
		if item.Category != "icons" {
			t.Fatalf("leaked %+v", item)
		}
	}
}

func TestTheLibrarySearchRefusesWhatItCannotAnswer(t *testing.T) {
	tool := NewStudioLibrarySearchTool()
	outside := studioSession(copilot.StudioImage, &fakeEditor{})
	outside.View = copilot.View{}
	cases := map[string]struct {
		cc   copilot.Context
		args map[string]interface{}
	}{
		"outside the studio": {outside, map[string]interface{}{"query": "seta"}},
		"unknown category":   {studioSession(copilot.StudioImage, &fakeEditor{}), map[string]interface{}{"category": "fotos"}},
		"negative limit":     {studioSession(copilot.StudioImage, &fakeEditor{}), map[string]interface{}{"query": "seta", "limit": -1}},
		"unknown editor":     {studioSession(copilot.StudioImage, &fakeEditor{}), map[string]interface{}{"query": "seta", "editor": "audio"}},
	}
	for name, tc := range cases {
		if res := tool.Execute(context.Background(), tc.cc, tc.args); res.Status != copilot.StatusError {
			t.Fatalf("%s: %+v", name, res)
		}
	}
	def := tool.Definition()
	if !copilottools.Contains(def.Parameters["category"].Enum, "shapes") || !copilottools.Contains(def.Parameters["category"].Enum, "icons") {
		t.Fatalf("category enum = %v", def.Parameters["category"].Enum)
	}
	if copilot.NeedsApproval(tool, copilot.ModeAsk) {
		t.Fatal("a search changes nothing and never asks for approval")
	}
}

func TestStudioToolsOfferTheLibrarySearch(t *testing.T) {
	found := false
	for _, tool := range StudioTools(StudioDeps{}) {
		if tool.Definition().Name == "studio_library_search" {
			found = true
		}
	}
	if !found {
		t.Fatal("the library search is not offered in the studio")
	}
	for _, edit := range []copilot.Tool{NewStudioEditImageTool(StudioDeps{}), NewStudioEditVideoTool(StudioDeps{})} {
		ops := edit.Definition().Parameters["operations"].Items
		if !copilottools.Contains(ops.Properties["op"].Enum, "add_element") || ops.Properties["item_id"].Type != "string" || ops.Properties["texts"].Items == nil || ops.Properties["colors"].Items == nil {
			t.Fatalf("%s lacks add_element", edit.Definition().Name)
		}
	}
}

func TestAddElementIsCheckedAgainstTheCatalogBeforeTheEditor(t *testing.T) {
	image := NewStudioEditImageTool(studioDeps(studio.KindImage, nil, nil))
	cc := studioSession(copilot.StudioImage, &fakeEditor{})
	batch := func(op map[string]interface{}) map[string]interface{} {
		return map[string]interface{}{"operations": []interface{}{op}}
	}
	refusals := map[string]struct {
		op   map[string]interface{}
		want string
	}{
		"missing id":     {map[string]interface{}{"op": "add_element"}, "item_id"},
		"misspelled id":  {map[string]interface{}{"op": "add_element", "item_id": "shape-hart"}, "shape-heart"},
		"video only":     {map[string]interface{}{"op": "add_element", "item_id": "control-null"}, "editor de imagem"},
		"unknown slot":   {map[string]interface{}{"op": "add_element", "item_id": "text-heading", "texts": []interface{}{map[string]interface{}{"slot": "title", "text": "Oi"}}}, "text"},
		"slot on a form": {map[string]interface{}{"op": "add_element", "item_id": "shape-heart", "texts": []interface{}{map[string]interface{}{"slot": "text", "text": "Oi"}}}, "não tem texto"},
	}
	for name, tc := range refusals {
		err := image.(copilot.Validator).Validate(context.Background(), cc, batch(tc.op))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	editor := &fakeEditor{}
	ok := batch(map[string]interface{}{"op": "add_element", "item_id": "text-heading", "x": 0.5, "y": 0.2, "texts": []interface{}{map[string]interface{}{"slot": "text", "text": "Oferta"}}, "colors": []interface{}{map[string]interface{}{"from": "#111111", "to": "#ff0055"}}})
	if res := image.Execute(context.Background(), studioSession(copilot.StudioImage, editor), ok); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{`"item_id":"text-heading"`, `"slot":"text"`, `"text":"Oferta"`, `"from":"#111111"`, `"to":"#ff0055"`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
	video := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	if err := video.(copilot.Validator).Validate(context.Background(), studioSession(copilot.StudioVideo, &fakeEditor{}), batch(map[string]interface{}{"op": "add_element", "item_id": "control-null", "at_ms": 1000})); err != nil {
		t.Fatal(err)
	}
}

func TestFramesAndGridsNeedClipsInTheVideoEditor(t *testing.T) {
	video := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	cc := studioSession(copilot.StudioVideo, &fakeEditor{})
	batch := func(op map[string]interface{}) map[string]interface{} {
		return map[string]interface{}{"operations": []interface{}{op}}
	}
	for _, id := range []string{"frame-circle", "grid-two-columns"} {
		err := video.(copilot.Validator).Validate(context.Background(), cc, batch(map[string]interface{}{"op": "add_element", "item_id": id}))
		if err == nil || !strings.Contains(err.Error(), "clip_ids") {
			t.Fatalf("%s without clips: %v", id, err)
		}
		if err := video.(copilot.Validator).Validate(context.Background(), cc, batch(map[string]interface{}{"op": "add_element", "item_id": id, "clip_ids": []interface{}{"c1", "c2"}})); err != nil {
			t.Fatalf("%s with clips: %v", id, err)
		}
	}
	if err := video.(copilot.Validator).Validate(context.Background(), cc, batch(map[string]interface{}{"op": "add_element", "item_id": "frame-heart", "clip_id": "c1"})); err != nil {
		t.Fatalf("one clip: %v", err)
	}
	if err := video.(copilot.Validator).Validate(context.Background(), cc, batch(map[string]interface{}{"op": "add_element", "item_id": "sticker-word-thanks", "duration_ms": 4000, "texts": []interface{}{map[string]interface{}{"slot": "text", "text": "VALEU"}}})); err != nil {
		t.Fatalf("sticker: %v", err)
	}
	if err := video.(copilot.Validator).Validate(context.Background(), cc, batch(map[string]interface{}{"op": "add_element", "item_id": "frame-polaroid", "clip_id": "c1"})); err == nil || !strings.Contains(err.Error(), "editor de vídeo") {
		t.Fatalf("a decorated frame stays in the image editor: %v", err)
	}
}

func TestFillFrameReachesTheImageEditor(t *testing.T) {
	image := NewStudioEditImageTool(studioDeps(studio.KindImage, nil, nil))
	editor := &fakeEditor{}
	ops := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "add_element", "ref": "grade", "item_id": "grid-two-by-two"},
		map[string]interface{}{"op": "fill_frame", "layer_id": "l1", "media_id": "m1"},
	}}
	if err := image.(copilot.Validator).Validate(context.Background(), studioSession(copilot.StudioImage, editor), ops); err != nil {
		t.Fatal(err)
	}
	if res := image.Execute(context.Background(), studioSession(copilot.StudioImage, editor), ops); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	if sent := renderJSON(cmd.Args); !strings.Contains(sent, `"op":"fill_frame"`) || !strings.Contains(sent, `"media_id":"m1"`) {
		t.Fatalf("the batch lost fill_frame: %s", sent)
	}
}

func TestTheLibrarySearchFindsFramesGridsAndStickers(t *testing.T) {
	catalog, err := library.Default()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ query, category, want string }{
		{"circulo", "frames", "frame-circle"},
		{"polaroide", "frames", "frame-polaroid"},
		{"colagem 2 por 2", "grids", "grid-two-by-two"},
		{"obrigado", "stickers", "sticker-word-thanks"},
		{"seta desenhada", "stickers", "sticker-arrow"},
	}
	for _, tc := range cases {
		found := catalog.Search(library.Query{Text: tc.query, Category: tc.category, Editor: "image", Locale: "pt"})
		if len(found) == 0 || found[0].ID != tc.want {
			t.Fatalf("%s: %v", tc.query, found)
		}
	}
	if found := catalog.Search(library.Query{Text: "polaroide", Editor: "video", Locale: "pt"}); len(found) != 0 {
		t.Fatalf("the polaroid has decorations only the image editor draws: %v", found[0].ID)
	}
}
