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

func TestStudioEditPassesGroupOperations(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "group_clips", "ref": "g", "clip_ids": []interface{}{"c-1", "c-2"}, "name": "Abertura"},
		map[string]interface{}{"op": "update_clip", "group_id": "@g", "clip_id": "c-1", "opacity": 0.5},
		map[string]interface{}{"op": "ungroup_clips", "clip_id": "@g"},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{
		`"op":"group_clips"`, `"ref":"g"`, `"clip_ids":["c-1","c-2"]`, `"name":"Abertura"`,
		`"op":"update_clip"`, `"group_id":"@g"`, `"clip_id":"c-1"`,
		`"op":"ungroup_clips"`, `"clip_id":"@g"`,
	} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
}

func TestStudioGroupSchemaOffersTheEditorFields(t *testing.T) {
	ops := opEnum(studioVideoOperation{})
	for _, name := range []string{"group_clips", "ungroup_clips"} {
		if !copilottools.Contains(ops, name) {
			t.Fatalf("the video tool does not offer %s", name)
		}
	}
	f, ok := reflect.TypeOf(studioVideoOperation{}).FieldByName("GroupID")
	if !ok || f.Type.Kind() != reflect.String {
		t.Fatal("GroupID is missing or has the wrong shape")
	}
	properties := NewStudioEditVideoTool(StudioDeps{}).Definition().Parameters["operations"].Items.Properties
	if properties["group_id"].Type != "string" {
		t.Fatalf("group_id must be a string, got %q", properties["group_id"].Type)
	}
}

func TestStudioEditGuideExplainsGroups(t *testing.T) {
	description := NewStudioEditVideoTool(StudioDeps{}).Definition().Description
	for _, want := range []string{"group_clips(", "ungroup_clips(", "group_id", "nulo", "trim", "corte", "bake_ducking"} {
		if !strings.Contains(description, want) {
			t.Fatalf("the video guide does not explain %s", want)
		}
	}
}
