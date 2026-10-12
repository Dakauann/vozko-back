package studiotools

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/studio"
)

func TestStudioEditPassesMotionBlurOperations(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "set_canvas", "shutter_angle": 270.0, "shutter_phase": -135.0},
		map[string]interface{}{"op": "update_clip", "clip_ids": []interface{}{"c-1", "c-2"}, "motion_blur": true},
		map[string]interface{}{"op": "update_clip", "clip_id": "c-3", "motion_blur": false},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{`"op":"set_canvas"`, `"shutter_angle":270`, `"shutter_phase":-135`, `"clip_ids":["c-1","c-2"]`, `"motion_blur":true`, `"clip_id":"c-3"`, `"motion_blur":false`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
}

func TestStudioEditRefusesAShutterOutsideItsRange(t *testing.T) {
	for _, op := range []map[string]interface{}{
		{"op": "set_canvas", "shutter_angle": studio.MaxShutterAngle + 1},
		{"op": "set_canvas", "shutter_angle": -1.0},
		{"op": "set_canvas", "shutter_phase": studio.MinShutterPhase - 1},
		{"op": "set_canvas", "shutter_phase": studio.MaxShutterPhase + 1},
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

func TestStudioMotionBlurSchemaOffersTheEditorFields(t *testing.T) {
	properties := NewStudioEditVideoTool(StudioDeps{}).Definition().Parameters["operations"].Items.Properties
	for name, kind := range map[string]string{"motion_blur": "boolean", "shutter_angle": "number", "shutter_phase": "number"} {
		if properties[name].Type != kind {
			t.Fatalf("%s must be a %s, got %q", name, kind, properties[name].Type)
		}
	}
	if _, ok := NewStudioEditImageTool(StudioDeps{}).Definition().Parameters["operations"].Items.Properties["motion_blur"]; ok {
		t.Fatal("the image tool offers motion blur, which only video clips have")
	}
}

func TestStudioEditGuideExplainsMotionBlur(t *testing.T) {
	description := NewStudioEditVideoTool(StudioDeps{}).Definition().Description
	for _, want := range []string{"motion_blur", "shutter_angle", "shutter_phase", "Desfoque de movimento", "180", "-90"} {
		if !strings.Contains(description, want) {
			t.Fatalf("the video guide does not explain %s", want)
		}
	}
}
