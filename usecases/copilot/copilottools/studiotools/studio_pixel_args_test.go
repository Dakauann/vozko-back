package studiotools

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/studio"
	"vozko/usecases/copilot/copilottools"
)

func TestStudioImageEditCarriesPixelOperations(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditImageTool(studioDeps(studio.KindImage, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "add_paint_layer", "ref": "tinta", "name": "Tinta"},
		map[string]interface{}{
			"op": "paint_area", "layer_id": "@tinta", "action": "fill", "paint_color": "#e4572e", "paint_opacity": 0.0,
			"area": map[string]interface{}{"shape": "rect", "x": 0.5, "y": 0.4, "w": 0.3, "h": 0.2, "feather": 0.0, "invert": false},
		},
		map[string]interface{}{
			"op": "paint_area", "layer_id": "photo", "action": "hide",
			"area": map[string]interface{}{"shape": "polygon", "points": []interface{}{
				map[string]interface{}{"x": 0.1, "y": 0.1}, map[string]interface{}{"x": 0.9, "y": 0.1}, map[string]interface{}{"x": 0.5, "y": 0.0},
			}},
		},
		map[string]interface{}{"op": "crop_to_area", "layer_id": "photo", "area": map[string]interface{}{"shape": "ellipse", "x": 0.5, "y": 0.5, "w": 0.6, "h": 0.6}},
		map[string]interface{}{"op": "copy_area_to_layer", "layer_id": "photo", "area": map[string]interface{}{"shape": "rect", "x": 0.25, "y": 0.25, "w": 0.5, "h": 0.5}},
		map[string]interface{}{"op": "update_layer", "layer_id": "photo", "clear": []interface{}{"mask"}},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioImage, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{
		`"op":"add_paint_layer"`, `"name":"Tinta"`,
		`"op":"paint_area"`, `"action":"fill"`, `"paint_color":"#e4572e"`, `"paint_opacity":0`,
		`"area":{"shape":"rect","x":0.5,"y":0.4,"w":0.3,"h":0.2,"feather":0,"invert":false}`,
		`"action":"hide"`, `"area":{"shape":"polygon","points":[{"x":0.1,"y":0.1},{"x":0.9,"y":0.1},{"x":0.5,"y":0}]}`,
		`"op":"crop_to_area"`, `"area":{"shape":"ellipse","x":0.5,"y":0.5,"w":0.6,"h":0.6}`,
		`"op":"copy_area_to_layer"`, `"clear":["mask"]`,
	} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
	item := edit.Definition().Parameters["operations"].Items
	if action := item.Properties["action"]; len(action.Enum) != 4 {
		t.Fatalf("action schema = %+v", action)
	}
	area := item.Properties["area"]
	if area.Items == nil || len(area.Items.Properties["shape"].Enum) != 3 {
		t.Fatalf("area schema = %+v", area)
	}
	if points := area.Items.Properties["points"]; points.Type != "array" || points.Items == nil || len(points.Items.Required) != 2 || points.Items.Properties["x"].Type == "" || points.Items.Properties["y"].Type == "" {
		t.Fatalf("area points schema = %+v", points)
	}
	if !copilottools.Contains(item.Properties["clear"].Items.Enum, "mask") {
		t.Fatal("clear does not offer mask")
	}
	for _, op := range []string{"add_paint_layer", "paint_area", "crop_to_area", "copy_area_to_layer"} {
		if !copilottools.Contains(item.Properties["op"].Enum, op) {
			t.Fatalf("op does not offer %s", op)
		}
	}
	if video := NewStudioEditVideoTool(StudioDeps{}).Definition().Parameters["operations"].Items; video.Properties["area"].Type != "" {
		t.Fatal("the video editor must not offer pixel areas")
	}
}
