package studiotools

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/studio"
)

func TestStudioEditPassesClipRepeatOperations(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "set_repeat", "clip_id": "c-1", "repeat": map[string]interface{}{"mode": "linear", "copies": 900.0, "x": 60.0, "end_opacity": 0.1, "order": "below"}},
		map[string]interface{}{"op": "set_repeat", "clip_ids": []interface{}{"c-2"}, "repeat": map[string]interface{}{"mode": "radial", "count": 8.0, "radius": 120.0}},
		map[string]interface{}{"op": "clear_repeat", "clip_id": "c-3"},
		map[string]interface{}{"op": "animate_repeat", "clip_id": "c-1", "repeat_property": "shift", "keys": []interface{}{
			map[string]interface{}{"at_ms": 0.0, "value": 0.0}, map[string]interface{}{"at_ms": 1500.0, "value": 2.0},
		}},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{`"op":"set_repeat"`, `"mode":"linear"`, `"copies":900`, `"end_opacity":0.1`, `"order":"below"`, `"mode":"radial"`, `"radius":120`, `"op":"clear_repeat"`, `"op":"animate_repeat"`, `"repeat_property":"shift"`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
}

func TestStudioEditRefusesBrokenClipRepeats(t *testing.T) {
	set := func(repeat map[string]interface{}) map[string]interface{} {
		return map[string]interface{}{"op": "set_repeat", "clip_id": "c-1", "repeat": repeat}
	}
	for name, op := range map[string]map[string]interface{}{
		"an unknown mode":       set(map[string]interface{}{"mode": "spiral"}),
		"an unknown order":      set(map[string]interface{}{"mode": "linear", "order": "middle"}),
		"no copies":             set(map[string]interface{}{"mode": "linear", "copies": 0.0}),
		"a scale of zero":       set(map[string]interface{}{"mode": "linear", "scale": 0.0}),
		"an opacity past one":   set(map[string]interface{}{"mode": "linear", "end_opacity": 2.0}),
		"a shift out of range":  set(map[string]interface{}{"mode": "linear", "shift": studio.MaxClipRepeatShift + 1}),
		"a radius out of range": set(map[string]interface{}{"mode": "radial", "radius": studio.MaxRepeatRadius + 1}),
		"a count of one":        set(map[string]interface{}{"mode": "radial", "count": 1.0}),
		"an unknown layout":     set(map[string]interface{}{"mode": "grid", "layout": "hex"}),
		"an unknown key target": {"op": "animate_repeat", "clip_id": "c-1", "repeat_property": "count"},
	} {
		editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
		edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
		res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), map[string]interface{}{"operations": []interface{}{op}})
		if res.Status == copilot.StatusOK {
			t.Fatalf("%s was accepted", name)
		}
		if _, sent := editor.sent(copilot.ScreenEdit); sent {
			t.Fatalf("%s reached the editor", name)
		}
	}
}

func TestStudioClipRepeatSchemaOffersTheEditorFields(t *testing.T) {
	video := NewStudioEditVideoTool(StudioDeps{}).Definition().Parameters["operations"].Items.Properties
	repeat := video["repeat"]
	if repeat.Type != "object" || repeat.Items == nil || repeat.Items.Properties["mode"].Type != "string" || repeat.Items.Properties["copies"].Type != "integer" || repeat.Items.Properties["radius"].Type != "number" {
		t.Fatalf("repeat must be an object with mode, copies and radius, got %+v", repeat)
	}
	if video["repeat_property"].Type != "string" {
		t.Fatal("the video tool must offer repeat_property")
	}
	image := NewStudioEditImageTool(StudioDeps{}).Definition().Parameters["operations"].Items.Properties
	if _, ok := image["repeat_property"]; ok {
		t.Fatal("the image tool offers repeat_property, which only video animates")
	}
	if _, ok := image["repeat"].Items.Properties["copies"]; ok {
		t.Fatal("the image repeat gained the linear fields of the clip repeat")
	}
	if image["repeat"].Items.Properties["count"].Type != "integer" || image["repeat"].Items.Properties["mode"].Type != "string" {
		t.Fatal("the image repeat lost its fields in the split")
	}
}

func TestStudioEditGuideExplainsClipRepeat(t *testing.T) {
	description := NewStudioEditVideoTool(StudioDeps{}).Definition().Description
	for _, want := range []string{"set_repeat", "clear_repeat", "animate_repeat", "repeat_property", "@bola", "end_opacity"} {
		if !strings.Contains(description, want) {
			t.Fatalf("the video guide does not explain %s", want)
		}
	}
}
