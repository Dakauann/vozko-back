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

func TestStudioEditPassesLineAnimation(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "set_line", "clip_id": "c-1", "wave_mode": "width", "wave_amount": 12.0, "wave_speed": 0.0, "wiggle_seed": 0.0, "trim_sequential": false, "taper_end_ease": -0.5, "at_ms": 300.0},
		map[string]interface{}{"op": "animate", "clip_id": "c-1", "property": "trimEnd", "keys": []interface{}{map[string]interface{}{"at_ms": 0.0, "value": 0.0}, map[string]interface{}{"at_ms": 800.0, "value": 1.0, "easing": "easeOut"}}},
		map[string]interface{}{"op": "set_line", "clip_id": "c-1", "line_clear": []interface{}{"wiggle", "taper"}},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{`"op":"set_line"`, `"wave_mode":"width"`, `"wave_amount":12`, `"wave_speed":0`, `"wiggle_seed":0`, `"trim_sequential":false`, `"taper_end_ease":-0.5`, `"property":"trimEnd"`, `"line_clear":["wiggle","taper"]`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
}

func TestStudioLineSchemaFollowsTheDocument(t *testing.T) {
	operation := reflect.TypeOf(studioVideoOperation{})
	property, _ := operation.FieldByName("Property")
	allowed := strings.Split(property.Tag.Get("enum"), ",")
	lanes := reflect.TypeOf(studio.LineKeyframes{})
	for i := 0; i < lanes.NumField(); i++ {
		name := strings.Split(lanes.Field(i).Tag.Get("json"), ",")[0]
		if !copilottools.Contains(allowed, name) {
			t.Fatalf("animate cannot reach the line property %s", name)
		}
	}
	mode, _ := reflect.TypeOf(studioLineArgs{}).FieldByName("WaveMode")
	if mode.Tag.Get("enum") != strings.Join([]string{studio.WaveBend, studio.WaveWidth}, ",") {
		t.Fatalf("the wave modes drifted from the document: %q", mode.Tag.Get("enum"))
	}
}
