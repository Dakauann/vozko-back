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

var textAnimatorOps = []string{"add_text_animator", "update_text_animator", "animate_text_animator", "move_text_animator", "remove_text_animator"}

func TestStudioEditPassesTextAnimatorOperations(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "add_text_animator", "clip_id": "c-1", "animator_preset": "cascadeIn", "selector_seed": 4.0},
		map[string]interface{}{
			"op": "add_text_animator", "clip_id": "c-1", "unit": "word",
			"animator_properties": map[string]interface{}{"y": 40.0, "opacity": 0.0, "fill": "#ffcc00"},
			"range_units":         "index", "range_start": 0.0, "range_end": 2.0, "range_offset": 1.0, "range_order": "edgesIn", "range_shape": "round", "ease_high": 50.0, "ease_low": -20.0, "selector_amount": -60.0,
		},
		map[string]interface{}{"op": "update_text_animator", "clip_id": "c-1", "animator_id": "anim-1", "selector_kind": "wiggly", "wiggly_rate": 3.0, "wiggly_loop_ms": 2000.0, "wiggly_correlation": 25.0, "remove_properties": []interface{}{"fill"}, "at_ms": 400.0},
		map[string]interface{}{"op": "animate_text_animator", "clip_id": "c-1", "animator_id": "anim-1", "selector_property": "offset", "keys": []interface{}{map[string]interface{}{"at_ms": 0.0, "value": -25.0}, map[string]interface{}{"at_ms": 900.0, "value": 100.0, "easing": "easeOut"}}},
		map[string]interface{}{"op": "move_text_animator", "clip_id": "c-1", "animator_id": "anim-1", "to_index": 0.0},
		map[string]interface{}{"op": "remove_text_animator", "clip_id": "c-1", "animator_id": "anim-1"},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{
		`"animator_preset":"cascadeIn"`, `"selector_seed":4`, `"unit":"word"`, `"y":40`, `"opacity":0`, `"fill":"#ffcc00"`,
		`"range_units":"index"`, `"range_end":2`, `"range_offset":1`, `"range_order":"edgesIn"`, `"range_shape":"round"`, `"ease_high":50`, `"ease_low":-20`, `"selector_amount":-60`,
		`"selector_kind":"wiggly"`, `"wiggly_rate":3`, `"wiggly_loop_ms":2000`, `"wiggly_correlation":25`, `"remove_properties":["fill"]`,
		`"selector_property":"offset"`, `"op":"move_text_animator"`, `"op":"remove_text_animator"`, `"animator_id":"anim-1"`,
	} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
}

func TestStudioTextAnimatorSchemaMatchesTheDocument(t *testing.T) {
	ops := opEnum(studioVideoOperation{})
	for _, name := range textAnimatorOps {
		if !copilottools.Contains(ops, name) {
			t.Fatalf("the video tool does not offer %s", name)
		}
	}
	fields := reflect.TypeOf(studioTextAnimatorArgs{})
	for field, want := range map[string][]string{
		"SelectorKind": studio.SelectorKinds(),
		"RangeOrder":   studio.RangeOrders(),
		"RangeShape":   studio.RangeShapes(),
		"RangeUnits":   studio.RangeUnits(),
	} {
		f, _ := fields.FieldByName(field)
		if f.Tag.Get("enum") != strings.Join(want, ",") {
			t.Fatalf("%s drifted from the document: %q", field, f.Tag.Get("enum"))
		}
	}
	properties := NewStudioEditVideoTool(StudioDeps{}).Definition().Parameters["operations"].Items.Properties
	for name, kind := range map[string]string{"animator_id": "string", "animator_properties": "object", "remove_properties": "array", "range_start": "number", "selector_seed": "integer", "wiggly_loop_ms": "integer", "selector_property": "string"} {
		if properties[name].Type != kind {
			t.Fatalf("%s must be a %s, got %q", name, kind, properties[name].Type)
		}
	}
}

func TestStudioEditGuideExplainsTextAnimators(t *testing.T) {
	description := NewStudioEditVideoTool(StudioDeps{}).Definition().Description
	for _, want := range append([]string{"animator_properties", "range_offset", "rampUp", "centerOut", "edgesIn", "wiggly_loop_ms", "ease_high", "colorSweep", "text_animators"}, textAnimatorOps...) {
		if !strings.Contains(description, want) {
			t.Fatalf("the video guide does not explain %s", want)
		}
	}
}
