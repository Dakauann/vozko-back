package studiotools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/studio"
	"vozko/usecases/copilot/copilottools"
)

type fakePresetCatalog struct {
	saved []studio.AnimationPreset
	err   error
	asked string
}

func (f *fakePresetCatalog) List(_ context.Context, workspaceID string) ([]studio.AnimationPreset, error) {
	f.asked = workspaceID
	if f.err != nil {
		return nil, f.err
	}
	builtins, err := studio.BuiltinPresets()
	if err != nil {
		return nil, err
	}
	return append(builtins, f.saved...), nil
}

func presetDeps(catalog *fakePresetCatalog) StudioDeps {
	deps := studioDeps(studio.KindVideo, nil, nil)
	deps.Presets = catalog
	return deps
}

func listPresets(t *testing.T, catalog *fakePresetCatalog, locale string, args map[string]interface{}) presetResult {
	t.Helper()
	editor := &fakeEditor{}
	cc := studioSession(copilot.StudioVideo, editor)
	cc.Locale = locale
	res := NewStudioAnimationPresetsTool(presetDeps(catalog)).Execute(context.Background(), cc, args)
	if res.Status != copilot.StatusOK {
		t.Fatalf("list %v = %+v", args, res)
	}
	if len(editor.commands) > 0 {
		t.Fatal("the preset list runs on the server and never reaches the editor")
	}
	return res.Data.(presetResult)
}

func savedSpin() studio.AnimationPreset {
	return studio.AnimationPreset{ID: "0b6d3f4e-2c1a-4f5e-9d8c-7b6a5f4e3d2c", WorkspaceID: "ws-1", Name: "Giro da marca", Description: "Uma volta rápida",
		Animation: json.RawMessage(`{"scope":"visual","durationMs":1000,"keyframes":{"rotation":[{"atMs":0,"value":0,"easing":"easeOut"},{"atMs":1000,"value":360,"easing":"linear"}]},"vfx":[{"id":"g","kind":"grain","seed":1}]}`)}
}

func TestThePresetListSpeaksTheThreadLanguage(t *testing.T) {
	catalog := &fakePresetCatalog{saved: []studio.AnimationPreset{savedSpin()}}
	got := listPresets(t, catalog, "en-US", nil)
	builtins, _ := studio.BuiltinPresets()
	if got.Total != len(builtins)+1 || len(got.Presets) != got.Total || catalog.asked != "ws-1" {
		t.Fatalf("result = %+v", got)
	}
	first := got.Presets[0]
	if first.PresetID != "builtin.riseIn" || first.Name != "Rise in" || !first.BuiltIn || first.Scope != "visual" || first.DurationMS != 1500 {
		t.Fatalf("first = %+v", first)
	}
	if strings.Join(first.Keys, ",") != "opacity,y" || strings.Join(first.Behaviors, ",") != "overshoot" || first.Entrance != "" {
		t.Fatalf("first summary = %+v", first)
	}
	last := got.Presets[len(got.Presets)-1]
	if last.Name != "Giro da marca" || last.BuiltIn || strings.Join(last.Vfx, ",") != "grain" || strings.Join(last.Keys, ",") != "rotation" {
		t.Fatalf("saved = %+v", last)
	}
	pt := listPresets(t, catalog, "", nil)
	if pt.Presets[0].Name != "Subir e surgir" {
		t.Fatalf("fallback = %+v", pt.Presets[0])
	}
	raw := renderJSON(got)
	for _, field := range []string{`"preset_id"`, `"built_in"`, `"duration_ms"`, `"text_animators"`, `"motion_blur"`, `"entrance"`, `"exit"`} {
		if !strings.Contains(raw, field) {
			t.Fatalf("missing %s in %s", field, raw)
		}
	}
}

func TestThePresetQueryIgnoresAccentsAndLanguage(t *testing.T) {
	catalog := &fakePresetCatalog{saved: []studio.AnimationPreset{savedSpin()}}
	cascade := listPresets(t, catalog, "en", map[string]interface{}{"query": "cascade"})
	if cascade.Total != 1 || cascade.Presets[0].PresetID != "builtin.wordCascade" || cascade.Presets[0].Scope != "text" || cascade.Presets[0].Entrance != "text" {
		t.Fatalf("cascade = %+v", cascade)
	}
	portuguese := listPresets(t, catalog, "en", map[string]interface{}{"query": "palavras"})
	if portuguese.Total != 1 || portuguese.Presets[0].Name != "Word cascade" {
		t.Fatalf("pt fallback = %+v", portuguese)
	}
	accent := listPresets(t, catalog, "pt", map[string]interface{}{"query": "rapida"})
	if accent.Total != 1 || accent.Presets[0].Name != "Giro da marca" {
		t.Fatalf("accent = %+v", accent)
	}
	none := listPresets(t, catalog, "pt", map[string]interface{}{"query": "inexistente"})
	if none.Total != 0 || none.Presets == nil {
		t.Fatalf("none = %+v", none)
	}
}

func TestThePresetListFailsClosed(t *testing.T) {
	tool := NewStudioAnimationPresetsTool(presetDeps(&fakePresetCatalog{err: errors.New("db down")}))
	if res := tool.Execute(context.Background(), studioSession(copilot.StudioVideo, &fakeEditor{}), nil); res.Status != copilot.StatusError {
		t.Fatalf("a broken repository answered %+v", res)
	}
	outside := studioSession(copilot.StudioVideo, &fakeEditor{})
	outside.View = copilot.View{}
	if res := NewStudioAnimationPresetsTool(presetDeps(&fakePresetCatalog{})).Execute(context.Background(), outside, nil); res.Status != copilot.StatusError {
		t.Fatalf("outside the studio %+v", res)
	}
	if res := NewStudioAnimationPresetsTool(StudioDeps{}).Execute(context.Background(), studioSession(copilot.StudioVideo, &fakeEditor{}), nil); res.Status != copilot.StatusError {
		t.Fatalf("no catalog %+v", res)
	}
	broken := studio.AnimationPreset{ID: "x", Name: "Quebrada", Animation: json.RawMessage(`{}`)}
	got := listPresets(t, &fakePresetCatalog{saved: []studio.AnimationPreset{broken}}, "pt", map[string]interface{}{"query": "quebrada"})
	if got.Total != 0 {
		t.Fatalf("an invalid stored preset was offered: %+v", got)
	}
	if copilot.NeedsApproval(tool, copilot.ModeAsk) {
		t.Fatal("a list changes nothing and never asks for approval")
	}
	video := copilot.View{Surface: copilot.SurfaceStudio, ProjectID: studioProjectID, ProjectKind: copilot.StudioVideo}
	image := video
	image.ProjectKind = copilot.StudioImage
	if !video.Offers(tool) || image.Offers(tool) {
		t.Fatal("presets belong to the video editor only")
	}
}

func TestSavingAPresetRunsTheEditorCommand(t *testing.T) {
	reply := `{"preset_id":"p-1","name":"Giro","scope":"visual","duration_ms":1000,"keys":["rotation"],"behaviors":[],"entrance":"","exit":"","text_animators":0,"vfx":[],"motion_blur":false,"skipped":[]}`
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenSavePreset: {OK: true, Data: json.RawMessage(reply)}}}
	tool := NewStudioSaveAnimationPresetTool(presetDeps(&fakePresetCatalog{}))
	args := map[string]interface{}{"clip_id": " c-title ", "name": "  Giro   da marca ", "description": " Uma volta "}
	cc := studioSession(copilot.StudioVideo, editor)
	if err := tool.(copilot.Validator).Validate(context.Background(), cc, args); err != nil {
		t.Fatal(err)
	}
	res := tool.Execute(context.Background(), cc, args)
	if res.Status != copilot.StatusOK || renderJSON(res.Data) != reply {
		t.Fatalf("result = %+v", res)
	}
	cmd, ok := editor.sent(copilot.ScreenSavePreset)
	if !ok || renderJSON(cmd.Args) != `{"clip_id":"c-title","description":"Uma volta","name":"Giro da marca"}` {
		t.Fatalf("command = %+v", cmd)
	}
	if !copilot.NeedsApproval(tool, copilot.ModeAsk) || copilot.NeedsApproval(tool, copilot.ModeFull) {
		t.Fatal("saving asks for approval only in ask mode")
	}
	fields := tool.(copilot.Describer).Describe(context.Background(), cc, args)
	if len(fields) != 2 || fields[0].Value != "Giro   da marca" || fields[1].Value != "c-title" {
		t.Fatalf("card = %+v", fields)
	}
	def := tool.Definition()
	if !copilottools.Contains(def.Required, "clip_id") || !copilottools.Contains(def.Required, "name") || copilottools.Contains(def.Required, "description") {
		t.Fatalf("required = %v", def.Required)
	}
	if copilot.ScreenSavePreset.Timeout() <= 0 {
		t.Fatal("the save command has no bounded wait")
	}
}

func TestSavingAPresetRefusesBadArgumentsBeforeTheEditor(t *testing.T) {
	cases := map[string]map[string]interface{}{
		"no clip":          {"name": "Giro"},
		"blank clip":       {"clip_id": "  ", "name": "Giro"},
		"no name":          {"clip_id": "c-1"},
		"blank name":       {"clip_id": "c-1", "name": "   "},
		"long name":        {"clip_id": "c-1", "name": strings.Repeat("a", studio.MaxPresetNameRunes+1)},
		"long description": {"clip_id": "c-1", "name": "Giro", "description": strings.Repeat("é", studio.MaxPresetDescriptionRunes+1)},
	}
	for name, args := range cases {
		editor := &fakeEditor{}
		tool := NewStudioSaveAnimationPresetTool(presetDeps(&fakePresetCatalog{}))
		cc := studioSession(copilot.StudioVideo, editor)
		if err := tool.(copilot.Validator).Validate(context.Background(), cc, args); err == nil {
			t.Errorf("%s: validated", name)
		}
		if res := tool.Execute(context.Background(), cc, args); res.Status != copilot.StatusError || len(editor.commands) > 0 {
			t.Errorf("%s: %+v", name, res)
		}
	}
	noEditor := studioSession(copilot.StudioVideo, nil)
	noEditor.Screen = nil
	tool := NewStudioSaveAnimationPresetTool(presetDeps(&fakePresetCatalog{}))
	if err := tool.(copilot.Validator).Validate(context.Background(), noEditor, map[string]interface{}{"clip_id": "c-1", "name": "Giro"}); err == nil {
		t.Fatal("a save without the editor was validated")
	}
	refusing := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenSavePreset: {OK: false, Error: &copilot.ScreenError{Code: "nothing_to_save", Message: "o clipe não tem animação"}}}}
	res := tool.Execute(context.Background(), studioSession(copilot.StudioVideo, refusing), map[string]interface{}{"clip_id": "c-1", "name": "Giro"})
	if res.Status != copilot.StatusError || !strings.Contains(res.Message, "o clipe não tem animação") {
		t.Fatalf("refusal = %+v", res)
	}
}

func TestStudioToolsOfferThePresetTools(t *testing.T) {
	names := map[string]bool{}
	for _, tool := range StudioTools(StudioDeps{}) {
		names[tool.Definition().Name] = true
	}
	if !names["studio_animation_presets"] || !names["studio_save_animation_preset"] {
		t.Fatalf("tools = %v", names)
	}
}
