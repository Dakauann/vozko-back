package studiotools

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/studio"
	"vozko/usecases/copilot/copilottools"
)

func TestStudioEditPassesTrackingOperations(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "track_point", "ref": "eye", "clip_id": "c-1", "point": map[string]interface{}{"x": 0.42, "y": 0.31}, "at_ms": 1200.0, "track_size": 0.08, "from_ms": 0.0, "to_ms": 3000.0, "track_direction": "forward", "keep_partial": true, "name": "Olho"},
		map[string]interface{}{"op": "attach_to_track", "clip_ids": []interface{}{"c-2"}, "parent_id": "c-1", "tracker_id": "@eye", "attach_mode": "keys", "keep_position": false},
		map[string]interface{}{"op": "stabilize_clip", "clip_id": "c-1", "tracker_id": "@eye", "second_tracker_id": "pt-2", "stabilize_rotation": true, "stabilize_scale": true, "smooth_ms": 800.0, "zoom": 1.3},
		map[string]interface{}{"op": "stabilize_clip", "clip_id": "c-1", "unstabilize": true},
		map[string]interface{}{"op": "remove_tracker", "clip_id": "c-1", "tracker_id": "pt-2"},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{
		`"op":"track_point"`, `"point":{"x":0.42,"y":0.31}`, `"track_size":0.08`, `"from_ms":0`, `"to_ms":3000`, `"track_direction":"forward"`, `"keep_partial":true`, `"name":"Olho"`,
		`"op":"attach_to_track"`, `"tracker_id":"@eye"`, `"attach_mode":"keys"`, `"keep_position":false`,
		`"op":"stabilize_clip"`, `"second_tracker_id":"pt-2"`, `"stabilize_rotation":true`, `"stabilize_scale":true`, `"smooth_ms":800`, `"zoom":1.3`, `"unstabilize":true`,
		`"op":"remove_tracker"`,
	} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
}

func TestStudioTrackSchemaOffersTheEditorFields(t *testing.T) {
	ops := opEnum(studioVideoOperation{})
	for _, name := range []string{"track_point", "attach_to_track", "stabilize_clip", "remove_tracker"} {
		if !copilottools.Contains(ops, name) {
			t.Fatalf("the video tool does not offer %s", name)
		}
	}
	operation := reflect.TypeOf(studioVideoOperation{})
	for field, want := range map[string]reflect.Kind{"Point": reflect.Ptr, "TrackerID": reflect.String, "AttachMode": reflect.String, "SmoothMS": reflect.Ptr, "Zoom": reflect.Ptr} {
		f, ok := operation.FieldByName(field)
		if !ok || f.Type.Kind() != want {
			t.Fatalf("%s is missing or has the wrong shape", field)
		}
	}
	properties := NewStudioEditVideoTool(StudioDeps{}).Definition().Parameters["operations"].Items.Properties
	for name, kind := range map[string]string{"point": "object", "track_size": "number", "from_ms": "integer", "to_ms": "integer", "track_direction": "string", "tracker_id": "string", "second_tracker_id": "string", "keep_partial": "boolean", "attach_mode": "string", "stabilize_rotation": "boolean", "stabilize_scale": "boolean", "smooth_ms": "integer", "zoom": "number", "unstabilize": "boolean"} {
		if properties[name].Type != kind {
			t.Fatalf("%s must be a %s, got %q", name, kind, properties[name].Type)
		}
	}
}

func TestStudioEditGuideExplainsTracking(t *testing.T) {
	description := NewStudioEditVideoTool(StudioDeps{}).Definition().Description
	for _, want := range []string{"track_point(", "attach_to_track(", "stabilize_clip(", "remove_tracker(", "keep_partial", "0.6", "smooth_ms", "parent_tracker", "zoom"} {
		if !strings.Contains(description, want) {
			t.Fatalf("the video guide does not explain %s", want)
		}
	}
}
