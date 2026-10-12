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

func TestStudioEditPassesAnchorScaleAxesAndCurvedPaths(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	path := []interface{}{
		map[string]interface{}{"at_ms": 0.0, "x": 0.2, "y": 0.7, "easing": "easeOut", "spatial": "auto"},
		map[string]interface{}{"at_ms": 600.0, "x": 0.5, "y": 0.3, "spatial": "free", "in": map[string]interface{}{"x": -0.1, "y": 0.0}, "out": map[string]interface{}{"x": 0.15, "y": -0.05}},
		map[string]interface{}{"at_ms": 1200.0, "x": 0.8, "y": 0.6, "spatial": "linear"},
	}
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "set_anchor", "clip_id": "c-1", "anchor": "bottom-left", "keep_visual": false},
		map[string]interface{}{"op": "set_anchor", "clip_ids": []interface{}{"c-1", "c-2"}, "anchor_x": 0.0, "anchor_y": -0.5},
		map[string]interface{}{"op": "scale_axes", "clip_id": "c-1", "separate": true},
		map[string]interface{}{"op": "scale_axes", "clip_id": "c-2", "separate": false, "keep_axis": "y"},
		map[string]interface{}{"op": "animate", "clip_id": "c-1", "property": "scaleX", "keys": []interface{}{map[string]interface{}{"at_ms": 0.0, "value": 1.0}, map[string]interface{}{"at_ms": 900.0, "value": 1.6}}},
		map[string]interface{}{"op": "animate", "clip_id": "c-1", "property": "position", "keys": path},
		map[string]interface{}{"op": "set_path", "clip_id": "c-2", "curved": false},
		map[string]interface{}{"op": "update_clip", "clip_id": "c-1", "flip_x": true, "flip_y": false, "auto_orient": true},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{
		`"op":"set_anchor"`, `"anchor":"bottom-left"`, `"keep_visual":false`, `"anchor_x":0`, `"anchor_y":-0.5`,
		`"op":"scale_axes"`, `"separate":true`, `"separate":false`, `"keep_axis":"y"`, `"property":"scaleX"`,
		`"property":"position"`, `"x":0.2`, `"y":0.7`, `"spatial":"auto"`, `"spatial":"free"`, `"in":{"x":-0.1,"y":0}`, `"out":{"x":0.15,"y":-0.05}`, `"spatial":"linear"`,
		`"op":"set_path"`, `"curved":false`, `"flip_x":true`, `"flip_y":false`, `"auto_orient":true`,
	} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
}

func TestStudioMotionSchemaFollowsTheDocument(t *testing.T) {
	operation := reflect.TypeOf(studioVideoOperation{})
	property, _ := operation.FieldByName("Property")
	allowed := strings.Split(property.Tag.Get("enum"), ",")
	lanes := reflect.TypeOf(studio.ClipKeyframes{})
	for _, lane := range []string{"ScaleX", "ScaleY", "Position"} {
		field, _ := lanes.FieldByName(lane)
		if name := copilottools.JSONName(field); !copilottools.Contains(allowed, name) {
			t.Fatalf("animate cannot reach the %s lane", name)
		}
	}
	spatial, _ := reflect.TypeOf(studioKeyArgs{}).FieldByName("Spatial")
	if spatial.Tag.Get("enum") != strings.Join([]string{studio.SpatialLinear, studio.SpatialAuto, studio.SpatialSmooth, studio.SpatialFree}, ",") {
		t.Fatalf("the spatial modes drifted from the document: %q", spatial.Tag.Get("enum"))
	}
	anchor, _ := operation.FieldByName("Anchor")
	if names := copilottools.EnumValues(anchor); len(names) != 9 || !copilottools.Contains(names, "center") || !copilottools.Contains(names, "top-left") || !copilottools.Contains(names, "bottom-right") {
		t.Fatalf("set_anchor must offer the nine anchor points: %v", names)
	}
}

func TestStudioEditGuideExplainsTheMotionOperations(t *testing.T) {
	description := NewStudioEditVideoTool(StudioDeps{}).Definition().Description
	for _, want := range []string{"set_anchor(", "scale_axes(", "set_path(", "auto_orient", "flip_x", "spatial"} {
		if !strings.Contains(description, want) {
			t.Fatalf("the video guide does not explain %s", want)
		}
	}
}
