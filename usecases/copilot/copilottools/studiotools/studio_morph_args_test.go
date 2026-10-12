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

func TestStudioEditPassesPathKeys(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "animate", "clip_id": "c-1", "property": "path", "keys": []interface{}{
			map[string]interface{}{"at_ms": 0.0, "path": "M0 0 L1 0 L1 1 L0 1 Z", "easing": "easeInOut"},
			map[string]interface{}{"at_ms": 800.0, "path": "M0.5 0 L1 1 L0 1 Z", "first_vertex": 2.0},
		}},
		map[string]interface{}{"op": "animate_mask", "clip_id": "c-2", "mask_id": "m-1", "mask_property": "path", "keys": []interface{}{
			map[string]interface{}{"at_ms": 0.0, "path": "M0.5 0 L1 0.5 L0.5 1 L0 0.5 Z"},
		}},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{`"property":"path"`, `"path":"M0 0 L1 0 L1 1 L0 1 Z"`, `"first_vertex":2`, `"mask_property":"path"`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
}

func TestStudioEditRefusesPathKeysThatCannotDraw(t *testing.T) {
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	square := "M0 0 L1 0 L1 1 L0 1 Z"
	for name, op := range map[string]map[string]interface{}{
		"a key without a path":         {"op": "animate", "clip_id": "c-1", "property": "path", "keys": []interface{}{map[string]interface{}{"at_ms": 0.0, "value": 1.0}}},
		"a path that does not parse":   {"op": "animate", "clip_id": "c-1", "property": "path", "keys": []interface{}{map[string]interface{}{"at_ms": 0.0, "path": "Q0"}}},
		"a negative first vertex":      {"op": "animate", "clip_id": "c-1", "property": "path", "keys": []interface{}{map[string]interface{}{"at_ms": 0.0, "path": square, "first_vertex": -1.0}}},
		"a path on an opacity key":     {"op": "animate", "clip_id": "c-1", "property": "opacity", "keys": []interface{}{map[string]interface{}{"at_ms": 0.0, "value": 1.0, "path": square}}},
		"a first vertex on a mask x":   {"op": "animate_mask", "clip_id": "c-1", "mask_id": "m", "mask_property": "x", "keys": []interface{}{map[string]interface{}{"at_ms": 0.0, "value": 0.5, "first_vertex": 1.0}}},
		"a mask path key with no path": {"op": "animate_mask", "clip_id": "c-1", "mask_id": "m", "mask_property": "path", "keys": []interface{}{map[string]interface{}{"at_ms": 0.0}}},
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

func TestStudioEditGuideExplainsMorphs(t *testing.T) {
	description := NewStudioEditVideoTool(StudioDeps{}).Definition().Description
	for _, want := range []string{"property path", "mask_property path", "first_vertex", "casa os pontos"} {
		if !strings.Contains(description, want) {
			t.Fatalf("the video guide does not explain %s", want)
		}
	}
	property, _ := reflect.TypeOf(studioVideoOperation{}).FieldByName("Property")
	mask, _ := reflect.TypeOf(studioVideoOperation{}).FieldByName("MaskProperty")
	if !copilottools.Contains(copilottools.EnumValues(property), "path") || !copilottools.Contains(copilottools.EnumValues(mask), "path") {
		t.Fatal("animate and animate_mask must offer path")
	}
}
