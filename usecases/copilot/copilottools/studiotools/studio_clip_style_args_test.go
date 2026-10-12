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

func TestStudioEditPassesClipStyles(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{
			"op":      "update_clip",
			"clip_id": "c-video",
			"strokes": []interface{}{map[string]interface{}{"color": "#ffffff", "width": 8.0, "align": "outside"}},
			"effects": []interface{}{map[string]interface{}{"type": "outer-glow", "color": "#ffd400", "blur": 24.0}},
		},
		map[string]interface{}{"op": "update_clip", "clip_id": "c-title", "fills": []interface{}{map[string]interface{}{"color": "#ffffff"}}},
		map[string]interface{}{"op": "animate_effect", "clip_id": "c-video", "effect_id": "fx-1", "effect_property": "opacity", "keys": []interface{}{map[string]interface{}{"at_ms": 0.0, "value": 0.2}, map[string]interface{}{"at_ms": 600.0, "value": 1.0, "easing": "easeInOut"}}},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{`"strokes":[`, `"align":"outside"`, `"effects":[`, `"type":"outer-glow"`, `"fills":[`, `"op":"animate_effect"`, `"effect_id":"fx-1"`, `"effect_property":"opacity"`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
}

func TestStudioEditRefusesClipStyleArgumentsThatCannotWork(t *testing.T) {
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	for name, op := range map[string]map[string]interface{}{
		"animate_effect without a property": {"op": "animate_effect", "clip_id": "c-1", "keys": []interface{}{map[string]interface{}{"at_ms": 0.0, "value": 1.0}}},
		"an unknown effect property":        {"op": "animate_effect", "clip_id": "c-1", "effect_property": "color", "keys": []interface{}{map[string]interface{}{"at_ms": 0.0, "value": 1.0}}},
		"an effect key without a value":     {"op": "animate_effect", "clip_id": "c-1", "effect_property": "blur", "keys": []interface{}{map[string]interface{}{"at_ms": 0.0}}},
		"an effect id on another operation": {"op": "update_clip", "clip_id": "c-1", "effect_id": "fx-1"},
	} {
		editor := &fakeEditor{}
		res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), map[string]interface{}{"operations": []interface{}{op}})
		if res.Status != copilot.StatusError {
			t.Fatalf("%s must be refused, got %+v", name, res)
		}
		if _, sent := editor.sent(copilot.ScreenEdit); sent {
			t.Fatalf("%s reached the editor", name)
		}
	}
}

func TestVideoOperationFieldsHaveOneJSONNameEach(t *testing.T) {
	seen := map[string]string{}
	for _, f := range reflect.VisibleFields(reflect.TypeOf(studioVideoOperation{})) {
		if f.Anonymous {
			continue
		}
		name := copilottools.JSONName(f)
		if owner, taken := seen[name]; taken {
			t.Fatalf("%s is the JSON name of both %s and %s", name, owner, f.Name)
		}
		seen[name] = f.Name
	}
	for _, want := range []string{"fills", "strokes", "effects", "effect_id", "effect_property"} {
		if _, ok := seen[want]; !ok {
			t.Fatalf("video operations lost %s", want)
		}
	}
}

func TestStudioEditGuideExplainsClipStyles(t *testing.T) {
	description := NewStudioEditVideoTool(StudioDeps{}).Definition().Description
	for _, want := range []string{"animate_effect(", "drop-shadow e outer-glow", "silhueta"} {
		if !strings.Contains(description, want) {
			t.Fatalf("the video guide does not explain %s", want)
		}
	}
}
