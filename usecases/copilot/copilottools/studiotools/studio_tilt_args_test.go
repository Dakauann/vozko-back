package studiotools

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/studio"
)

func TestStudioEditPassesTiltOperations(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "set_canvas", "perspective": 0.0},
		map[string]interface{}{"op": "update_clip", "clip_ids": []interface{}{"c-1", "c-2"}, "rotation_x": -20.0, "rotation_y": 45.0},
		map[string]interface{}{"op": "animate", "clip_id": "c-3", "property": "rotationY", "keys": []interface{}{map[string]interface{}{"at_ms": 0, "value": 0.0}, map[string]interface{}{"at_ms": 800, "value": 180.0}}},
		map[string]interface{}{"op": "add_text_animator", "clip_id": "c-4", "animator_preset": "flipIn", "animator_properties": map[string]interface{}{"rotationX": 90.0, "rotationY": -30.0}},
		map[string]interface{}{"op": "update_text_animator", "clip_id": "c-4", "remove_properties": []interface{}{"rotationY"}},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{`"perspective":0`, `"rotation_x":-20`, `"rotation_y":45`, `"property":"rotationY"`, `"value":180`, `"animator_preset":"flipIn"`, `"rotationX":90`, `"rotationY":-30`, `"remove_properties":["rotationY"]`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
}

func TestStudioEditRefusesTurnsOutsideTheirRange(t *testing.T) {
	for _, op := range []map[string]interface{}{
		{"op": "update_clip", "clip_id": "c-1", "rotation_x": studio.MaxTilt + 1},
		{"op": "update_clip", "clip_id": "c-1", "rotation_y": -studio.MaxTilt - 1},
		{"op": "set_canvas", "perspective": studio.MaxPerspective + 1},
		{"op": "set_canvas", "perspective": -1.0},
	} {
		editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
		edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
		res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), map[string]interface{}{"operations": []interface{}{op}})
		if res.Status == copilot.StatusOK {
			t.Fatalf("%v was accepted", op)
		}
		if _, sent := editor.sent(copilot.ScreenEdit); sent {
			t.Fatalf("%v reached the editor", op)
		}
	}
}

func TestStudioTiltSchemaOffersTheEditorFields(t *testing.T) {
	properties := NewStudioEditVideoTool(StudioDeps{}).Definition().Parameters["operations"].Items.Properties
	for _, name := range []string{"rotation_x", "rotation_y", "perspective"} {
		if properties[name].Type != "number" {
			t.Fatalf("%s must be a number, got %q", name, properties[name].Type)
		}
	}
	for _, want := range []string{"rotationX", "rotationY"} {
		if !slices.Contains(properties["property"].Enum, want) || properties["remove_properties"].Items == nil || !slices.Contains(properties["remove_properties"].Items.Enum, want) {
			t.Fatalf("%s is missing from the animate property list or the animator removals", want)
		}
	}
	if !slices.Contains(properties["animator_preset"].Enum, "flipIn") {
		t.Fatal("the flipIn preset is missing")
	}
	tags := map[string]bool{}
	kind := reflect.TypeOf(studioAnimatorPropertiesArgs{})
	for i := 0; i < kind.NumField(); i++ {
		tags[strings.Split(kind.Field(i).Tag.Get("json"), ",")[0]] = true
	}
	if !tags["rotationX"] || !tags["rotationY"] {
		t.Fatal("animator_properties lost rotationX or rotationY")
	}
	if _, ok := NewStudioEditImageTool(StudioDeps{}).Definition().Parameters["operations"].Items.Properties["rotation_x"]; ok {
		t.Fatal("the image tool offers a 3D rotation, which only video clips have")
	}
}

func TestStudioEditGuideExplainsTilt(t *testing.T) {
	description := NewStudioEditVideoTool(StudioDeps{}).Definition().Description
	for _, want := range []string{"Rotação 3D", "rotation_x", "rotation_y", "rotationX", "rotationY", "perspective", "flipIn", "40"} {
		if !strings.Contains(description, want) {
			t.Fatalf("the video guide does not explain %s", want)
		}
	}
}
