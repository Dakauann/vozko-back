package studiotools

import (
	"context"
	"slices"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/studio"
)

func TestStudioEditPassesAdjustmentOperations(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "add_effect_clip", "ref": "ajuste", "at_ms": 0, "duration_ms": 3000},
		map[string]interface{}{"op": "set_camera", "clip_id": "@ajuste", "camera_zoom": 1.5, "camera_pan_x": -0.1, "camera_pan_y": 0.2, "camera_rotation": 12.0},
		map[string]interface{}{"op": "animate", "clip_id": "@ajuste", "property": "cameraZoom", "keys": []interface{}{map[string]interface{}{"at_ms": 0, "value": 1.0}, map[string]interface{}{"at_ms": 3000, "value": 2.0}}},
		map[string]interface{}{"op": "set_camera", "clip_id": "@ajuste", "clear_camera": true},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{`"op":"set_camera"`, `"camera_zoom":1.5`, `"camera_pan_x":-0.1`, `"camera_pan_y":0.2`, `"camera_rotation":12`, `"property":"cameraZoom"`, `"clear_camera":true`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
}

func TestStudioEditRefusesACameraOutsideItsRange(t *testing.T) {
	for _, op := range []map[string]interface{}{
		{"op": "set_camera", "clip_id": "fx", "camera_zoom": studio.MaxCameraZoom + 1},
		{"op": "set_camera", "clip_id": "fx", "camera_zoom": studio.MinCameraZoom / 2},
		{"op": "set_camera", "clip_id": "fx", "camera_pan_x": studio.MaxCameraPan + 0.5},
		{"op": "set_camera", "clip_id": "fx", "camera_pan_y": -studio.MaxCameraPan - 0.5},
		{"op": "set_camera", "clip_id": "fx", "camera_rotation": studio.MaxCameraRotation + 1},
		{"op": "set_camera", "clip_id": "fx", "clear_camera": true, "camera_zoom": 2.0},
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

func TestStudioCameraSchemaOffersTheEditorFields(t *testing.T) {
	properties := NewStudioEditVideoTool(StudioDeps{}).Definition().Parameters["operations"].Items.Properties
	for name, kind := range map[string]string{"camera_pan_x": "number", "camera_pan_y": "number", "camera_zoom": "number", "camera_rotation": "number", "clear_camera": "boolean"} {
		if properties[name].Type != kind {
			t.Fatalf("%s must be a %s, got %q", name, kind, properties[name].Type)
		}
	}
	for _, want := range []string{"cameraPanX", "cameraPanY", "cameraZoom", "cameraRotation"} {
		if !slices.Contains(properties["property"].Enum, want) {
			t.Fatalf("animate cannot key %s", want)
		}
	}
	if !slices.Contains(properties["op"].Enum, "set_camera") {
		t.Fatal("the video tool does not offer set_camera")
	}
	if _, ok := NewStudioEditImageTool(StudioDeps{}).Definition().Parameters["operations"].Items.Properties["camera_zoom"]; ok {
		t.Fatal("the image tool offers a camera, which only video effect clips have")
	}
}

func TestStudioEditGuideExplainsAdjustmentClips(t *testing.T) {
	description := NewStudioEditVideoTool(StudioDeps{}).Definition().Description
	for _, want := range []string{"set_camera", "camera_zoom", "clear_camera", "add_effect_clip sem vfx_kind", "add_mask limita o ajuste", "cameraZoom", "motion_blur"} {
		if !strings.Contains(description, want) {
			t.Fatalf("the video guide does not explain %s", want)
		}
	}
}
