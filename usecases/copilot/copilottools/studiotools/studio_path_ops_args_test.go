package studiotools

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/studio"
	"vozko/domain/tools"
)

func TestStudioEditPassesVideoPathOpOperations(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "set_path_ops", "clip_id": "c-1", "path_ops": []interface{}{
			map[string]interface{}{"kind": "offset", "amount": 12.0, "copies": 3.0, "copy_offset": 8.0, "join": "round"},
			map[string]interface{}{"kind": "merge", "mode": "subtract", "operand": "ellipse", "x": 0.5, "y": 0.5, "w": 0.3, "h": 0.3},
		}},
		map[string]interface{}{"op": "animate_path_op", "clip_id": "c-1", "path_op_id": "op-1", "path_op_property": "amount", "keys": []interface{}{
			map[string]interface{}{"at_ms": 0.0, "value": 0.0}, map[string]interface{}{"at_ms": 800.0, "value": 24.0},
		}},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{`"op":"set_path_ops"`, `"kind":"offset"`, `"copies":3`, `"copy_offset":8`, `"kind":"merge"`, `"operand":"ellipse"`, `"op":"animate_path_op"`, `"path_op_id":"op-1"`, `"path_op_property":"amount"`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
}

func TestStudioEditPassesImagePathOpOperations(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditImageTool(studioDeps(studio.KindImage, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "set_path_ops", "layer_ids": []interface{}{"l-1"}, "path_ops": []interface{}{
			map[string]interface{}{"kind": "zigZag", "size": 6.0, "ridges": 400.0, "points": "smooth"},
		}},
		map[string]interface{}{"op": "combine_shapes", "layer_ids": []interface{}{"l-1", "l-2"}, "mode": "subtract", "live": true},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioImage, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{`"op":"set_path_ops"`, `"kind":"zigZag"`, `"ridges":400`, `"points":"smooth"`, `"op":"combine_shapes"`, `"live":true`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
}

func TestStudioEditRefusesBrokenPathOps(t *testing.T) {
	item := func(fields map[string]interface{}) map[string]interface{} {
		return map[string]interface{}{"op": "set_path_ops", "clip_id": "c-1", "path_ops": []interface{}{fields}}
	}
	for name, op := range map[string]map[string]interface{}{
		"an amount out of range":       item(map[string]interface{}{"kind": "offset", "amount": studio.MaxPathOpOffset + 1, "join": "round"}),
		"no copies":                    item(map[string]interface{}{"kind": "offset", "copies": 0.0}),
		"negative ridges":              item(map[string]interface{}{"kind": "zigZag", "ridges": -1.0}),
		"an unknown kind":              item(map[string]interface{}{"kind": "spiral"}),
		"no kind":                      item(map[string]interface{}{"amount": 3.0}),
		"an unknown join":              item(map[string]interface{}{"kind": "offset", "join": "sharp"}),
		"unknown points":               item(map[string]interface{}{"kind": "zigZag", "points": "wavy"}),
		"an unknown merge mode":        item(map[string]interface{}{"kind": "merge", "mode": "flatten"}),
		"an unknown operand":           item(map[string]interface{}{"kind": "merge", "operand": "heart"}),
		"a broken path":                item(map[string]interface{}{"kind": "merge", "path": "Z"}),
		"an operand and a path":        item(map[string]interface{}{"kind": "merge", "operand": "rect", "path": "M0 0 L1 0 L1 1 Z"}),
		"a field of another kind":      item(map[string]interface{}{"kind": "roundCorners", "amount": 3.0}),
		"a text field of another kind": item(map[string]interface{}{"kind": "twist", "join": "round"}),
		"a merge size of zero":         item(map[string]interface{}{"kind": "merge", "w": 0.0}),
		"an unknown key property":      {"op": "animate_path_op", "clip_id": "c-1", "path_op_id": "op-1", "path_op_property": "speed"},
	} {
		editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
		edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
		res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), map[string]interface{}{"operations": []interface{}{op}})
		if res.Status == copilot.StatusOK {
			t.Fatalf("%s was accepted", name)
		}
		if _, sent := editor.sent(copilot.ScreenEdit); sent {
			t.Fatalf("%s reached the editor", name)
		}
	}
}

func TestStudioEditRefusesBrokenImagePathOps(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditImageTool(studioDeps(studio.KindImage, nil, nil))
	op := map[string]interface{}{"op": "set_path_ops", "layer_id": "l-1", "path_ops": []interface{}{map[string]interface{}{"kind": "roundCorners", "radius": studio.MaxRoundCorners + 1}}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioImage, editor), map[string]interface{}{"operations": []interface{}{op}}); res.Status == copilot.StatusOK {
		t.Fatal("a radius out of range was accepted")
	}
	if _, sent := editor.sent(copilot.ScreenEdit); sent {
		t.Fatal("a radius out of range reached the editor")
	}
}

func TestStudioPathOpsSchemaOffersTheEditorFields(t *testing.T) {
	video := NewStudioEditVideoTool(StudioDeps{}).Definition().Parameters["operations"].Items.Properties
	image := NewStudioEditImageTool(StudioDeps{}).Definition().Parameters["operations"].Items.Properties
	for editor, properties := range map[string]map[string]tools.Parameter{"video": video, "image": image} {
		list := properties["path_ops"]
		if list.Type != "array" || list.Items == nil || list.Items.Type != "object" {
			t.Fatalf("%s: path_ops must be an array of objects, got %+v", editor, list)
		}
		if list.Items.Properties["kind"].Type != "string" || list.Items.Properties["copies"].Type != "integer" || list.Items.Properties["amount"].Type != "number" {
			t.Fatalf("%s: the path op item lost its field types", editor)
		}
	}
	if video["path_op_property"].Type != "string" || video["path_op_id"].Type != "string" {
		t.Fatal("the video tool must offer path_op_id and path_op_property")
	}
	if image["live"].Type != "boolean" {
		t.Fatal("the image tool must offer live")
	}
	if _, ok := image["path_op_property"]; ok {
		t.Fatal("the image tool offers path_op_property, which only video animates")
	}
}

func TestStudioEditGuidesExplainPathOps(t *testing.T) {
	video := NewStudioEditVideoTool(StudioDeps{}).Definition().Description
	for _, want := range []string{"set_path_ops", "animate_path_op", "path_op_property", "puckerBloat", "merge", "path_ops"} {
		if !strings.Contains(video, want) {
			t.Fatalf("the video guide does not explain %s", want)
		}
	}
	image := NewStudioEditImageTool(StudioDeps{}).Definition().Description
	for _, want := range []string{"set_path_ops", "live", "roundCorners", "path_ops"} {
		if !strings.Contains(image, want) {
			t.Fatalf("the image guide does not explain %s", want)
		}
	}
}
