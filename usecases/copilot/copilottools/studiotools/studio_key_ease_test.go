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

func TestStudioEditPassesKeyEaseOperations(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "animate", "clip_id": "c-1", "property": "x", "keys": []interface{}{map[string]interface{}{"at_ms": 0.0, "value": 0.2}, map[string]interface{}{"at_ms": 500.0, "value": 0.4, "smooth": true}, map[string]interface{}{"at_ms": 900.0, "value": 0.8}}},
		map[string]interface{}{"op": "ease_keys", "clip_id": "c-1", "property": "x", "keys_ms": []interface{}{0.0, 900.0}, "side": "both"},
		map[string]interface{}{"op": "ease_keys", "clip_id": "c-1", "property": "x", "keys_ms": []interface{}{500.0}, "side": "out", "key_speed": 0.4, "influence": 60.0},
		map[string]interface{}{"op": "smooth_keys", "clip_id": "c-1", "property": "x", "smooth": false},
		map[string]interface{}{"op": "reverse_keys", "clip_id": "c-1"},
		map[string]interface{}{"op": "time_stretch_keys", "clip_id": "c-1", "percent": 150.0, "from_key": "last"},
		map[string]interface{}{"op": "rove_keys", "clip_id": "c-1", "property": "x"},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{
		`"smooth":true`, `"op":"ease_keys"`, `"keys_ms":[0,900]`, `"side":"both"`, `"side":"out"`, `"key_speed":0.4`, `"influence":60`,
		`"op":"smooth_keys"`, `"smooth":false`, `"op":"reverse_keys"`, `"op":"time_stretch_keys"`, `"percent":150`, `"from_key":"last"`, `"op":"rove_keys"`,
	} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
}

func TestStudioEditRefusesKeyEaseValuesOutOfRange(t *testing.T) {
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	for name, op := range map[string]map[string]interface{}{
		"no influence":       {"op": "ease_keys", "clip_id": "c-1", "side": "in", "influence": 0.0},
		"influence too high": {"op": "ease_keys", "clip_id": "c-1", "side": "in", "influence": 101.0},
		"zero stretch":       {"op": "time_stretch_keys", "clip_id": "c-1", "percent": 0.0},
		"negative instant":   {"op": "reverse_keys", "clip_id": "c-1", "keys_ms": []interface{}{-5.0}},
		"unknown side":       {"op": "ease_keys", "clip_id": "c-1", "side": "around"},
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

func TestStudioEditGuideExplainsInAndOutEase(t *testing.T) {
	description := NewStudioEditVideoTool(StudioDeps{}).Definition().Description
	for _, want := range []string{"ease_keys(", "smooth_keys(", "reverse_keys(", "time_stretch_keys(", "rove_keys(", "ease_in", "ease_out", "peak_speed", "alturas do quadro"} {
		if !strings.Contains(description, want) {
			t.Fatalf("the video guide does not explain %s", want)
		}
	}
	side, _ := reflect.TypeOf(studioVideoOperation{}).FieldByName("Side")
	if names := copilottools.EnumValues(side); !copilottools.Contains(names, "both") || !copilottools.Contains(names, "in") || !copilottools.Contains(names, "out") {
		t.Fatalf("side must offer in, out and both: %v", names)
	}
}
