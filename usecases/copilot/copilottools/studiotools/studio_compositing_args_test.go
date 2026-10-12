package studiotools

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/studio"
)

func TestStudioEditPassesLumaKeyBlendAndMattes(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "set_luma_key", "clip_id": "c-1", "luma_mode": "light", "luma_threshold": 0.0, "luma_softness": 0.2, "at_ms": 0.0},
		map[string]interface{}{"op": "animate_luma", "clip_id": "c-1", "luma_property": "threshold", "keys": []interface{}{map[string]interface{}{"at_ms": 0.0, "value": 0.3}}},
		map[string]interface{}{"op": "remove_luma_key", "clip_id": "c-1"},
		map[string]interface{}{"op": "update_clip", "clip_id": "c-2", "blend_mode": "add", "matte_channel": "luma", "matte_invert": false},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{`"op":"set_luma_key"`, `"luma_mode":"light"`, `"luma_threshold":0`, `"luma_softness":0.2`, `"luma_property":"threshold"`, `"op":"remove_luma_key"`, `"blend_mode":"add"`, `"matte_channel":"luma"`, `"matte_invert":false`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
}

func TestTheCompositingSchemaFollowsTheDocument(t *testing.T) {
	modes := strings.Join(studio.BlendModes(), ",")
	for _, args := range []any{studioStyleArgs{}, studioPatternArgs{}, studioPaintArgs{}, studioEffectArgs{}} {
		field, ok := reflect.TypeOf(args).FieldByName("BlendMode")
		if !ok || field.Tag.Get("enum") != modes {
			t.Fatalf("%T blend_mode drifted: %q", args, field.Tag.Get("enum"))
		}
	}
	luma := reflect.TypeOf(studioLumaArgs{})
	if mode, _ := luma.FieldByName("LumaMode"); mode.Tag.Get("enum") != strings.Join(studio.LumaKeyModes(), ",") {
		t.Fatalf("luma_mode drifted: %q", mode.Tag.Get("enum"))
	}
	if channel, _ := reflect.TypeOf(studioMaskArgs{}).FieldByName("MatteChannel"); channel.Tag.Get("enum") != strings.Join(studio.MatteChannels(), ",") {
		t.Fatalf("matte_channel drifted: %q", channel.Tag.Get("enum"))
	}
}
