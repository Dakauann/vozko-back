package studiotools

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/studio"
)

func TestStudioEditImagePassesStillEffects(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditImageTool(studioDeps(studio.KindImage, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "add_vfx", "ref": "g", "layer_id": "photo", "vfx_kind": "grain", "vfx_seed": 0.0, "vfx_params": map[string]interface{}{"amount": 0.0, "chroma": 0.4}},
		map[string]interface{}{"op": "add_vfx", "artboard_id": "a1", "vfx_kind": "lightLeak", "vfx_params": map[string]interface{}{"color": "#ffaa00", "x": 0.9}},
		map[string]interface{}{"op": "update_vfx", "layer_id": "photo", "vfx_id": "@g", "vfx_hidden": true},
		map[string]interface{}{"op": "move_vfx", "layer_id": "photo", "vfx_id": "@g", "to_index": 0.0},
		map[string]interface{}{"op": "remove_vfx", "layer_id": "photo", "vfx_id": "@g"},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioImage, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{`"op":"add_vfx"`, `"vfx_kind":"grain"`, `"vfx_seed":0`, `"amount":0`, `"chroma":0.4`, `"artboard_id":"a1"`, `"color":"#ffaa00"`, `"vfx_hidden":true`, `"to_index":0`, `"op":"move_vfx"`, `"op":"remove_vfx"`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
}

func TestTheStillVfxSchemaFollowsTheDocument(t *testing.T) {
	fields := reflect.TypeOf(studioStillVfxArgs{})
	kinds, _ := fields.FieldByName("VfxKind")
	if kinds.Tag.Get("enum") != sortedJoin(studio.StillVfxKinds()) {
		t.Fatalf("vfx_kind drifted: %q", kinds.Tag.Get("enum"))
	}
	wanted := map[string]bool{"color": true, "color2": true, "color3": true, "color4": true}
	for _, kind := range studio.StillVfxKinds() {
		for _, name := range studio.StillVfxParams(kind) {
			wanted[name] = true
		}
	}
	params := reflect.TypeOf(studioStillVfxParamsArgs{})
	names := []string{}
	for i := 0; i < params.NumField(); i++ {
		names = append(names, strings.Split(params.Field(i).Tag.Get("json"), ",")[0])
	}
	expected := []string{}
	for name := range wanted {
		expected = append(expected, name)
	}
	if sortedJoin(names) != sortedJoin(expected) {
		t.Fatalf("vfx_params drifted: %v", names)
	}
}

func TestTheImageToolListsTheStillEffectOperations(t *testing.T) {
	ops := strings.Split(reflect.TypeOf(studioImageOperation{}).Field(0).Tag.Get("enum"), ",")
	for _, want := range []string{"add_vfx", "update_vfx", "move_vfx", "remove_vfx"} {
		found := false
		for _, op := range ops {
			found = found || op == want
		}
		if !found {
			t.Fatalf("studio_edit_image does not offer %s", want)
		}
	}
	if !strings.Contains(studioImageEditGuide, "add_vfx(") {
		t.Fatal("the image guide does not explain the still effects")
	}
}

func TestTheStillVfxArgumentsKeepToWhatAStillCanShow(t *testing.T) {
	cases := map[string]struct {
		op   studioImageOperation
		want string
	}{
		"a corner pin corner": {op: studioImageOperation{Op: "add_vfx", studioStillVfxArgs: studioStillVfxArgs{VfxKind: "cornerPin", VfxParams: &studioStillVfxParamsArgs{TopLeftX: floatArg(0.2)}}}},
		"a wave height out of range": {
			op:   studioImageOperation{Op: "add_vfx", studioStillVfxArgs: studioStillVfxArgs{VfxKind: "waveWarp", VfxParams: &studioStillVfxParamsArgs{Height: floatArg(500)}}},
			want: "vfx_params.height de waveWarp vai de 0 a 200",
		},
		"a corner on a mosaic": {
			op:   studioImageOperation{Op: "add_vfx", studioStillVfxArgs: studioStillVfxArgs{VfxKind: "mosaic", VfxParams: &studioStillVfxParamsArgs{TopLeftX: floatArg(0.2)}}},
			want: "vfx_params.topLeftX não vale para mosaic",
		},
	}
	for name, c := range cases {
		if got := c.op.argsIssue(); got != c.want {
			t.Errorf("%s: got %q, want %q", name, got, c.want)
		}
	}
}
