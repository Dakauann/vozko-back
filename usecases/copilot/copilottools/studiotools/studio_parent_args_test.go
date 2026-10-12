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

func TestStudioEditPassesParentOperations(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "add_null", "ref": "rig", "at_ms": 0.0, "duration_ms": 3000.0, "x": 0.4, "y": 0.6, "track_id": "t-1"},
		map[string]interface{}{"op": "parent_clip", "clip_ids": []interface{}{"c-1", "c-2"}, "parent_id": "@rig", "keep_position": false, "at_ms": 1200.0},
		map[string]interface{}{"op": "parent_clip", "clip_id": "c-2", "unparent": true, "keep_position": true},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{
		`"op":"add_null"`, `"ref":"rig"`, `"duration_ms":3000`, `"x":0.4`, `"y":0.6`, `"track_id":"t-1"`,
		`"op":"parent_clip"`, `"clip_ids":["c-1","c-2"]`, `"parent_id":"@rig"`, `"keep_position":false`, `"at_ms":1200`,
		`"clip_id":"c-2"`, `"unparent":true`, `"keep_position":true`,
	} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
}

func TestStudioParentSchemaOffersTheEditorFields(t *testing.T) {
	ops := opEnum(studioVideoOperation{})
	for _, name := range []string{"parent_clip", "add_null"} {
		if !copilottools.Contains(ops, name) {
			t.Fatalf("the video tool does not offer %s", name)
		}
	}
	operation := reflect.TypeOf(studioVideoOperation{})
	for field, want := range map[string]reflect.Kind{"ParentID": reflect.String, "Unparent": reflect.Ptr, "KeepPosition": reflect.Ptr} {
		f, ok := operation.FieldByName(field)
		if !ok || f.Type.Kind() != want {
			t.Fatalf("%s is missing or has the wrong shape", field)
		}
	}
	properties := NewStudioEditVideoTool(StudioDeps{}).Definition().Parameters["operations"].Items.Properties
	for name, kind := range map[string]string{"parent_id": "string", "unparent": "boolean", "keep_position": "boolean"} {
		if properties[name].Type != kind {
			t.Fatalf("%s must be a %s, got %q", name, kind, properties[name].Type)
		}
	}
}

func TestStudioEditGuideExplainsParenting(t *testing.T) {
	description := NewStudioEditVideoTool(StudioDeps{}).Definition().Description
	for _, want := range []string{"parent_clip(", "add_null(", "keep_position", "unparent", "parent", "nulo", "âncora do pai", "opacidade", "círculo"} {
		if !strings.Contains(description, want) {
			t.Fatalf("the video guide does not explain %s", want)
		}
	}
}
